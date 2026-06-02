# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Temporal platform infrastructure and shared client library, intended to be absorbed into a monorepo. Read `DESIGN.md` for the full platform vision, namespace isolation model, and worker ownership decisions.

Two things live here:

| Path | What |
|---|---|
| `infra/temporal/` | Docker Compose for local dev; ECS Fargate IaC (in progress) |
| `libs/temporal-client/` | Shared Python package — monorepo services import this to connect to Temporal |

## Local Dev

```bash
cd infra/temporal
docker compose up -d
```

See `infra/temporal/README.md` for full instructions and example worker commands.

## Shared Client Library

`libs/temporal-client` is a standalone `uv`-managed Python package. Services add it as a path dependency:

```toml
dependencies = [
    "temporal-client @ file:../../libs/temporal-client",
]
```

Services call `get_client()` at startup — it reads `TEMPORAL_HOST` and `TEMPORAL_NAMESPACE` from the environment and fails fast if either is missing.

```python
from temporal_client import get_client, build_worker

client = await get_client()
worker = build_worker(client, task_queue="my-queue", workflows=[...], activities=[...])
await worker.run()
```

### Environment variables (required per service)

| Variable | Example | Notes |
|---|---|---|
| `TEMPORAL_HOST` | `localhost:7233` | gRPC address of the Temporal server |
| `TEMPORAL_NAMESPACE` | `svc-my-service` | Namespace provisioned for this service |

### Package structure

```
libs/temporal-client/src/temporal_client/
├── config.py      — reads TEMPORAL_HOST / TEMPORAL_NAMESPACE, fails fast if missing
├── client.py      — get_client() → Client
├── worker.py      — build_worker() → Worker
└── examples/      — OrderWorkflow demo; validates the lib against local Compose
```

## Temporal Conventions

- **Determinism**: no `datetime.now()`, `random`, or direct I/O inside `@workflow.defn` methods. Use `workflow.now()` and `workflow.unsafe.imports_passed_through()` for non-deterministic imports.
- **Activity options**: always set `start_to_close_timeout` + `RetryPolicy` at the call site.
- **Signals + wait**: use `@workflow.signal` + `workflow.wait_condition()` for human-in-the-loop steps. See `examples/workflows.py` for the pattern.
- **Namespace per service**: each service gets its own Temporal namespace (`svc-{name}`). Local dev uses `default`. Production namespaces are provisioned via `infra/temporal/scripts/provision-namespace.sh` (in progress).
- **Task queue naming**: services choose their own task queue name — no central registry needed since namespaces are isolated.
