# Temporal Authorization & RBAC — Design

Status: **planned** (not yet built). This documents the decision to run Temporal
OSS with a custom authorization layer ("Option B") so that per-namespace RBAC is
enforced for both service workers and human UI/CLI access.

Temporal is a platform primitive owned by the platform team (see `../../DESIGN.md`
§2). This doc covers only the authorization layer for the shared Temporal
instance — not the WaaS orchestration service, which authorizes its own API with
Entra tokens independently.

---

## 1. Why this exists

Temporal OSS ships **no built-in RBAC**. It ships an authorization *interface*
(two Go hooks) whose default is a no-op — anyone who can reach the frontend gRPC
port or the Web UI has full admin on every namespace. Managed RBAC/SSO is a
Temporal Cloud feature.

We need real per-namespace isolation for two populations:

| Population | Reaches Temporal via | Needs |
|---|---|---|
| **Service workers/clients** | gRPC, mTLS cert identity | Read/Write/Worker on **their own** namespace only |
| **Operators + power-user devs** | Web UI (and CLI) | Per-namespace scoped visibility + actions; operators get cross-namespace |

The direct-worker model already gives the first population its boundary via
per-namespace mTLS. The gap is the **Web UI**, where OSS visibility is
all-or-nothing without the authorizer plugin. Closing that gap is the whole
reason for this work.

---

## 2. How Temporal authorization works

Two compile-time Go interfaces sit at the frontend. **Every** gRPC call — from a
worker, the CLI, or the UI's backend — passes through both:

```
caller ──(mTLS cert and/or Bearer JWT)──▶ frontend
                                            │
                                   ClaimMapper.GetClaims(AuthInfo)
                                     AuthInfo = { TLSSubject, AuthToken, headers }
                                            │  → Claims{ Subject, System role, Namespaces: {ns→role} }
                                            ▼
                                   Authorizer.Authorize(Claims, CallTarget)
                                     CallTarget = { Namespace, APIName, Request }
                                            │  → Allow | Deny
                                            ▼
                                   API executes (or PermissionDenied)
```

- **Roles** are a per-namespace bitmask: `Worker`, `Reader`, `Writer`, `Admin`,
  plus a system-level role for cross-namespace operators.
- **Authz is stateless** — derived entirely from the cert or token on each call.
  There is no ACL store. Namespace provisioning stays "create namespace"; there
  is no grant table to maintain. Role assignment lives in Entra (for humans) and
  in the cert-naming convention (for services).

### What we build vs. reuse

| Piece | Decision |
|---|---|
| `Authorizer` | **Reuse Temporal's default.** It already maps gRPC API name → required role (readers hit read-only APIs, writers can start/signal, etc.). Do not rewrite. |
| `ClaimMapper` | **Custom.** We have two identity sources (cert + JWT); the stock mappers each understand only one. This ~200-line mapper is the real deliverable. |

---

## 3. The custom ClaimMapper (dual-input)

One mapper, two inputs: mTLS cert for services (no token), Entra JWT for humans.

```go
// claimmapper.go (illustrative — not version-pinned)
func (m *dualClaimMapper) GetClaims(info *authorization.AuthInfo) (*authorization.Claims, error) {
    // Human path: UI/CLI presents an Entra bearer token
    if info.AuthToken != "" {
        return m.jwt.GetClaims(info) // delegate to the default JWT claim mapper
    }
    // Service path: worker presents an mTLS cert, no token
    if info.TLSSubject != nil {
        ns := namespaceFromSAN(info.TLSSubject) // e.g. spiffe://rodenas.us/svc-orders → "svc-orders"
        return &authorization.Claims{
            Subject:    info.TLSSubject.CommonName,
            Namespaces: map[string]authorization.Role{
                ns: authorization.RoleWorker | authorization.RoleWriter,
            },
        }, nil
    }
    return nil, errUnauthenticated
}
```

The human path delegates to Temporal's **default JWT claim mapper**, configured
with `permissionsClaimName: roles` — see §6 for why that lets us skip custom JWT
parsing entirely.

---

## 4. The custom server image

Authorization hooks are **compile-time** — there is no runtime plugin/dlopen.
The supported extension path is building the server *as a library*:

```go
// main.go (illustrative)
s, err := temporal.NewServer(
    temporal.WithConfigLoader(cfgPath, env, zone),
    temporal.WithClaimMapper(func(*config.Config) authorization.ClaimMapper {
        return newDualClaimMapper(...)
    }),
    // Authorizer left as default
)
```

Produces a `temporal-server` binary with our mapper compiled in. Multi-stage
Docker build:

```
build  stage: golang → go test → compile main.go (imports go.temporal.io/server) → temporal-server
runtime stage: minimal base + binary + rendered config
```

Owned files (proposed `infra/temporal/server-image/`):

```
server-image/
├── go.mod / go.sum
├── main.go           # wires WithClaimMapper
├── claimmapper.go    # dual cert/JWT logic  ← the real deliverable
├── claimmapper_test.go
└── Dockerfile        # ARG TEMPORAL_VERSION pins upstream
```

**Only the server image is custom.** `temporalio/admin-tools` (schema tools + CLI)
and `temporalio/ui` stay stock — no fork, no build.

---

## 5. What changes about `auto-setup`

Local dev currently runs `temporalio/auto-setup:1.27` (`compose.yml`), which
bundles server + admin-tools + a boot script that renders config, migrates
schema, and creates the `default` namespace on every start. A custom server
binary can't use that bundled image.

**Nothing is lost — the steps unbundle.** Each auto-setup responsibility maps to
an explicit, still-available command:

| auto-setup did (on every boot) | Replacement |
|---|---|
| Render config from env | Committed config template (keep `dockerize`, or a static YAML) |
| Create DB + `setup-schema`/`update-schema` | One-shot `temporal-sql-tool` run from **stock admin-tools** |
| Create `default` namespace + search attrs | `temporal operator namespace create` (init step) |
| `exec temporal-server` | Custom server image |

Why this is an upgrade for prod, not a regression: auto-setup migrates schema on
**every server boot**, which races when multiple Fargate tasks start together.
Production wants schema migration as a **deliberate one-shot** (a gated deploy
step / single `run-task`), with server tasks doing nothing but serve.

- **Local dev:** add an `init` compose service (stock admin-tools) that runs
  schema setup + namespace creation once, gated by a healthcheck, before the
  custom server starts. Mirrors the prod split.
- **Prod:** schema migration is a pipeline/`run-task` step in the deploy, keyed
  to the Temporal version being rolled out.

---

## 6. Entra (Azure AD) — the human path

- **App registration for the Temporal UI** (OIDC): redirect/callback URI on the
  ALB, client ID + secret (secret → Secrets Manager).
- **App roles carry the permission.** Set each app-role *value* to Temporal's
  native `namespace:role` string — e.g. `svc-orders:write`, `svc-billing:read`,
  `system:admin`. The token's `roles` claim then already speaks Temporal's
  language, so the **default JWT claim mapper works unchanged** with
  `permissionsClaimName: roles`. No custom JWT parsing.
- **JWKS** for signature verification:
  `https://login.microsoftonline.com/<tenant>/discovery/v2.0/keys` via
  `keySourceURIs`. The default token key provider handles key rotation.
- **Role assignment = Entra group/app-role assignment** — RBAC administered
  where we already do it, mirroring the FastAPI app-roles pattern.

**Gotcha to validate early:** app roles must land in the **access token** the UI
forwards to the frontend (not just the id token), and the token audience must be
our app. This is the finicky Entra detail that bites people — verify before
building anything else.

---

## 7. Cert → namespace convention (the service path)

Per-service mTLS certs already encode identity. Formalize the mapping the
ClaimMapper reads:

- Identity source: **SAN URI** (SPIFFE-style, e.g. `spiffe://rodenas.us/svc-orders`)
  or CN. Pick one; SAN URI is cleaner and unambiguous.
- Mapping: `svc-<name>` → namespace `svc-<name>`, role `Worker | Writer`.
- Operators' service certs (platform tooling) → system role if needed.

This is the input to `namespaceFromSAN`. Certs are issued today via
`make issue-cert SVC=…`; the convention just fixes what goes in the SAN.

---

## 8. Role model

| Principal | Identity | Grant |
|---|---|---|
| Service worker | mTLS cert | `Worker + Writer` on its own namespace |
| Power-user dev | Entra app role | `Reader` (or `Writer`) on their team namespace(s) |
| Operator | Entra app role | `system:admin` (cross-namespace) |

`Reader` sees and queries; `Writer` can start/signal/terminate; `Worker` can poll
task queues; `Admin` includes namespace management. Devs default to `Reader`
unless a team needs self-service terminate/retry (then `Writer`).

---

## 9. Deployment topology

### AWS (ECS Fargate) — primary target

- **Custom server image** in ECR; frontend service stays **private** (no public
  gRPC).
- **UI behind the ALB with OIDC done in `ui-server`** (not ALB-only OIDC): the
  user's token must reach the frontend to drive per-namespace scoping. Once
  enabled, the UI shows each person exactly the namespaces their token grants —
  the per-namespace UI scoping this whole effort buys, for free.
- **JWKS** reachable from frontend tasks.
- **Secrets Manager:** Entra client secret (+ existing CA key).
- **Schema migration** as a gated deploy step (stock admin-tools `run-task`).

### Local dev — mirror the shape, relax enforcement

- Run the **same custom server image** instead of `auto-setup`, plus the `init`
  service for schema + `default` namespace (§5).
- UI OIDC against a **dev app registration**, with a single config flag to
  **disable auth locally** when hacking — same image and shape as prod,
  enforcement optional.
- Re-enabling the local mTLS toggle (`compose.yml` + client `TEMPORAL_TLS_*`)
  exercises the service-cert path in dev too.

---

## 10. Build & delivery pipeline

Treat the server image like any service image, plus an upstream-version pin.

- **Trigger:** change under `infra/temporal/server-image/`.
- **Steps (self-hosted runner, DinD):** `go test` → `docker build --build-arg
  TEMPORAL_VERSION=<pinned>` → tag `{temporal-version}-{git-sha}` → push to ECR.
- **Upgrade unit — lock together:** `go.temporal.io/server` module version, the
  server image tag, and the `admin-tools` tag used for migration. A Dependabot
  bump to `go.temporal.io/server` is the Temporal-upgrade PR: bump all three,
  re-test, re-run schema migration on rollout.
- Reuse the standard build-and-push-to-ECR composite; the only Temporal-specific
  part is the version pin.

**This is the real recurring cost of Option B** — not the ~200 lines of Go, but
owning a custom server image tracked against upstream Temporal on every upgrade.

---

## 11. CLI / tctl path

Humans running `temporal`/`tctl` against prod need a JWT too:
`--grpc-meta authorization="Bearer <token>"` or `TEMPORAL_CLI_AUTHORIZATION_TOKEN`.
Decide how operators mint that token — a device-code flow against the same UI app
registration is the natural choice. Service automation continues to use mTLS
certs (no token needed) via the §3 cert path.

---

## 12. Delivery plan

This is a **build sequence**, not a test plan and not a requirements list. Each
phase is an ordered increment that leaves the stack working and de-risks one
independent thing; **Build** is what you construct, **Done when** is the
acceptance check that proves it. The requirements these ultimately satisfy —
a worker reaches only its own namespace, a dev sees only their team's workflows,
an operator can act cross-namespace — are the use cases in §1/§8, not phases.

### Phase 1 — Custom image runs locally (auth off)
- **Build:** scaffold `server-image/` (main.go, dualClaimMapper, Dockerfile,
  tests); swap `auto-setup` → custom image + `init` service for schema/namespace
  setup. Mapper starts permissive (auth disabled).
- **Done when:** `make up` brings the stack up and existing workers connect,
  with no authz behavior change. Isolates operational/build risk only.

### Phase 2 — Service path enforced
- **Build:** enable the cert branch of the ClaimMapper; re-enable local mTLS;
  fix the cert SAN → namespace convention (§7).
- **Done when:** a `svc-a` cert can poll/start in `svc-a` but a call against
  `svc-b` returns `PermissionDenied`.

### Phase 3 — Human path enforced
- **Build:** Entra UI app registration with `namespace:role` app-role values;
  wire `ui-server` OIDC; enable operator CLI token minting (same app, §11).
- **Done when:** the access token forwarded by `ui-server` carries the roles
  claim with correct audience (§6 gotcha); a dev sees only their namespace(s) in
  the UI; an operator sees all. This is the highest-risk phase — keep it its own
  gate.

### Phase 4 — Production on ECS
- **Build:** ECS task defs (custom server private, UI behind ALB), schema
  migration as a gated deploy step, secrets wired (Entra client secret, CA key).
- **Done when:** prod stack serves with the same enforcement proven in phases
  2–3, frontend not publicly reachable.

---

## 13. Open questions / risks

- **Access-token roles claim (§6):** must confirm Entra emits app roles in the
  access token forwarded by `ui-server`, with correct audience. Validate before
  committing to the rest.
- **System-role granularity:** is a single `system:admin` enough, or do we need
  a read-only cross-namespace operator role? Likely yes for on-call viewers.
- **Config templating:** keep `dockerize`-style env rendering from auto-setup, or
  move to a committed static config? Leaning committed config for prod
  reproducibility.
- **Upgrade cadence:** how fast do we track upstream Temporal releases given each
  is now a build+test+migrate PR? Define a support policy (e.g. N-1).
- **Cert SAN format:** SPIFFE URI vs CN — decide once; it's baked into
  `namespaceFromSAN` and the cert-issuing scripts.
