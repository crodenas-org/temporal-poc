#!/usr/bin/env bash
# Generates a local dev CA, Temporal server cert, and a default client cert.
# Output goes to infra/temporal/certs/ (gitignored).
# Usage: ./scripts/gen-certs.sh [service-name]
#   service-name defaults to "dev-client"

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="$SCRIPT_DIR/../certs"
SERVICE="${1:-dev-client}"

mkdir -p "$CERTS_DIR"

# CA
if [[ ! -f "$CERTS_DIR/ca.crt" ]]; then
  echo "Generating CA..."
  step certificate create "Temporal Local CA" \
    "$CERTS_DIR/ca.crt" "$CERTS_DIR/ca.key" \
    --profile root-ca \
    --not-after 87600h \
    --no-password --insecure
else
  echo "CA already exists, skipping."
fi

# Server cert — SANs match container hostnames used by workers
echo "Generating server cert..."
step certificate create temporal \
  "$CERTS_DIR/server.crt" "$CERTS_DIR/server.key" \
  --profile leaf \
  --ca "$CERTS_DIR/ca.crt" --ca-key "$CERTS_DIR/ca.key" \
  --san temporal \
  --san localhost \
  --not-after 8760h \
  --no-password --insecure --force

# Client cert for the Temporal UI container
echo "Generating client cert for: temporal-ui..."
step certificate create "temporal-ui" \
  "$CERTS_DIR/temporal-ui.crt" "$CERTS_DIR/temporal-ui.key" \
  --profile leaf \
  --ca "$CERTS_DIR/ca.crt" --ca-key "$CERTS_DIR/ca.key" \
  --san temporal-ui \
  --not-after 8760h \
  --no-password --insecure --force

# Client cert for the requested service
echo "Generating client cert for: $SERVICE..."
step certificate create "$SERVICE" \
  "$CERTS_DIR/$SERVICE.crt" "$CERTS_DIR/$SERVICE.key" \
  --profile leaf \
  --ca "$CERTS_DIR/ca.crt" --ca-key "$CERTS_DIR/ca.key" \
  --san "$SERVICE" \
  --not-after 8760h \
  --no-password --insecure --force

echo ""
echo "Certs written to: $CERTS_DIR"
echo "  CA:          ca.crt"
echo "  Server:      server.crt / server.key"
echo "  UI client:   temporal-ui.crt / temporal-ui.key"
echo "  Dev client:  $SERVICE.crt / $SERVICE.key"
echo ""
echo "Set these in your service .env:"
echo "  TEMPORAL_TLS_CA=\$(pwd)/certs/ca.crt"
echo "  TEMPORAL_TLS_CERT=\$(pwd)/certs/$SERVICE.crt"
echo "  TEMPORAL_TLS_KEY=\$(pwd)/certs/$SERVICE.key"
