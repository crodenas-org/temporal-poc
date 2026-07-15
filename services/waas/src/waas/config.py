"""Domain service base URLs (DESIGN.md §5 `${services.*}`).

Read from env with local-dev defaults. In production these resolve to the
in-VPC domain service endpoints; here they point at the local uvicorn apps.
"""
import os

DNS_URL = os.environ.get("SERVICES_DNS_URL", "http://localhost:8010")
COMPUTE_URL = os.environ.get("SERVICES_COMPUTE_URL", "http://localhost:8011")

# Notification defaults (POC). In the mature model the recipient is resolved from
# the approval policy's approver types (DESIGN.md §Approver types); here we take a
# hardcoded default and let a request supply `approver_email` to override.
NOTIFY_TO = os.environ.get("WAAS_NOTIFY_TO", "platform-approvers@example.com")

# Base URL for the approver-facing links embedded in notifications.
UI_BASE = os.environ.get("WAAS_UI_BASE", "http://localhost:8080/namespaces/default/workflows")
API_BASE = os.environ.get("WAAS_API_BASE", "http://localhost:8004")
