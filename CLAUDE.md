# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Temporal platform infrastructure and shared Python client library, structured for drop-in to a monorepo. Temporal is a platform primitive — owned centrally, each service connects with its own namespace and mTLS identity.

Two things live here:

| Path | What |
|---|---|
| `infra/temporal/` | Podman Compose stack for local dev; ECS Fargate IaC (in progress); cert generation scripts |
| `libs/temporal-client/` | Shared Python package — monorepo services import this to connect to Temporal |

Read `DESIGN.md` for the full platform vision, namespace isolation model, and worker ownership decisions. Read `README.md` for setup and usage instructions.

## Local Dev Stack

```bash
cd infra/temporal
make certs    # one-time: generate CA + server + dev-client certs (requires: brew install step)
make up       # start Temporal + Postgres 18 + UI (~30s for auto-setup to finish)
make ps       # verify all three containers are up
make ui       # open http://localhost:8080
```

Full Makefile reference: `make help`

## Shared Client Library

`libs/temporal-client` is an installable `uv`-managed Python package (`src/` layout, `hatchling` build backend).

Services add it as a path dependency:

```toml
dependencies = [
    "temporal-client @ file:../../libs/temporal-client",
]
```

Usage:

```python
from temporal_client import get_client, build_worker

client = await get_client()   # reads TEMPORAL_HOST + TEMPORAL_NAMESPACE from env
worker = build_worker(client, task_queue="my-queue", workflows=[...], activities=[...])
await worker.run()
```

### Environment variables (required per service)

| Variable | Local dev value | Notes |
|---|---|---|
| `TEMPORAL_HOST` | `localhost:7233` | gRPC address |
| `TEMPORAL_NAMESPACE` | `default` or `svc-{name}` | Must be provisioned first |
| `TEMPORAL_TLS_CA` | `certs/ca.crt` path | mTLS: optional, falls back to plaintext if unset |
| `TEMPORAL_TLS_CERT` | `certs/svc-name.crt` path | mTLS: must pair with TLS_KEY |
| `TEMPORAL_TLS_KEY` | `certs/svc-name.key` path | mTLS: must pair with TLS_CERT |

### Package structure

```
libs/temporal-client/src/temporal_client/
├── config.py    — reads env vars, fails fast with clear error if required vars missing
├── client.py    — get_client() → Client; builds TLSConfig when cert vars are set
├── worker.py    — build_worker() → Worker convenience wrapper
└── examples/    — OrderWorkflow demo; use make example-worker / example-start to run
```

## mTLS

Local certs live in `infra/temporal/certs/` (gitignored). Generate with `make certs` (one-time) and issue per-service certs with `make issue-cert SVC=svc-name`. The `step` CLI is required (`brew install step`).

The client lib enables mTLS automatically when all three `TEMPORAL_TLS_*` env vars are set. No code changes needed between plaintext and mTLS — only env vars differ.

## Namespaces

Each service gets a dedicated Temporal namespace. The `default` namespace is auto-created by the `auto-setup` image on first boot.

```bash
# Create a namespace
podman exec temporal-dev_temporal_1 \
  temporal operator namespace create svc-orders --address temporal:7233

# Issue a client cert for it
make issue-cert SVC=svc-orders   # from infra/temporal/
```

## Temporal Conventions

- **Determinism**: no `datetime.now()`, `random`, or direct I/O inside `@workflow.defn` methods. Use `workflow.now()` and `workflow.unsafe.imports_passed_through()` for non-deterministic imports.
- **Activity options**: always set `start_to_close_timeout` + `RetryPolicy` at the call site.
- **Signals + wait**: use `@workflow.signal` + `workflow.wait_condition()` for human-in-the-loop steps. See `examples/workflows.py` for the pattern.
- **Task queues**: each service picks its own name — no central registry needed; namespace isolation prevents collisions.

## AWS Deployment (in progress)

Planned: ECS Fargate for Temporal server + UI, Aurora PostgreSQL per env (shared with app services, Temporal uses dedicated databases). CA key in Secrets Manager, certs issued via GitHub Actions at service onboarding. See `README.md` AWS section and `DESIGN.md` for details.
