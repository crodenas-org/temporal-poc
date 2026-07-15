"""WaaS — Workflow as a Service orchestrator.

WaaS holds NO domain logic. Its workflows are built from generic primitives:
  - load_inputs : read request inputs from the store (thin-payload pattern)
  - api_call    : authenticated HTTP call to a domain service endpoint

Domain business logic lives in the services that own each endpoint (dns-svc,
compute-svc, ...). WaaS only orchestrates them over HTTP (DESIGN.md §6 api_call).
"""
