# Temporal — Local Dev

> Full setup and usage docs are in the [root README](../../README.md). This file covers quick-reference commands for when you're already set up.

## Prerequisites (first time only)

```bash
brew install step        # cert generation
brew install podman      # containers
pip install podman-compose
```

## First-time setup

```bash
make certs    # generate CA + server + dev-client certs
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

## Smoke test

```bash
make example-worker                          # terminal 1
make example-start                           # terminal 2 — copy the workflow ID
make example-approve ID=order-ORD-XXXXXXXX
make example-result  ID=order-ORD-XXXXXXXX
```

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
