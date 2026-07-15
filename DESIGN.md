# Orchestration Service — High Level Design

## 1. Overview

The Orchestration Service provides **Workflow as a Service (WaaS)** — a platform that lets any authorized team define, publish, and deliver their services through a consistent, durable, auditable request lifecycle. Teams do not write orchestration code. They define *what* their service requires and *how* it should be delivered; the platform executes it.

**What it provides:**
- A service catalog that end users browse to request resources and services
- A workflow engine that durably executes multi-step delivery workflows, surviving failures and waiting days for approvals
- Orchestration-managed primitives for cross-cutting concerns: change management, approvals, notifications, incident management
- Orchestration service accounts for those primitives — teams do not need their own ServiceNow, SMTP, or AD credentials to participate
- A dynamic form contract so any frontend can render request forms from catalog item definitions without custom code

**Who uses it:**
- **Workflow authors** — team members who define catalog items and delivery workflows (Linux team, DNS team, security team, etc.)
- **End users / requesters** — anyone submitting a request through the catalog
- **Approvers** — individuals or group members who act on pending approval gates
- **Orchestration operators** — the orchestration team who own the service and its primitives

---

## 2. Platform Context

The hosting platform is a known quantity: ECS Fargate cluster, ALB with Entra OIDC authentication, Secrets Manager scoped per ECS task role, ECR for images, VPC with private subnets for services and public subnets for the ALB. Each service is an Entra registered application. Each ECS task has its own IAM role and policy. That infrastructure is not designed here.

The platform provides a shared Aurora PostgreSQL cluster that services may opt into — the platform provisions a dedicated database on the shared cluster and injects the connection details. Services are not required to use it; they may provision their own database or use no database at all. The orchestration service uses the shared platform database for its request and audit records.

The orchestration service is one service on that platform. It is the last architectural piece before teams begin decomposing existing systems into the new platform model.

```
End users / FE apps
        │  (Entra token, user identity)
        ▼
Orchestration Service  ←─── workflow authors register catalog items via API
        │  (Entra token, orchestration app identity)
        ├──▶ Linux provisioning API
        ├──▶ DNS provisioning API
        ├──▶ Any registered team API
        │
        ├──▶ ServiceNow          (orchestration service account)
        ├──▶ SMTP / notifications (orchestration service account)
        └──▶ AD / directory      (orchestration service account)

Shared Temporal instance  ←── owned by platform team, used by all services
        │
        ├── namespace: orchestration   (orchestration service workflows)
        └── namespace: team-*          (team-internal workflows, optional)
```

Team provisioning APIs are **headless from the orchestration perspective** — they have no awareness of the orchestration-initiated CHG, approval chain, or workflow. They receive a well-formed payload and provision the resource. Internally, a team service can use the shared core libraries with their own credentials to create CHGs for their own internal processes, call ServiceNow, or run Temporal workflows for complex provisioning sequences in their own namespace — the same libs, BYOC. They are Entra-protected and only reachable within the VPC.

Each environment is a **separate AWS account**. Resource names do not embed environment names. S3 buckets are an exception due to the global namespace requirement — naming policy is `[company-prefix]-[user-selectable-name]-[env]`. No enforced naming convention for ECR repositories at this time.

---

## 3. Core Concepts

| Term | Definition |
|---|---|
| **Catalog item** | A published, requestable service. Defines what inputs are required, what templates apply, what approval policy governs it, and which workflow delivers it. |
| **Workflow definition** | The ordered sequence of steps that delivers a catalog item. References primitives and team API endpoints. Versioned independently of the catalog item. |
| **Request** | A single instance of a catalog item being fulfilled. Created when an end user submits the form. Has a lifecycle from submitted to terminal state. |
| **Primitive** | An orchestration-provided, reusable workflow step. Executes using orchestration service accounts. Examples: `create_chg`, `approval_gate`, `send_notification`. |
| **Template** | A named, reusable content definition bound to a catalog item. Used by primitives at runtime. Examples: CHG templates, email templates, DNS note templates. |
| **Workflow author** | A team member who defines a catalog item and its delivery workflow via the orchestration API. Does not write Temporal code. |
| **Compensation** | The undo activity declared per step. Executed in reverse order during a cancel operation. |

---

## 4. Catalog Item Anatomy

A catalog item is the unit of registration. Workflow authors submit it via the orchestration API.

```yaml
catalog_item:
  name: "Linux VM"
  description: "Provision a standard Linux virtual machine"
  category: "Compute"
  version: 2                          # incremented on each publish
  status: published                   # draft | published | deprecated

  # Catalog item ownership — separate from the requester
  ownership:
    system_owner: jane.doe@company.com
    technical_contact: linux-team@company.com
    business_owner: dept-head@company.com
    cost_center: "CC-1042"

  # Templates — omit a key to use the platform generic; provide a name to override with a custom template
  templates:
    chg: "tmpl-linux-vm-standard"           # custom template registered before this catalog item
    notification_complete: "tmpl-notify-vm-ready"   # custom
    # notification_submitted and notification_rejected omitted — platform generics used

  # Approval policy
  approval:
    steps:
      - id: manager_approval
        type: serial
        approvers:
          - type: resolver
            resolver: manager_of_requester
      - id: security_approval
        type: parallel
        condition: "inputs.environment == 'prod'"
        approvers:
          - type: group
            id: "DL-Security-Approvers"
      - id: cost_approval
        policy: cost_approval             # platform-level named policy; opt-in per catalog item

  # Input schema — drives dynamic form generation
  inputs:
    schema:
      type: object
      required: [hostname, size, disk_gb, environment]
      properties:
        hostname:
          type: string
          title: "Hostname"
          pattern: "^[a-z][a-z0-9\\-]{2,30}$"
        size:
          type: string
          title: "Instance Size"
          enum: ["2x4", "4x8", "8x16"]
        disk_gb:
          type: integer
          title: "Disk (GB)"
          minimum: 20
          maximum: 2000
        environment:
          type: string
          title: "Environment"
          enum: ["dev", "staging", "prod"]
        justification:
          type: string
          title: "Business Justification"
          description: "Required for production requests"
    ui:
      justification:
        widget: textarea
        condition: "inputs.environment == 'prod'"

  # Workflow steps — embedded inline; not a separate registered resource
  workflow:
    steps:
      - id: open_chg
        type: create_chg
        params:
          template: "${catalog.templates.chg}"
          title: "Provision Linux VM: ${inputs.hostname}"
          environment: "${inputs.environment}"

      - id: manager_approval
        type: approval_gate
        ref: "${catalog.approval.steps.manager_approval}"
        params:
          timeout: 48h
          on_timeout: abandon

      - id: security_approval
        type: approval_gate
        ref: "${catalog.approval.steps.security_approval}"
        condition: "${inputs.environment == 'prod'}"
        params:
          timeout: 24h
          on_timeout: abandon

      - id: reserve_ip
        type: api_call
        params:
          url: "${services.dns}/ip-reservations"
          method: POST
          body:
            hostname: "${inputs.hostname}"
            requester: "${request.requester_upn}"
        compensation:
          url: "${services.dns}/ip-reservations/${steps.reserve_ip.output.reservation_id}"
          method: DELETE

      - id: create_netgroup
        type: api_call
        params:
          url: "${services.linux}/netgroups"
          method: POST
          body:
            hostname: "${inputs.hostname}"
        compensation:
          url: "${services.linux}/netgroups/${steps.create_netgroup.output.netgroup_id}"
          method: DELETE

      - id: provision_vm
        type: api_call
        params:
          url: "${services.linux}/vms"
          method: POST
          body:
            hostname: "${inputs.hostname}"
            size: "${inputs.size}"
            disk_gb: "${inputs.disk_gb}"
            ip: "${steps.reserve_ip.output.ip_address}"
            netgroup: "${steps.create_netgroup.output.netgroup_id}"
            tags:
              cost_center: "${request.cost_center}"
              owner: "${request.requester_upn}"
              department: "${request.department}"
        compensation:
          url: "${services.linux}/vms/${steps.provision_vm.output.vm_id}"
          method: DELETE

      - id: close_chg
        type: close_chg
        params:
          chg_id: "${steps.open_chg.output.chg_id}"
          resolution: "Provisioning completed successfully"

      - id: notify_complete
        type: send_notification
        params:
          template: "${catalog.templates.notification_complete}"
          to: "${request.requester_upn}"
          data:
            hostname: "${inputs.hostname}"
            ip: "${steps.reserve_ip.output.ip_address}"
            vm_id: "${steps.provision_vm.output.vm_id}"

    on_failure:
      - type: create_incident
        params:
          title: "VM provision failed: ${inputs.hostname}"
          linked_chg: "${steps.open_chg.output.chg_id}"
          assignment_group: "${catalog.ownership.technical_contact}"
      - type: wait_for_incident_resolution
        params:
          timeout: 72h
          on_timeout: abandon
```

### Templates

The orchestration service ships a set of **platform generic templates** covering common notifications and change records. These are available to all catalog items at no cost to the author — if a template key is omitted from the catalog item, the platform generic is used.

When the generic content is not appropriate, the workflow author registers a **custom template** via the authoring API before submitting the catalog item. Custom templates are scoped to the author's catalog item — they are not shared with or reused by other catalog items. The catalog item references the custom template by the name the author assigned at registration.

A catalog item can mix: use the platform generic for some template slots, override others with a custom template.

---

### Standard platform fields

Collected on every request regardless of catalog item. Pre-populated where possible from the requester's Entra profile. Not defined by the workflow author.

| Field | Source |
|---|---|
| `requester_upn` | Entra token |
| `requester_display_name` | Entra token |
| `cost_center` | Entra profile attribute |
| `department` | Entra profile attribute |
| `manager_upn` | Entra profile / Graph API |
| `submitted_at` | Platform, at trigger time |

### Publish-time validation

When a catalog item is published the orchestration service validates the full payload before accepting it. The service parses the embedded workflow steps, extracts every `${catalog.templates.*}` expression, and verifies each named template exists — either as a platform generic or as a custom template the author has already registered. All pieces must be present before the catalog item can be published.

Additional checks at publish time:
- All step types are known primitives or resolvable service endpoints
- All `${inputs.*}` and `${steps.*}` expressions reference fields and steps that exist in the definition
- Approval group IDs and resolver types are valid

Publication is rejected if any check fails. Missing templates and broken expressions are caught before the catalog item is ever visible to end users.

### Versioning

- A catalog item has one active version at any time
- Authors work in `draft` status until ready to publish
- Publishing increments the version and sets status to `published`
- A previous version can be restored as the active version; only one version is ever active
- In-flight requests always complete on the version they were triggered against — the orchestration service stores a snapshot of the full catalog item payload (including embedded workflow steps) at trigger time, not a pointer to current

---

## 5. Workflow Step Format

Workflow steps are embedded directly in the catalog item payload under the `workflow.steps` key. They are not a separate registered resource. The full catalog item — metadata, inputs, approval policy, templates, and steps — is submitted and versioned as one unit.

The complete example is shown in §4. This section documents the expression syntax and available step types.

### Expression syntax

Steps reference prior outputs and request context via `${}` expressions:
- `${inputs.*}` — requester-provided form fields
- `${request.*}` — platform standard fields (requester_upn, cost_center, etc.)
- `${steps.<id>.output.*}` — output from a prior step
- `${catalog.*}` — catalog item metadata (templates, ownership)
- `${services.*}` — base URLs from orchestration service configuration

### Step types

| Type | Description |
|---|---|
| `create_chg` | Opens a change record using the orchestration service account and the referenced template |
| `update_chg` | Adds a work note or progress comment to an open CHG mid-workflow |
| `close_chg` | Closes a change record with an outcome (success, cancelled, failed) |
| `approval_gate` | Suspends execution, notifies approvers, resumes on signal |
| `api_call` | Authenticated HTTP call to any internal service endpoint |
| `send_notification` | Email or messaging notification using a template |
| `create_incident` | Creates an INC in the ITSM system |
| `wait_for_incident_resolution` | Polls INC status, resumes workflow when INC is closed |
| `condition` | Branches based on expression evaluation |
| `parallel` | Fans out multiple steps, waits for all to complete |
| `wait` | Pauses for a fixed duration or until a specified time |
| `transform` | Reshapes data between steps without an external call |

---

## 6. Primitive Library

Primitives are orchestration-maintained Temporal activities. They execute using orchestration service credentials — workflow authors do not supply credentials for these.

### `approval_gate`
Suspends the workflow and notifies approvers. Resumes when all required approvals are received or the timeout is reached.

Supports:
- **Serial** — approvers notified in order; each must approve before the next is notified
- **Parallel** — all approvers notified simultaneously; all must approve
- **Conditional** — step is skipped entirely if condition evaluates to false at runtime

### `create_chg`
Opens a change record in the ITSM system using the orchestration service account. The CHG template referenced in the catalog item controls format, categorization, and required fields. The CHG is branded as the orchestration service — teams do not need their own ITSM service accounts. Returns a `chg_id` available to subsequent steps via `${steps.<id>.output.chg_id}`.

### `update_chg`
Adds a work note or progress comment to an open CHG. Used mid-workflow to keep the CHG as a living record of what happened during provisioning — IP reserved, VM provisioned, approval received — rather than just an open/close bookend. Useful for audit trail within the ITSM system independent of the orchestration service's own audit log.

### `close_chg`
Closes a change record with an explicit outcome. Accepts an `outcome` parameter: `success`, `cancelled`, or `failed`. The closure code and resolution notes written to the CHG reflect the outcome — a failed or rejected workflow closes differently than a successful one.

### `create_incident`
Creates an INC and assigns it to the catalog item's technical contact group. Typically used in `on_failure` blocks. Links to the associated CHG if one exists.

### `wait_for_incident_resolution`
Polls the INC until it reaches a resolved/closed state, then automatically signals the workflow to resume. Closes the loop between workflow failure and manual remediation. Configured timeout triggers abandonment if the INC is not resolved in time.

### `send_notification`
Sends email or messaging notifications using a template defined in the catalog item. Executed using orchestration service credentials. Template data is populated from request context and step outputs.

### `api_call`
Authenticated HTTP call to any internal service endpoint — provisioning, lookups, status checks, or any other operation the workflow needs. The orchestration service calls using its Entra app identity — the target service must have granted API permission to the orchestration app registration. The originating requester's UPN is passed in the request payload as metadata, not as an auth token.

### `condition`
Evaluates an expression against input fields or prior step outputs and branches the workflow. Supports simple field comparisons at definition time; runtime expression evaluation against step outputs.

### `parallel`
Fans out a set of steps and waits for all to complete before proceeding. Used for concurrent provisioning steps that have no dependency on each other.

### `wait`
Pauses the workflow for a fixed duration or until a datetime. Useful for scheduled follow-up steps or enforced cooling-off periods.

---

## 7. Approval Model

### Approver types

| Type | Description |
|---|---|
| `person` | Named individual by UPN |
| `group` | AD group; any member may approve |
| `resolver` | Orchestration-resolved identity (e.g. `manager_of_requester`) |
| `input` | Requester-selected individual; UPN provided via form field at submit time |

### Approval structure

Approval steps are declared on the catalog item and referenced by workflow steps. A catalog item may have multiple approval steps executed in the sequence defined by the workflow. The catalog item controls the ordering of all steps, including named platform policies.

**Serial** — approvers are notified in order. Each must act before the next receives notification.

**Parallel** — all approvers notified simultaneously. All must approve for the gate to pass.

**Conditional** — the entire approval step is skipped if the condition evaluates false at trigger time.

### Named approval policies

The orchestration service defines platform-level approval policies for org-wide concerns. Catalog items opt in by referencing a policy by name. Not all catalog items are required to include any given policy.

When a catalog item references a named policy, the orchestration service automatically injects the policy's required input fields into the request form — the workflow author does not declare them. The policy owns the field definitions.

**`cost_approval`** — requires the requester to select a named cost approver at submit time. The selected person receives a notification and must approve before the workflow proceeds. This policy always involves a specific individual; there is no group or DL fallback.

```yaml
# catalog item opts in — ordering relative to other steps is author-controlled
approval:
  steps:
    - id: manager_approval
      type: serial
      approvers:
        - type: resolver
          resolver: manager_of_requester

    - id: cost_approval
      policy: cost_approval       # injects cost_approver person-picker field into the form
```

The injected `cost_approver` field uses a `person_picker` UI widget so frontends render a directory search rather than free text. Any person in the directory is a valid selection — there is no role or group restriction. The value is validated as a known directory UPN before the request is accepted.

For custom inline approval steps that use `type: input`, the workflow author may declare a `source` on the UI hint to restrict the picker to a list provided by the service team's own API. When `source` is absent the platform directory is used. When `source` is present the frontend populates the picker from that endpoint, and the platform validates the submitted UPN against the same endpoint at submit time — preventing a caller from bypassing the picker with an arbitrary UPN.

```yaml
# custom inline approval with a service-provided approver list
inputs:
  schema:
    properties:
      technical_approver:
        type: string
        title: "Technical Approver"
        format: upn
  ui:
    technical_approver:
      widget: person_picker
      source: "${services.linux}/approvers"   # service-provided list; omit for full directory

approval:
  steps:
    - id: technical_approval
      type: serial
      approvers:
        - type: input
          field: inputs.technical_approver
```

The approval step references the input field the same way regardless of where the list originates — the `source` is a concern of the form rendering and validation layer, not the approval gate itself.

Named policies support a `pre_approved_if` condition (not yet defined) that, when true at trigger time, bypasses the approval gate entirely. The criteria for pre-approval of cost requests have not been determined.

### Self-approval

**Open design question.** When the resolved approver identity matches the requester identity, the correct behavior is not yet defined. This is a concrete scenario for `cost_approval` — a requester could enter their own UPN as the cost approver. Escalation rules, secondary approver requirements, and whether enforcement is platform-level or policy-level require further design. This must be resolved before the approval gate primitive is built.

### Notification

Approvers are notified when an approval gate activates. The delivery mechanism is abstracted — the approval gate triggers a notification event and the orchestration service delivers it via one or more configured channels. The approve/reject action is always an API call (`POST /requests/{id}/approvals/{step_id}/approve`) regardless of delivery channel; the notification carries whatever link or inline action the channel supports.

Planned delivery mechanisms (v1 starts with one, others added without changing workflow definitions):
- Email
- Microsoft Teams
- Orchestration portal (pending approval inbox)
- SMS (escalation / timeout scenarios)

Delivery preference can be configured at the orchestration service level, the catalog item level, or per approver.

---

## 8. Request Lifecycle

### States

```
submitted
    │
    ▼
pending_approval       ← paused at one or more approval gates
    │
    ▼
provisioning           ← steps executing
    │
    ├──▶ completed     ← all steps succeeded
    ├──▶ failed        ← step failed, INC created, waiting for resolution
    ├──▶ cancelled     ← cancel requested, compensation running
    └──▶ abandoned     ← terminated without compensation
```

### Cancel

A cancel signal can be sent to any request that has not reached a terminal state. Once received:
1. The current step is allowed to complete or time out
2. Compensation activities are executed in reverse order for all completed steps that declared one
3. The CHG is closed as cancelled
4. The requester is notified

Cancel is only meaningful before provisioning is complete. The orchestration service enforces this — a cancel signal after `completed` is rejected.

### Resume

When a step fails, the workflow parks in `failed` state and an INC is created. Two resume paths:

**Auto-resume** — `wait_for_incident_resolution` primitive polls the INC. When it is closed, the workflow automatically retries the failed step and continues. No human signal to the platform required.

**Manual resume** — an operator sends an explicit resume signal via the orchestration API after resolving the external condition. The workflow retries from the failed step.

### Abandon

Terminates the workflow immediately. No compensation is run. An INC is created if one does not already exist. The associated CHG is closed as failed. Used when the workflow is stuck in a state where compensation would be harmful or meaningless. Manual cleanup of any partial provisioning is the responsibility of the owning team.

---

## 9. Auth & Credentials

### Service-to-service auth

Every service on the platform is an Entra registered application. The orchestration service holds API permissions granted by each team's app registration.

```
End user → Orchestration   Entra user token            orchestration validates identity
Orchestration → Linux API  Entra app token, app role   Linux team grants role to orchestration app
Orchestration → DNS API    Entra app token, app role   DNS team grants role to orchestration app
Orchestration → ServiceNow Orchestration service acct  stored in Secrets Manager, orchestration ECS role
Orchestration → SMTP       Orchestration service acct  stored in Secrets Manager, orchestration ECS role
```

### App roles

Each team's app registration defines app roles representing logical access levels rather than individual endpoint permissions. The orchestration service is assigned one role per target service — that role covers all endpoints the orchestration service is permitted to call.

Example — Linux team app registration:

```
App roles:
  linux.provision   — POST /vms, POST /netgroups, POST /sudo-grants, etc.
  linux.read        — GET endpoints only
  linux.admin       — full access including decommission endpoints
```

The orchestration app registration requests `linux.provision`. The Linux API validates the role on the incoming token — not the specific endpoint being called. One role grants access to the full provisioning surface.

The orchestration service's accumulated permissions across all team services:

```
API permissions held by orchestration app registration:
  linux-api     → linux.provision
  dns-api       → dns.provision
  windows-api   → windows.provision
  ...
```

The `api_call` primitive acquires a token scoped to the target service's app ID. The role is already embedded in what was granted — no per-call role selection needed. The call either succeeds or returns 403.

**Governance:** each team explicitly assigns the orchestration app registration their role in their Entra app manifest. That assignment is the team's consent to allow orchestration to call their service. It is auditable, revocable, and owned by the team.

### Originating user identity

When the orchestration service calls a team provisioning API, the end user's identity is not forwarded as an auth token. The team API authenticates the orchestration service's app identity. The originating requester's UPN and other standard fields are included in the request payload as data — for resource tagging, audit, and ownership attribution on the provisioned resource.

### Secrets

Each ECS task role is scoped to its own Secrets Manager path by the platform. The orchestration service accesses its credentials (ITSM, SMTP, AD) via its task role. Team services access their credentials via their own task role. Credentials to external systems are stored in Secrets Manager regardless of origin — even third-party vaults are accessed via a Secrets Manager reference. Secret resolution in workflow step execution follows the same pattern: the orchestration worker resolves secrets at activity execution time using its task role.

### BYOC vs orchestration credentials

The shared core libraries support both modes. When a team calls a shared lib from their own service with their own creds, they supply the secret reference. When the orchestration service calls the same lib as a primitive activity, it supplies its own orchestration credentials. The lib does not distinguish — credential sourcing is the caller's concern.

---

## 10. Audit & Compliance

The orchestration service maintains its own database (a dedicated database on the platform's shared Aurora PostgreSQL cluster) as the authoritative record of requests and their history. Temporal's execution history is the source of truth for workflow execution, but is not the primary query surface for compliance or reporting.

### What is recorded

| Record | Contents |
|---|---|
| Request | request_id, catalog_item, version snapshot, requester_upn, submitted_at, current_state, terminal_state, terminal_at |
| Step execution | request_id, step_id, step_type, started_at, completed_at, status, input snapshot, output snapshot |
| Approval decision | request_id, step_id, approver_upn, decision (approved/rejected), decided_at, comment |
| Ticket reference | request_id, step_id, ticket_type (CHG/INC), ticket_id, created_at, closed_at |

### API endpoints for status and history

```
GET  /requests                          list requests (filterable by state, requester, catalog item, date)
GET  /requests/{id}                     full request detail with current state
GET  /requests/{id}/steps               step-by-step execution history
GET  /requests/{id}/approvals           approval decisions and pending gates
GET  /requests/{id}/tickets             CHG and INC references

POST /requests/{id}/cancel              request cancellation
POST /requests/{id}/resume              manual resume signal
POST /requests/{id}/abandon             abandon without compensation

POST /requests/{id}/approvals/{step_id}/approve
POST /requests/{id}/approvals/{step_id}/reject
```

Structured JSON logs are emitted to CloudWatch for every state transition, step execution, and approval decision. These are the compliance audit trail and are independent of the API.

---

## 11. API Surface

The orchestration service is API-first. All functionality is exposed via the API. A frontend or BFF adapter consumes this API but is independent of it.

### Catalog API (end user surface)
```
GET  /catalog                           list all published catalog items by category
GET  /catalog/{item_id}                 catalog item detail including full input schema
GET  /catalog/{item_id}/schema          JSON Schema for the item's input form (dynamic form contract)
POST /catalog/{item_id}/requests        submit a request; returns request_id
```

### Request API (end user surface)
```
GET  /requests                          requester's request history
GET  /requests/{id}                     request status and detail
GET  /requests/{id}/steps               execution timeline
POST /requests/{id}/cancel
POST /requests/{id}/resume
POST /requests/{id}/approvals/{step_id}/approve
POST /requests/{id}/approvals/{step_id}/reject
```

### Approval inbox (approver surface)
```
GET  /approvals/pending                 all approval gates pending the caller's action
```

### Authoring API (workflow author surface)
```
GET    /definitions/catalog-items                      list all catalog items the caller owns
POST   /definitions/catalog-items                      register or update a catalog item (draft; workflow steps included in payload)
POST   /definitions/catalog-items/{id}/publish         publish current draft
POST   /definitions/catalog-items/{id}/rollback        restore previous published version

GET    /definitions/templates                           list available templates (platform generics + caller's custom templates)
POST   /definitions/templates                          register a custom template (must exist before referencing catalog item is published)

GET    /definitions/catalog-items/{id}/validate        validate definition against schema
```

### Reference API (form data surface)
```
GET  /reference/people?q={query}        directory person search; used by person_picker fields
GET  /reference/groups?q={query}        AD group search
```

Reference endpoints are read-only lookups that populate dynamic form fields. They are called by the frontend at form render time, not at request submit time. Access is gated by the user's Entra token — no elevated permissions are used. Additional reference endpoints are added here as platform-level named policies require them.

### Dynamic form contract

`GET /catalog/{item_id}/schema` returns a JSON Schema document plus UI hints. Any frontend that can render JSON Schema can generate a functional, validated form for any catalog item without custom code. Hosting platform standard fields (cost center, department) are pre-populated from the Entra token by the API — the form schema only includes fields the requester must explicitly provide.

UI hints may include a `source` URL on `person_picker` and other dynamic fields. When `source` points to `/reference/*` the orchestration API serves the list. When `source` points to a service team endpoint (e.g. `${services.linux}/approvers`) the frontend calls that service directly. The frontend treats `source` as an opaque URL in both cases.

---

## 12. Shared Libraries

The hosting platform provides shared core Python libraries available to all services. The orchestration service consumes these as Temporal activity implementations.

| Library | Provides |
|---|---|
| `temporal-client` | `get_client()`, `build_worker()` — Temporal connection for any service |
| `platform-aws` | AWS service abstractions (S3, EC2, Secrets Manager, etc.) |
| `platform-entra` | Entra token acquisition, Graph API queries, group membership |
| `platform-itsm` | ServiceNow CHG and INC create/update/close |
| `platform-notifications` | Email and messaging with template rendering |
| `platform-dns` | DNS record and IP reservation abstractions |
| `platform-ad` | AD group management, user lookups |
| `platform-db` | Database connection factory |

All libraries support BYOC — the caller supplies a secret reference and the lib resolves it from Secrets Manager at call time. When the orchestration service calls these libs as primitive activities it supplies orchestration-scoped secret references. When a team calls these libs from their own service they supply their own secret references scoped to their task role.

### Reference endpoint convention

Any service in the monorepo that defines catalog items with dynamic form fields owns the reference endpoints that back those fields. These are standard read-only API endpoints on the service — there is nothing orchestration-specific about them beyond the fact that their URLs appear in `source` fields in catalog item UI hints.

Platform convention for reference endpoints:

- Read-only, no side effects
- Accept a `q` query parameter for search/filter where the field is a picker
- Return a JSON array: `[{ "value": "<upn or id>", "label": "<display name>" }]`
- Gated by the service's normal Entra token validation — the user's token, not an elevated service token

The orchestration API's `/reference/*` endpoints follow this same convention. A service team implementing their own reference endpoints (e.g. `GET /approvers?q=`) should match this shape so any frontend consuming the dynamic form contract handles all `source` URLs uniformly.

---

## 13. AI-Assisted Authoring

The YAML-based authoring model is intentionally well-suited to AI assistance. The workflow definition format has a defined schema, a finite primitive library, and a constrained expression language — all of which are things a language model can learn and reliably produce output against.

### AI-assisted catalog item drafting

A workflow author describes what they want to offer in plain language. An AI assistant drafts the complete catalog item YAML, workflow YAML, and CHG template. The author reviews, adjusts, and submits via the authoring API. The schema validation on publish catches structural errors before anything runs.

```
Author: "Sudo access request for a Linux server. Requester provides hostname
         and justification. Manager must approve. If the server is production,
         security team also approves. Linux team API grants access."

AI: drafts catalog_item.yaml, workflow.yaml, chg-template.j2

Author: reviews approval group names, adjusts timeouts, submits
```

The author is a reviewer and tuner, not a writer. This is the right division of labor for a structured format with known constraints.

### MCP server

The orchestration service exposes an MCP (Model Context Protocol) server alongside its REST API. This allows any MCP-compatible AI assistant to interact with the orchestration service directly as a set of tools — reading the catalog, drafting definitions, validating and submitting them, and checking request status.

Proposed MCP tools:

| Tool | Description |
|---|---|
| `list_catalog_items` | Browse published catalog items by category |
| `get_primitive_library` | Return the full primitive library with parameter schemas — gives the AI the vocabulary it can use in workflow steps |
| `get_definition_schema` | Return the JSON Schema for catalog item and workflow definitions — the grammar the AI must conform to |
| `draft_catalog_item` | Given a natural language description, produce a draft catalog item and workflow YAML |
| `validate_definition` | Validate a draft definition against the schema and return errors |
| `submit_definition` | Submit a validated draft via the authoring API |
| `list_requests` | Query request history and status |
| `get_request` | Get full detail and execution timeline for a request |
| `get_pending_approvals` | List approval gates pending the caller's action |
| `approve_request` | Send an approval decision |

Authentication to the MCP server follows the same Entra token model as the REST API — the AI assistant acts on behalf of the authenticated user, not with elevated permissions.

### Longer-term: AI agent for service onboarding

Once the authoring API and MCP server are stable, the natural next step is an AI agent that a team can describe their service to and receive a complete, ready-to-review catalog item package. The agent would:

1. Ask clarifying questions about inputs, approval requirements, and the team's provisioning API
2. Draft the catalog item, workflow definition, and templates
3. Validate against the schema
4. Present the draft for human review
5. Submit on confirmation

This is straightforward to build once the YAML format is stable and the MCP server exists. The constrained expression language is an advantage here — a smaller, well-defined grammar produces more reliable AI output than an open-ended one, which is another reason to define its ceiling deliberately.

---

## 14. Implementation Status & Deferred Gaps

A local POC exists under `services/`: `waas` (orchestrator) plus `dns-svc` and `compute-svc` (the trivial domain services it orchestrates). It proves the core model end to end — thin-payload requests (workflow carries only `request_id`), DB-authoritative inputs, edit-before-approval, an approval gate, and orchestration of domain services purely via the `api_call` primitive with no domain logic in WaaS.

**Largest gap — the workflow step interpreter is not built.** §4/§5 describe workflows as *data*: a `workflow.steps` list interpreted at runtime. The POC instead **hardcodes each workflow in Python** (`waas/workflows.py::ProvisionWorkflow`). Every new offering therefore requires new Python, which contradicts the §1 promise that teams do not write orchestration code. The deferred piece is a single generic workflow that (1) loops the step list, (2) dispatches on `step.type` to the primitive library (§6), (3) resolves `${inputs.*}` / `${steps.<id>.output.*}` / `${services.*}` expressions (§5), and (4) accumulates step outputs into a runtime context. This interpreter is what turns WaaS from per-offering code into a data-driven platform; it is the central thing still to build.

Other POC deferrals (endpoint/model *shapes* are correct; internals are thin):

- Freeze/cutoff + a `draft` request state for edit-before-approval (pending §8 amendment)
- JSON Schema validation of request inputs against the catalog item (§4)
- Entra token on `api_call`; end-user auth on the API (§9)
- Compensation execution (the domain `DELETE` hooks exist but are not wired)
- Real Postgres (SQLite stands in) and the authoring API (§11)
- mTLS disabled in the local stack for dev convenience — re-enable via `compose.yml` + client `TEMPORAL_TLS_*`

---

## 15. Open Design Questions

**Self-approval** — when the resolved approver identity matches the requester identity, behavior is undefined. Most concrete for `cost_approval` where the requester selects the approver directly. Escalation rules, secondary approver policy, and whether this is platform-enforced or policy-level require further design.

**Named policy pre-approval criteria** — `cost_approval` and future named policies will support a `pre_approved_if` condition that bypasses the gate when true at trigger time. The conditions for cost pre-approval have not been defined.

**Rollback completeness** — compensation activities cover the happy-path undo case. Partial failures mid-compensation (compensation step itself fails) and workflows where some steps have no meaningful compensation need defined handling.

**Workflow definition format edge cases** — complex conditional branching, loops, and dynamic step generation from prior step output are not covered by the current step model. The boundary between what is expressible in the definition format and what requires a team-internal Temporal workflow needs to be drawn explicitly.

---

## 16. Out of Scope for v1

- **Frontend portal** — a separate application consuming the catalog and request APIs. The API provides everything needed; the portal is a consumer of it.
- **BFF adapter** — an optional facade between a specific frontend and the orchestration API. Independent of the API itself.
- **Metrics and analytics** — request volume, approval SLA tracking, step failure rates, catalog item usage. The data model supports it; the reporting surface is deferred.
- **Scheduled workflows** — catalog items triggered on a schedule rather than by a requester. The Temporal infrastructure supports it; the authoring model for it is deferred.
- **Cross-team Temporal child workflows** — one team's workflow directly invoking another team's Temporal workflow as a child workflow. Cross-team composition is supported via `api_call` to the owning team's API, which is the correct pattern and preserves credential and ownership boundaries.
- **External webhook triggers** — initiating a request from an external event source.
