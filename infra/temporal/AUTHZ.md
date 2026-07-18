# Temporal Authorization & RBAC — Design

Status: **human path built and verified locally** (2026-07-16); service cert path
(Phase 2) and ECS (Phase 4) outstanding. See §12 for phase state, §14 for the
bring-up and its hard-won gotchas, and §15 for the scoped-UI decision.

This documents the decision to run Temporal OSS with a custom authorization layer
("Option B") so that per-namespace RBAC is enforced for both service workers and
human UI/CLI access.

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
| `Authorizer` | **Reuse Temporal's default, wrapped by a keyhole.** The default already maps gRPC API name → required role; do not rewrite it. `uiRenderAuthorizer` delegates every decision to it except the two cluster-scoped APIs the OSS UI needs to render for a scoped human (§15). |
| `ClaimMapper` | **Custom — and mandatory, not optional.** Two identity sources (cert + JWT) and the stock mappers understand only one. It must also read the role-bearing ID token out of `ExtraData`, which no stock mapper does (§14 #12). The real deliverable. |

---

## 3. The custom ClaimMapper (dual-input)

One mapper, two inputs: mTLS cert for services (no token), Entra JWT for humans.

```go
// claimmapper.go (illustrative — see the real file for the details that matter)
func (m *dualClaimMapper) GetClaims(info *authorization.AuthInfo) (*authorization.Claims, error) {
    // Human path: the UI sends TWO tokens. The Authorization header holds a
    // Microsoft Graph access token (no roles, unverifiable here); the ID token in
    // Authorization-Extras carries the roles. Prefer ExtraData, fall back to
    // AuthToken for the CLI. This is why the mapper MUST be custom (§14 #12).
    if token := humanToken(info); token != "" {
        return m.jwt.GetClaims(&authorization.AuthInfo{
            AuthToken: token,
            Audience:  m.audience, // pin aud to our client id
        })
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
*parsing* entirely. Note what remains ours: choosing the right header, and pinning
the audience.

---

## 4. The custom server image

Authorization hooks are **compile-time** — there is no runtime plugin/dlopen.
The supported extension path is building the server *as a library*:

```go
// main.go (illustrative — see the real file; auth-off keeps the last three off)
s, err := temporal.NewServer(
    temporal.WithConfigLoader(cfgPath, env, zone),
    temporal.WithClaimMapper(func(*config.Config) authorization.ClaimMapper {
        return newDualClaimMapper(...)
    }),
    // The library NewServer path does NOT build the authorizer from config —
    // without this it stays noop/allow-all even with JWT configured (§14 #4).
    temporal.WithAuthorizer(newUIRenderAuthorizer(authorization.NewDefaultAuthorizer())),
    // Trims ListNamespaces to the caller's namespaces (§14 #14, §15).
    temporal.WithChainedFrontendGrpcInterceptors(newNamespaceFilterInterceptor()),
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
├── go.mod / go.sum        # go.mod pins the Temporal version — bumping it is the upgrade PR (§10)
├── main.go                # wires the mapper, authorizer, and interceptor
├── claimmapper.go         # dual cert/JWT logic  ← the real deliverable
├── authorizer.go          # keyhole over the default authorizer: UI render APIs (§15)
├── interceptor.go         # filters ListNamespaces to the caller's namespaces (§14 #14)
├── authdebug.go           # TEMPORAL_AUTH_DEBUG=1 diagnostics; denials are otherwise silent (#13)
├── claimmapper_test.go    # cert path
├── jwt_claims_test.go     # human path: system prefix (#11), header routing (#12), audience pin
├── authorizer_test.go     # incl. "does not widen anything else"
├── interceptor_test.go    # incl. operator carve-out + pagination edge
└── Dockerfile             # ARG TEMPORAL_VERSION pins upstream
```

**Only the server image is custom.** `temporalio/admin-tools` (schema tools + CLI)
and `temporalio/ui` stay stock — no fork, no build. That is a deliberate
constraint with a real cost: the UI's 403-becomes-login-loop behavior (§14 #14) is
something we work *around* rather than fix.

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
  native `namespace:role` string — e.g. `svc-orders:write`, `svc-billing:read`.
  Cluster-wide roles **must** use the literal namespace `temporal-system`
  (`temporal-system:admin`, `temporal-system:read`) — `system:admin` looks right
  and grants nothing (§14 #11). The token's `roles` claim then already speaks
  Temporal's language, so **no custom JWT *parsing*** is needed
  (`permissionsClaimName: roles`) — but a custom claim mapper is still required to
  read the token out of the right header (§14 #12).
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
| Operator | Entra app role | `temporal-system:admin` (cross-namespace) |
| Read-only operator | Entra app role | `temporal-system:read` (cross-namespace read) |

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

### Phase 3 — Human path enforced ✅ DONE (2026-07-16, verified live)
- **Built:** Entra app registration with Temporal-native app-role values
  (cluster roles are `temporal-system:*`, §14 #11); `ui-server` OIDC wired;
  `dualClaimMapper` sources the role-bearing **ID token** from `ExtraData` and
  pins its audience (§14 #12); `uiRenderAuthorizer` allows the two cluster-scoped
  render APIs for any caller holding a grant (§15).
- **Verified live, both directions** — the negative test is the one that counts
  (#4, #13):

  | Roles | Switcher shows | `default` | `svc-demo` |
  |---|---|---|---|
  | `temporal-system:admin` | all namespaces (server returns 3, ~2245 B) | 200 | 200 (correct) |
  | `default:read` | **only `default`** (server returns 1, 724 B) | 200 | **403 on all four calls** |

  (`temporal-system` never appears in the switcher for anyone — the stock UI hides
  its own internal namespace. Not our filter; the server does return it.)

- **Not done:** operator CLI token minting (§11).
- **Known edge:** hand-typing a forbidden namespace URL still login-loops
  (§14 #14) — the filter removes the invitation, not the underlying UI behavior.

### Phase 4 — Production on ECS
- **Build:** ECS task defs (custom server private, UI behind ALB), schema
  migration as a gated deploy step, secrets wired (Entra client secret, CA key).
- **Done when:** prod stack serves with the same enforcement proven in phases
  2–3, frontend not publicly reachable.

---

## 13. Open questions / risks

- **~~Access-token roles claim (§6)~~ — RESOLVED (2026-07-16), but not as first
  written.** Entra emits app roles in the **ID token**, which `ui-server` forwards
  via `Authorization-Extras`. The frontend does **not** read that header on its
  own — Temporal's default JWT mapper only ever reads `Authorization`, which holds
  a Microsoft Graph access token with no roles. Our `dualClaimMapper` now sources
  the ID token from `ExtraData` and pins its audience. See §14 #11/#12.
- **System-role granularity:** is a single cluster-admin role enough, or do we
  need a read-only cross-namespace operator role? **Both exist now**
  (`temporal-system:admin`, `temporal-system:read`). Whether scoped humans need
  `temporal-system:read` depends on the §15 UI-render decision.
- **Config templating:** keep `dockerize`-style env rendering from auto-setup, or
  move to a committed static config? Leaning committed config for prod
  reproducibility.
- **Upgrade cadence:** how fast do we track upstream Temporal releases given each
  is now a build+test+migrate PR? Define a support policy (e.g. N-1).
- **Cert SAN format:** SPIFFE URI vs CN — decide once; it's baked into
  `namespaceFromSAN` and the cert-issuing scripts.

---

## 14. Local auth bring-up — status & hard-won learnings

**Status (2026-07-16, second pass), branch `phase-2-auth`:** the human path is now
**genuinely** working and verified with live logins in both directions:

| Roles | Result |
|---|---|
| `temporal-system:admin` | Full UI. Every call 200s, including `ListNamespaces` + `GetClusterInfo`. |
| `default:read` | Claims map to `System=0, Namespaces{default:Reader}`. Exactly two calls denied: `ListNamespaces`, `GetClusterInfo`. All namespace-scoped calls (`namespaces/default`, `search-attributes`, `workflows`, `workflow-count`) 200. |

**The first pass at this section was wrong, and its errors propagated into §15.**
Two real bugs meant *no human token ever produced any claims at all* — every API
was denied for every user, which was misread as "the OSS UI needs system APIs":

1. `system:admin` never granted cluster scope (#11) — the prefix is
   `temporal-system`.
2. The mapper read the wrong token entirely (#12) — claim mapping failed with
   `crypto/rsa: verification error` on every request.

Both are fixed and pinned by tests (`jwt_claims_test.go`). Set
`TEMPORAL_AUTH_DEBUG=1` on the `temporal` service to see, per request, which
credential surfaces arrived and what claims they mapped to — authorization
denials are otherwise **completely silent**, which is what hid these for two
sessions.

### Local workflow

| Command | Effect |
|---|---|
| `make up` | Plaintext, **auth OFF** (Phase 1 behavior; workers connect freely) |
| `make auth-on` | Recreate server + UI with Entra enforcement (reads `.env`) |
| `make auth-off` | Back to plaintext |
| `make entra-app` | Create/refresh the single shared app registration (needs `az login`) |
| `make entra-assign ROLES="default:read"` | Set your app roles (for testing scope) |

`.env` (gitignored) holds `TEMPORAL_AUTH_TENANT_ID/CLIENT_ID/CLIENT_SECRET`,
written by `scripts/entra-app-setup.sh`.

### Gotchas we hit (all real, all fixed)

1. **`config.Load` signature (1.27):** `config.Load(env, configDir, zone, &cfg)
   error` — not the functional-options form. Config lives at `<configDir>/<env>.yaml`.
2. **`Claims` has no `AuthType` field in 1.27** (added later upstream).
3. **admin-tools ENTRYPOINT is `tini -- sleep infinity`.** Any `command:` is
   appended as args to `sleep` and ignored; override `entrypoint: ["bash"]` to run
   the schema/namespace scripts.
4. **The library `NewServer` path does NOT build the authorizer from config.**
   `authorization.authorizer: "default"` in YAML is insufficient — without an
   explicit `temporal.WithAuthorizer(authorization.NewDefaultAuthorizer())` the
   authorizer is **noop (allow-all)**. Most dangerous gotcha here: the positive
   test (admin sees everything) is indistinguishable from no enforcement. **Only a
   negative test (a scoped user denied) proves it.** This is why we test scoping.
5. **Enabling the authorizer requires `internal-frontend`.** Temporal's own system
   workers connect with no credentials; once the authorizer is on they're rejected
   (`Request unauthorized`) and the server exits(1). Fix: run the `internal-frontend`
   service (port 7236, bypasses auth) **and omit `publicClient` entirely**.
   Production-correct, not a local hack. Requires adding `internal-frontend` to
   `ForServices` in `main.go`.
6. **The OSS UI's landing page needs exactly two cluster-scoped APIs**
   (**corrected 2026-07-16**; the original claim of "500s" and "needs system APIs
   broadly" was an artifact of bugs #11/#12 — every call was failing, not just
   system ones). Measured with claim mapping actually working, a `default:read`
   user is denied precisely:
   - `ListNamespaces` — `ScopeCluster` (`common/api/metadata.go:81`)
   - `GetClusterInfo` — `ScopeCluster` (`metadata.go:122`)

   and gets **403** (not 500); the UI then bounces to `/login`, which presents as
   a login loop. Everything else it needs is `ScopeNamespace` and works fine —
   including `DescribeNamespace` (`metadata.go:80`) and `ListSearchAttributes`
   (`metadata.go:165`). So the gap between a scoped user and a working UI is two
   API calls, not an architectural ceiling. See §15.
7. **Entra token mechanics — the UI sends TWO tokens and only one is usable.**
   App-role *values* are Temporal-native `<namespace>:<role>`, so no custom JWT
   parsing is needed — but the token routing is the trap:

   | Header | Token | aud | `roles` |
   |---|---|---|---|
   | `Authorization` | Graph **access** token | `00000003-0000-0000-c000-000000000000` | absent |
   | `Authorization-Extras` | **ID** token | our client id | **present** |

   `TEMPORAL_AUTH_SCOPES=openid,profile,email` requests only Graph scopes, so the
   access token is addressed to Graph — unverifiable against our tenant JWKS and
   role-less. The roles live in the ID token. See #12 for what that breaks.
8. **`ui-server` OIDC discovery is one-shot at startup.** If it can't fetch the
   tenant's `.well-known/openid-configuration` at boot, `/api/v1/settings` returns
   `Auth.Options: null` and sign-in bounces with no MS prompt. Fix: `podman restart
   temporal-dev_temporal-ui_1`. Watch for it after `make auth-on` recreates the UI.
9. **podman-compose + one-shot containers:** re-running `make up` over a live stack
   can exit 125 on the already-exited `temporal-schema`/`temporal-defaultns`. Use
   `make reset && make up` for a clean bring-up. (`service_completed_successfully`
   ordering *is* honored.)
10. **Namespace creation needs auth off (or admin creds).** With auth on the
    admin-tools sidecar has no token, so `make namespace` fails. Create namespaces
    with auth off, then flip on.
11. **`system:admin` grants NOTHING at cluster scope — the prefix is
    `temporal-system`.** The default JWT mapper only sets `claims.System` when the
    permission's namespace part equals `primitives.SystemLocalNamespace`, which is
    the literal string **`"temporal-system"`**
    (`default_jwt_claim_mapper.go:43` → `primitives/namespaces.go:35`). A role
    named `system:admin` parses as a *namespace* role on a namespace called
    `system` — which doesn't exist — leaving `claims.System == 0` and every
    `ScopeCluster` API denied. It reads exactly like cluster admin and is silently
    inert. Cluster roles must be `temporal-system:admin` / `temporal-system:read`.
    Pinned by `TestSystemPrefix_Regression`.
12. **The frontend reads `Authorization`, but the roles are in
    `Authorization-Extras`.** `AuthInfo.AuthToken` is the `Authorization` header;
    `AuthInfo.ExtraData` is `Authorization-Extras` (`interceptor.go:184`). The
    default JWT mapper reads **only** `AuthToken` — so out of the box it validates
    the Graph access token (#7), fails with `crypto/rsa: verification error`, and
    **claim mapping fails for every single request**. Every API is denied, not just
    cluster-scoped ones. Fix: `dualClaimMapper` prefers the ID token from
    `ExtraData`, normalizes the `Bearer` prefix, and pins `aud` to
    `TEMPORAL_AUTH_CLIENT_ID` so another app's tenant-signed token can't be
    replayed. This is what `ExtraData` exists for — and why a custom claim mapper
    is required, not optional, for the UI path.
13. **Authorization failures are silent.** The authorizer returns
    `PermissionDenied` with no server log, so "misrouted token", "role-less token"
    and "correctly denied" are indistinguishable from the outside. Combined with
    #4 (a positive admin test can't detect a noop authorizer), this is why these
    bugs survived a session that believed the path was "validated end to end".
    `TEMPORAL_AUTH_DEBUG=1` exists precisely to break that tie — use it before
    concluding anything about why a request was denied.

14. **The stock UI turns any 403 into a login loop.** When a scoped user opens a
    namespace they lack a role on, the frontend correctly returns
    `PermissionDenied` → ui-server 403 → and the UI reads that as *unauthenticated*:
    it redirects to `/login`, re-authenticates successfully, lands back on the same
    page, 403s again, forever. Observed live:

    ```
    200 /api/v1/cluster-info          landing page fine
    200 /api/v1/namespaces            switcher fine
    403 /api/v1/namespaces/svc-demo   correctly denied
    200 /login                        ...and round we go
    ```

    Enforcement is right; the UI's reaction to it is not. We don't build the UI
    (§4 keeps it stock), so this isn't ours to fix directly. The consequence:
    **an unfiltered namespace switcher is a trap, not a cosmetic leak** — it
    advertises namespaces that dead-end the user. That is why the ListNamespaces
    filter (§15) is load-bearing rather than a nicety. Deep-linking a forbidden
    URL by hand still loops; accepted, as the alternative is forking the UI.
### Local bring-up that works (verified 2026-07-16)

1. `make reset && make up` (clean). Create namespaces **now**, while auth is off
   (#10): `make namespace NS=svc-demo`.
2. `TEMPORAL_AUTH_DEBUG=1 make auth-on`.
3. Sign in at http://localhost:8080 in a **fresh incognito window** — a normal
   window replays a token minted with the old roles after any `entra-assign`.
4. Verify with the debug log, not by eyeballing the UI:
   `podman logs temporal-dev_temporal_1 | grep AUTHDEBUG`.
   `system_role=8` is admin; `system_role=0 namespaces=map[default:2]` is
   `default:read`.

Note `Auth.Options: null` from `/api/v1/settings` is **not** a failure signal —
it is null on a working login (the old #8 diagnosis was a red herring; the login
loop it described was really #11/#12).

### Where to resume

1. **Decide §15** — the scoped-UI question is now a two-API problem, not a
   ceiling. That decision gates the rest.
2. Then: Phase 2 (service cert path + local mTLS), and the Phase 1 GHA→ECR
   pipeline. Both unchanged and lower-risk than what's already done.

---

## 15. Feasibility re-assessment & decision point

### Superseded (2026-07-16, first pass) — kept because the reasoning error matters

> The first pass concluded that the OSS UI "cannot give a namespace-scoped human a
> working scoped view" and that the marquee benefit "hits an OSS ceiling we can't
> code around" — and therefore that the honest options were a coarse operator UI
> (≈ Option A) or buying Temporal Cloud.
>
> **That conclusion was false.** It rested on §14#6, which was itself an artifact
> of bugs #11 and #12: claim mapping was failing for *every* request, so *every*
> API was denied. That was misread as "system APIs are unreachable for scoped
> users". No ceiling had been found — only a broken token path.
>
> The lesson is #13: authorization denials are silent, so a broken auth path and a
> correctly-enforcing one look identical from the UI. A "value vs. cost"
> re-assessment was written on top of an unverified failure diagnosis, and it
> nearly ended the project. **Diagnose, then decide.**

### Current assessment (2026-07-16, second pass)

Both drivers are confirmed hard requirements: **self-hosting is forced** (workflow
payloads cannot leave the VPC), and **scoped dev UI is non-negotiable**. Temporal
Cloud is out; the coarse-operator-UI compromise is out.

That combination has no row in the old decision table — but it is now a small,
bounded engineering problem rather than a ceiling. With claim mapping fixed, a
`default:read` user is denied **exactly two** cluster-scoped calls (§14#6):

| Call | Scope | Why the UI wants it |
|---|---|---|
| `ListNamespaces` | `ScopeCluster` | landing page + namespace switcher |
| `GetClusterInfo` | `ScopeCluster` | version/capability banner |

Everything else the UI touches is `ScopeNamespace` and already works for a scoped
user. We compile our own server, so both are reachable:

1. **A custom authorizer** wrapping `authorization.NewDefaultAuthorizer()` that
   allows those two APIs for any *authenticated* principal and defers everything
   else to the default. Gets the UI rendering. On its own it leaks namespace
   *names* cluster-wide (no workflow data).
2. **A frontend gRPC interceptor** — `temporal.WithChainedFrontendGrpcInterceptors`
   (`temporal/server_option.go:193`) — filtering the `ListNamespaces` **response**
   to the caller's namespaces. Custom interceptors run *after* the internal ones,
   so `ctx.Value(authorization.MappedClaims)` is already populated. This is what
   turns "renders" into genuinely *scoped*.

**Status: both (1) and (2) are BUILT and verified live (2026-07-16).**

(2) was initially deferred as a mere "name leak" — that call was wrong. An
unfiltered switcher advertises namespaces the user cannot open, and the stock UI
turns the resulting 403 into an infinite login loop (§14 #14). The filter is what
makes the scoped UI actually usable, not a privacy nicety. Measured result in §12
Phase 3.

### Live risks (both shipped)

- **Deep-links still loop.** The filter removes the *invitation* (a scoped user
  never sees `svc-demo` to click) but pasting `/namespaces/svc-demo/workflows` by
  hand still 403s and loops (§14 #14). Accepted: rare path, and the fix is forking
  the UI.
- **Response-shape coupling.** `interceptor.go` reads
  `workflowservice.ListNamespacesResponse` and `authorization.MappedClaims`. This
  is the most upgrade-sensitive code we own — re-check it on every Temporal bump
  (§10). The authorizer, which only reads `target.APIName`, is far more stable.
- **Pagination.** Filtering happens after the page is assembled, so a page can be
  short or empty while `NextPageToken` still points at more. Harmless at our
  namespace count; pinned by `TestInterceptor_PreservesPageToken`.
- **Allowlist drift.** `uiRenderAPIs` is measured from the views exercised so far,
  not derived from the UI's source. A future UI version or an unexercised view may
  call another cluster-scoped API and 403. Extend it from `TEMPORAL_AUTH_DEBUG=1`
  observation only — never by guessing (#13). Worth re-measuring on each
  `temporalio/ui` bump, which is unpinned (`:latest`) in `compose.yml`.
  `GetSystemInfo` is instructive: it is `ScopeCluster` and looks necessary, but is
  served from ui-server's own connection and never needed allowlisting.

### Outcome

**Both requirements are met on self-hosted OSS.** A namespace-scoped dev signs in
with Entra, sees only their own namespace in the switcher, browses it normally,
and cannot reach another namespace's data. An operator with `temporal-system:*`
keeps the full cluster view. No Temporal Cloud, no UI fork.

The §15 (first pass) conclusion — that this was impossible without buying Cloud —
was wrong, and it was wrong because a failure was diagnosed by inference instead
of measurement. Total cost of doing it properly: two ~100-line files and a test
suite.
