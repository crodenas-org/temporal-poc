#!/usr/bin/env bash
# Set the signed-in user's app-role assignments on the temporal-ui app to
# EXACTLY the roles passed as args (existing assignments for this app are
# removed first). Used to test RBAC enforcement by downgrading from system:admin
# to a scoped role and confirming access actually changes.
#
# Usage:
#   entra-assign-role.sh default:read              # only default:read
#   entra-assign-role.sh default:read svc-demo:read
#   entra-assign-role.sh                           # no roles -> no access
#
# After running, sign out and back in (or use a fresh incognito window) so a new
# token is issued with the updated roles.
set -euo pipefail

APP_NAME="${APP_NAME:-temporal-ui}"
guid() { echo -n "$1" | md5 | sed -E 's/(.{8})(.{4})(.{4})(.{4})(.{12})/\1-\2-\3-\4-\5/'; }

APP_ID="$(az ad app list --display-name "$APP_NAME" --query '[0].appId' -o tsv)"
SP_OID="$(az ad sp show --id "$APP_ID" --query id -o tsv)"
USER_OID="$(az ad signed-in-user show --query id -o tsv)"
UPN="$(az ad signed-in-user show --query userPrincipalName -o tsv)"
echo ">> user: $UPN"

# Remove existing assignments for this app
az rest --method GET \
  --uri "https://graph.microsoft.com/v1.0/users/$USER_OID/appRoleAssignments" \
  --query "value[?resourceId=='$SP_OID'].id" -o tsv | while read -r AID; do
    [ -n "$AID" ] || continue
    echo ">> removing existing assignment $AID"
    az rest --method DELETE \
      --uri "https://graph.microsoft.com/v1.0/users/$USER_OID/appRoleAssignments/$AID" >/dev/null
done

# Add the requested roles
for role in "$@"; do
  echo ">> assigning $role"
  az rest --method POST \
    --uri "https://graph.microsoft.com/v1.0/users/$USER_OID/appRoleAssignments" \
    --headers "Content-Type=application/json" \
    --body "{\"principalId\":\"$USER_OID\",\"resourceId\":\"$SP_OID\",\"appRoleId\":\"$(guid "$role")\"}" \
    >/dev/null
done

echo ">> done. Roles now: ${*:-<none>}"
echo ">> sign out of the UI and back in (fresh incognito is easiest) for a new token."
