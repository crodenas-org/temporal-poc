# waas — Workflow as a Service orchestrator

Orchestrates domain services into a durable request lifecycle. **WaaS holds no
domain logic.** Its workflows are built from two generic primitives:

- `load_inputs` — read request inputs from the store (thin-payload pattern)
- `api_call` — an authenticated HTTP call to a domain-service endpoint (§6)

Business logic lives in the services that own each endpoint — here `dns-svc`
(IP reservation) and `compute-svc` (VM provisioning). WaaS calls them over HTTP;
it never imports them.

## Sample workflow (`ProvisionWorkflow`)

```
load_inputs(request_id)                      # thin payload -> inputs from store
  -> approval gate (signal)
  -> load_inputs again                       # edit-before-cutoff still applies
  -> api_call POST dns-svc/ip-reservations   # reserve IP        (domain service)
  -> api_call POST compute-svc/vms  (ip ⟵)   # provision VM, fed step-1 output
```

The only Python the worker runs is `load_inputs` and `api_call`. Everything
domain-specific happens behind an HTTP call to the owning service.

## Run the demo

Temporal stack up (`cd infra/temporal && make up`), then five processes:

```bash
# domain services (own the logic)
cd services/dns-svc     && make install && make api      # :8010
cd services/compute-svc && make install && make api      # :8011

# waas
cd services/waas && make install
make worker                                              # shell: watch orchestration logs
make api                                                 # :8004

# drive it
make create-request          # POST /catalog/linux-vm/requests {hostname: web01} -> request_id
make approve ID=<request_id> # POST /requests/<id>/approvals/approval/approve
make status  ID=<request_id>
```

The worker log shows the two `api_call`s hitting dns-svc then compute-svc, and
the result carries `reservation_id`, `ip_address`, and `vm_id` — all produced by
the domain services, orchestrated by WaaS.

## Deferred (POC scope)

Data-driven step interpretation from a catalog definition, freeze/cutoff, schema
validation, Entra auth on api_call, compensation, real Postgres. The workflow
here is hand-written; the eventual model reads steps from the catalog item (§4/§5).
