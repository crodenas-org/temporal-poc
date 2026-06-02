# Temporal — Local Dev

## Start

```bash
cd infra/temporal
podman compose up -d
```

- gRPC: `localhost:7233`
- UI: `http://localhost:8080`

## Stop / reset

```bash
podman compose down          # stop, keep data
podman compose down -v       # stop and wipe postgres volume
```

## Service environment variables

Copy `.env.example` to your service's `.env` and set:

```
TEMPORAL_HOST=localhost:7233
TEMPORAL_NAMESPACE=default   # use your provisioned namespace in non-local envs
```

## Validate with the example worker

From the `libs/temporal-client` directory:

```bash
# Terminal 1 — worker
TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default \
  uv run python -m temporal_client.examples.worker

# Terminal 2 — trigger a workflow
TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default \
  uv run python -m temporal_client.examples.starter start

# Then approve or reject using the workflow ID printed above
TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default \
  uv run python -m temporal_client.examples.starter approve <workflow-id>
```
