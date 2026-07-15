# Temporal — Local Dev

> Full setup and usage docs are in the [root README](../../README.md). This file covers quick-reference commands for when you're already set up.

## Prerequisites (first time only)

```bash
brew install podman      # containers
pip install podman-compose
brew install step        # cert generation — only needed if re-enabling mTLS
```

## First-time setup

Local dev runs plaintext — mTLS is disabled in the stack, so `make certs` is not
required. Run it only if you're re-enabling mTLS (see the root README's mTLS section).

```bash
make up       # start stack (~30s for auto-setup to finish)
make ps       # verify all three containers are healthy
```

## Daily commands

```bash
make up       # start
make down     # stop, keep data
make reset    # stop and wipe postgres volume
make logs     # follow logs
make ps       # container status
make ui       # open http://localhost:8080
```

## Smoke test — WaaS demo

Requires the stack up. Each in its own terminal, then drive it:

```bash
make dns-svc                 # domain service :8010
make compute-svc             # domain service :8011
make waas-worker             # orchestration worker
make waas-api                # WaaS API :8004

make create-request          # -> request_id
make approve ID=<request_id>
make status  ID=<request_id>
```

See `../../services/waas/README.md` for the workflow shape.

## Namespace management

```bash
# Create a namespace for a new service
podman exec temporal-dev_temporal_1 \
  temporal operator namespace create svc-orders --address temporal:7233

# Issue a client cert for it
make issue-cert SVC=svc-orders
```

## Service env vars

Copy `.env.example` to your service `.env`:

```
TEMPORAL_HOST=localhost:7233
TEMPORAL_NAMESPACE=svc-orders
TEMPORAL_TLS_CA=/path/to/infra/temporal/certs/ca.crt
TEMPORAL_TLS_CERT=/path/to/infra/temporal/certs/svc-orders.crt
TEMPORAL_TLS_KEY=/path/to/infra/temporal/certs/svc-orders.key
```
