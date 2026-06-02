# Temporal Platform

Shared Temporal infrastructure and Python client library, structured for drop-in to a monorepo.

Temporal is treated as a **platform primitive** — owned and operated centrally, used by all services. Each service connects with its own namespace and mTLS identity. See `DESIGN.md` for the full platform vision.

---

## Repo structure

```
infra/temporal/
├── compose.yml              # local dev: Temporal server + Postgres 18 + UI
├── dynamicconfig/           # Temporal server tuning (committed)
├── certs/                   # generated certs — gitignored, never committed
├── .env.example             # env var template for services
├── Makefile                 # all local dev commands
└── scripts/
    └── gen-certs.sh         # generates CA + server + client certs via step CLI

libs/temporal-client/
└── src/temporal_client/
    ├── client.py            # get_client() — connects using env vars, mTLS if certs present
    ├── config.py            # reads TEMPORAL_HOST, TEMPORAL_NAMESPACE, TEMPORAL_TLS_*
    ├── worker.py            # build_worker() convenience wrapper
    └── examples/            # OrderWorkflow demo — validates the full stack end-to-end
```

---

## Prerequisites

- [Podman](https://podman.io/) with `podman-compose`
- [step CLI](https://smallstep.com/docs/step-cli/) — `brew install step`
- Python 3.11+ with [uv](https://docs.astral.sh/uv/)

---

## First-time setup

```bash
cd infra/temporal

# 1. Generate local CA, server cert, and dev-client cert
make certs

# 2. Start the stack (takes ~30s for auto-setup to initialize)
make up

# 3. Verify
make ps
```

Expected output from `make ps`:

```
NAMES                       STATUS                   PORTS
temporal-dev_postgresql_1   Up N seconds (healthy)   5432/tcp
temporal-dev_temporal_1     Up N seconds             0.0.0.0:7233->7233/tcp
temporal-dev_temporal-ui_1  Up N seconds             0.0.0.0:8080->8080/tcp
```

UI is at `http://localhost:8080` — or `make ui` to open it directly.

---

## Daily dev workflow

```bash
make up      # start stack
make down    # stop, keep data
make reset   # stop and wipe postgres volume (fresh state)
make logs    # follow all container logs
make ps      # container status
```

---

## Running the example workflow (smoke test)

Requires the stack to be running. In two terminals from `infra/temporal/`:

```bash
# Terminal 1 — start the worker
make example-worker

# Terminal 2 — trigger a workflow, copy the workflow ID
make example-start

# Approve or reject it
make example-approve ID=order-ORD-XXXXXXXX
make example-result  ID=order-ORD-XXXXXXXX
```

---

## Namespaces

The `default` namespace is created automatically by the `auto-setup` image on first boot.

To create a namespace for a service:

```bash
podman exec temporal-dev_temporal_1 \
  temporal operator namespace create svc-orders --address temporal:7233
```

To verify:

```bash
podman exec temporal-dev_temporal_1 \
  temporal operator namespace describe svc-orders --address temporal:7233
```

**Naming convention:** `svc-{service-name}` in all environments. Locally you can use `default` for quick testing.

---

## mTLS

All connections to Temporal are mutually authenticated. Each service presents a client certificate signed by the shared CA. The server validates client certs; clients validate the server cert.

### Certificate layout

```
infra/temporal/certs/    ← gitignored
├── ca.crt / ca.key      # root CA — ca.key is sensitive, store in Secrets Manager in AWS
├── server.crt / server.key   # Temporal server cert
├── temporal-ui.crt / temporal-ui.key  # UI container client cert
└── {service}.crt / {service}.key      # one per service
```

### Issuing a cert for a new service

```bash
# From infra/temporal/
make issue-cert SVC=svc-orders
```

This produces `certs/svc-orders.crt` and `certs/svc-orders.key`, signed by the local CA.

### Service env vars

Point your service at the certs:

```bash
TEMPORAL_HOST=localhost:7233
TEMPORAL_NAMESPACE=svc-orders
TEMPORAL_TLS_CA=/path/to/infra/temporal/certs/ca.crt
TEMPORAL_TLS_CERT=/path/to/infra/temporal/certs/svc-orders.crt
TEMPORAL_TLS_KEY=/path/to/infra/temporal/certs/svc-orders.key
```

mTLS is enabled automatically when all three `TEMPORAL_TLS_*` vars are set. Omitting them falls back to plaintext — useful for running without certs during early development.

---

## Using the shared client library

### Adding as a dependency

In your service's `pyproject.toml`:

```toml
dependencies = [
    "temporal-client @ file:../../libs/temporal-client",
]
```

Adjust the relative path to match your monorepo layout.

### Connecting

```python
from temporal_client import get_client, build_worker

# Reads TEMPORAL_HOST, TEMPORAL_NAMESPACE, and TEMPORAL_TLS_* from environment
client = await get_client()

# Override namespace explicitly when one process needs multiple namespaces
client = await get_client(namespace="svc-payments")
```

### Running a worker

```python
from temporal_client import get_client, build_worker

client = await get_client()
worker = build_worker(
    client,
    task_queue="my-service-queue",
    workflows=[MyWorkflow],
    activities=[my_activity],
)
await worker.run()
```

### Task queue naming

Each service chooses its own task queue name. No central registry needed — namespace isolation prevents collisions between services.

---

## Monorepo integration checklist

When dropping this into the monorepo:

- [ ] Copy `infra/temporal/` → `infra/temporal/` in the monorepo
- [ ] Copy `libs/temporal-client/` → `libs/temporal-client/` in the monorepo
- [ ] Add `infra/temporal/certs/` to the monorepo's `.gitignore`
- [ ] Each developer runs `make certs && make up` once after cloning
- [ ] First consuming service: add `temporal-client` as a path dependency, set the five env vars in its `.env`
- [ ] Provision a namespace for each service: `make issue-cert SVC=svc-name` + `temporal operator namespace create`

---

## AWS deployment (in progress)

The AWS path is planned but not yet implemented. The design:

- **Temporal server + UI**: ECS Fargate tasks per environment (dev / qa / prod)
- **Database**: Aurora PostgreSQL — one cluster per env, shared with app services. Temporal uses dedicated databases (`temporal` + `temporal_visibility`); app services use separate databases on the same cluster.
- **mTLS in AWS**: CA key stored in Secrets Manager. Service certs issued at onboarding via a GitHub Actions workflow using `step`. Certs stored in Secrets Manager and injected into ECS task definitions as secrets.
- **Namespaces**: provisioned via `infra/temporal/scripts/provision-namespace.sh` (to be built) as part of service onboarding.

See `DESIGN.md` for the full architecture including the Workflow-as-a-Service platform layer planned on top of this infrastructure.
