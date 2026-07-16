#!/usr/bin/env bash
# Create a Temporal namespace via the stock admin-tools CLI. Used both by the
# one-shot `temporal-defaultns` container (for `default`) and by `make namespace`
# for service namespaces. The custom server image is distroless (no CLI), so all
# namespace ops run from admin-tools (see AUTHZ.md §5).
#
# Retries until the frontend is reachable, so it can start alongside the server.
set -euo pipefail

NS="${1:-${NAMESPACE:-default}}"
ADDR="${TEMPORAL_ADDRESS:-temporal:7233}"
RETENTION="${RETENTION:-72h}"

for i in $(seq 1 30); do
  if temporal operator namespace describe --namespace "$NS" --address "$ADDR" >/dev/null 2>&1; then
    echo ">> namespace '$NS' already exists"
    exit 0
  fi
  if temporal operator namespace create --namespace "$NS" --address "$ADDR" --retention "$RETENTION"; then
    echo ">> created namespace '$NS'"
    exit 0
  fi
  echo ">> waiting for temporal frontend at $ADDR ($i/30)..."
  sleep 2
done

echo "!! timed out creating namespace '$NS'" >&2
exit 1
