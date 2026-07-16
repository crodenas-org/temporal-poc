# Custom Temporal Server Image

A thin wrapper around the standard Temporal server that compiles in a custom
`ClaimMapper` for per-namespace RBAC. This is the officially supported extension
path — see the [server-as-a-library docs](https://docs.temporal.io/self-hosted-guide/embedded-server)
and the [authorizer sample](https://github.com/temporalio/samples-server/tree/main/extensibility/authorizer)
it's modeled on. Full design: [`../AUTHZ.md`](../AUTHZ.md).

## What's here

| File | Role |
|---|---|
| `claimmapper.go` | The **owned logic**: dual-input mapper (mTLS cert → namespace, Entra JWT → default JWT mapper). Stable across Temporal versions. |
| `claimmapper_test.go` | Unit tests for the cert branch + guard rails. No server/DB/network needed. |
| `main.go` | Server bootstrap. Registers the mapper via `temporal.WithClaimMapper`; everything else is stock Temporal. |
| `Dockerfile` | Multi-stage build → static binary on distroless. |
| `go.mod` | Pins `go.temporal.io/server` — the version pin is the upgrade unit. |

Only the **server** image is custom. `temporalio/admin-tools` (schema + CLI) and
`temporalio/ui` stay stock.

## Phase 1 posture (per AUTHZ.md §12)

**Authorization is OFF.** The mapper is compiled in and registered, but Temporal
only invokes a claim mapper when the server config declares a
`global.authorization` block — Phase 1 config has none. So this image is
**behavior-identical to `auto-setup`**, minus the bundled boot-time schema setup.
That isolation is deliberate: Phase 1 proves the *operational* swap (custom build,
unbundled schema/namespace init) without changing any authz behavior.

The cert branch is turned on in Phase 2; the JWT delegate in Phase 3.

## Build & test

```bash
cd infra/temporal/server-image
go mod tidy      # generates go.sum against the pinned version
go test ./...    # runs the mapper unit tests
docker build -t temporal-server-custom .
```

> **Not yet built/validated in this repo.** There is no Go toolchain in the dev
> loop here; the first real `go mod tidy` + `docker build` runs in CI (self-hosted
> runner, DinD). The **version-sensitive lines are in `main.go`** — config load
> (`config.Load`) and logger construction track `go.temporal.io/server`, so a
> version bump may rename them; `go build` surfaces it immediately. The
> `claimmapper.go` interfaces (`ClaimMapper`, `AuthInfo`, `Claims`, `Role`) are
> stable and were verified against the current authorization package.

## Still needed to complete Phase 1

1. ~~A server **config file** (`config/docker.yaml`).~~ Done — committed static
   config, authorization block intentionally omitted.
2. An **`init`** step (stock `admin-tools`) that runs `temporal-sql-tool` schema
   setup, plus a step that creates the `default` namespace, replacing auto-setup's
   boot script. Note: the distroless server image has **no `temporal` CLI** — all
   CLI/schema/namespace ops move to an `admin-tools` sidecar.
3. **compose.yml** swap: `temporalio/auto-setup` → build this image + the
   `admin-tools` sidecar; mount `config/` and `dynamicconfig/`.
4. **Makefile**: repoint namespace targets (currently `podman exec temporal …`)
   at the `admin-tools` sidecar, since the server image has no CLI.
5. Verify `make up` brings the stack up and existing workers connect (auth off).
