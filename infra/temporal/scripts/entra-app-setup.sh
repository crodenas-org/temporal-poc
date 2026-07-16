#!/usr/bin/env bash
# Create/refresh the SINGLE shared Entra app registration for Temporal UI SSO,
# used across all environments (redirect URIs for local/staging/prod all live on
# this one app). App-role `value`s are Temporal-native "<namespace>:<role>"
# strings so the default JWT claim mapper (permissionsClaimName: roles) reads
# them directly — no custom JWT parsing (AUTHZ.md §6).
#
# Idempotent: reuses the app by display name; role IDs are derived
# deterministically from their value so re-runs are stable. Re-run to add a new
# env's redirect URI or new roles without disturbing existing ones.
#
# Requires: `az login` with rights to create app registrations.
# This run also mints a LOCAL client secret into infra/temporal/.env (gitignored,
# NEVER printed). Prod gets its own secret on the same app, stored in Secrets
# Manager. macOS `md5` is assumed (this repo's dev machine).
set -euo pipefail

APP_NAME="${APP_NAME:-temporal-ui}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="${ENV_FILE:-$HERE/../.env}"

# Redirect URIs — this ONE app serves every env, so add staging/prod here as they
# come online. All are registered on the same app registration.
REDIRECT_URIS=(
  "http://localhost:8080/auth/sso/callback"
  # "https://temporal.staging.example/auth/sso/callback"
  # "https://temporal.prod.example/auth/sso/callback"
)

# Temporal-native roles. value == "<namespace>:<role>"; role ∈ read|write|admin|worker.
ROLE_VALUES=("system:admin" "default:read" "default:write" "svc-demo:read")

# Deterministic GUID from a string (stable app-role IDs across re-runs).
guid() { echo -n "$1" | md5 | sed -E 's/(.{8})(.{4})(.{4})(.{4})(.{12})/\1-\2-\3-\4-\5/'; }

TENANT_ID="$(az account show --query tenantId -o tsv)"
SIGNED_IN="$(az ad signed-in-user show --query userPrincipalName -o tsv)"
echo ">> tenant:    $TENANT_ID"
echo ">> signed in: $SIGNED_IN"

# 1. App registration (reuse if present)
APP_ID="$(az ad app list --display-name "$APP_NAME" --query '[0].appId' -o tsv)"
if [ -z "$APP_ID" ]; then
  echo ">> creating app '$APP_NAME'"
  APP_ID="$(az ad app create --display-name "$APP_NAME" \
    --sign-in-audience AzureADMyOrg \
    --web-redirect-uris "${REDIRECT_URIS[@]}" \
    --query appId -o tsv)"
else
  echo ">> reusing app '$APP_NAME' ($APP_ID)"
  az ad app update --id "$APP_ID" --web-redirect-uris "${REDIRECT_URIS[@]}"
fi

# 2. App roles
ROLES_JSON="$(mktemp)"
{
  echo "["
  first=1
  for v in "${ROLE_VALUES[@]}"; do
    [ $first -eq 1 ] || echo ","
    first=0
    printf '  {"allowedMemberTypes":["User","Application"],"description":"Temporal %s","displayName":"%s","isEnabled":true,"value":"%s","id":"%s"}' \
      "$v" "$v" "$v" "$(guid "$v")"
  done
  echo
  echo "]"
} > "$ROLES_JSON"
echo ">> setting app roles: ${ROLE_VALUES[*]}"
az ad app update --id "$APP_ID" --app-roles @"$ROLES_JSON"
rm -f "$ROLES_JSON"

# 3. Service principal (enterprise app) so roles can be assigned
if ! az ad sp show --id "$APP_ID" >/dev/null 2>&1; then
  echo ">> creating service principal"
  az ad sp create --id "$APP_ID" >/dev/null
fi

# 4. Assign the signed-in user to system:admin (so you can log in immediately)
USER_OID="$(az ad signed-in-user show --query id -o tsv)"
SP_OID="$(az ad sp show --id "$APP_ID" --query id -o tsv)"
echo ">> assigning $SIGNED_IN -> system:admin"
az rest --method POST \
  --uri "https://graph.microsoft.com/v1.0/users/$USER_OID/appRoleAssignments" \
  --headers "Content-Type=application/json" \
  --body "{\"principalId\":\"$USER_OID\",\"resourceId\":\"$SP_OID\",\"appRoleId\":\"$(guid system:admin)\"}" \
  >/dev/null 2>&1 && echo "   assigned" \
  || echo "   (already assigned, or needs admin consent — assign via Portal if UI login later 403s)"

# 5. Client secret -> .env (gitignored). Never printed.
echo ">> resetting client secret -> $ENV_FILE"
SECRET="$(az ad app credential reset --id "$APP_ID" --display-name local-dev --years 1 --query password -o tsv)"
touch "$ENV_FILE"
sed -i.bak '/^TEMPORAL_AUTH_/d' "$ENV_FILE" && rm -f "$ENV_FILE.bak"
{
  echo "TEMPORAL_AUTH_TENANT_ID=$TENANT_ID"
  echo "TEMPORAL_AUTH_CLIENT_ID=$APP_ID"
  echo "TEMPORAL_AUTH_CLIENT_SECRET=$SECRET"
} >> "$ENV_FILE"

echo ">> done"
echo "   client id: $APP_ID"
echo "   tenant id: $TENANT_ID"
echo "   secret:    written to $ENV_FILE (not shown)"
