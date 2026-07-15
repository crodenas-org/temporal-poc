"""Domain service base URLs (DESIGN.md §5 `${services.*}`).

Read from env with local-dev defaults. In production these resolve to the
in-VPC domain service endpoints; here they point at the local uvicorn apps.
"""
import os

DNS_URL = os.environ.get("SERVICES_DNS_URL", "http://localhost:8010")
COMPUTE_URL = os.environ.get("SERVICES_COMPUTE_URL", "http://localhost:8011")
