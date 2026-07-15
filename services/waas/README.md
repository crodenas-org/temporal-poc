# waas — Workflow as a Service orchestrator

Orchestrates domain services into a durable request lifecycle. **WaaS holds no
domain logic.** Its workflows are built from generic primitives:

- `load_inputs` — read request inputs from the store (thin-payload pattern)
- `api_call` — an authenticated HTTP call to a domain-service endpoint (§6)
- `send_notification` — platform notification primitive (§send_notification);
  orchestration-owned, delivery **simulated** (logged) in this POC

Business logic lives in the services that own each endpoint — here `dns-svc`
(IP reservation) and `compute-svc` (VM provisioning). WaaS calls them over HTTP;
it never imports them.

## Sample workflow (`ProvisionWorkflow`)

```
load_inputs(request_id)                      # thin payload -> inputs from store
  -> send_notification (awaiting approval)   # best-effort, non-fatal (simulated)
  -> approval gate (signal)
  -> load_inputs again                       # edit-before-cutoff still applies
  -> api_call POST dns-svc/ip-reservations   # reserve IP        (domain service)
  -> api_call POST compute-svc/vms  (ip ⟵)   # provision VM, fed step-1 output
```

The only Python the worker runs is `load_inputs`, `api_call`, and
`send_notification` — all generic primitives. Everything domain-specific happens
behind an HTTP call to the owning service. The notification recipient defaults to
`WAAS_NOTIFY_TO` and is overridden by an `approver_email` field on the request.

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

Notification specifics still deferred: real delivery (`send_notification` logs
instead of sending), recipient resolution from the approval policy's approver
types, and folding the approver notice into an `approval_gate` primitive that owns
both the notification and the wait.
