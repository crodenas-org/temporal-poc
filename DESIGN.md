# Workflow-as-a-Service Platform Design

## Vision

A shared orchestration platform that lets internal teams expose their services and compose them into durable, auditable workflows — without writing orchestration code. Teams define workflows declaratively; the platform renders forms, executes steps, handles approvals, and manages failures.

---

## Platform Architecture

```
Platform Infrastructure (platform team owned)
├── Temporal cluster           — platform primitive, like ECS or RDS
│   ├── namespace: platform    — platform/onboarding workflows
│   ├── namespace: orch-app    — orchestration app internal workflows
│   ├── namespace: team-*      — one per team, provisioned on onboarding
│   └── ...
└── Temporal host injected into every ECS service as TEMPORAL_HOST

Platform Orchestration Service (ECS, orchestration team owned)
├── API layer              — trigger workflows, query status, handle signals
├── Service catalog UI     — self-service portal, dynamic forms, approval inbox
├── Primitive library      — reusable activities (approval, ITSM, email, HTTP)
├── Service registry       — registered team endpoints + auth config
└── Workers
    ├── orch-app namespace     — orchestration app's own internal workflows
    └── team-* namespaces      — WaaS workflows requested via the service catalog
                                 (dynamically served; teams don't need their own workers)

Team ECS Service (optional worker)
└── team-* namespace       — team's own internal workflows, if they choose to run them
```

**Temporal is a platform primitive** — owned and operated by the platform team alongside ECS and URL namespace provisioning. The orchestration app connects to it the same way any other team would, with no special treatment.

---

## Worker Ownership Model

This is the key design decision that shapes everything else:

| Workflow type | Namespace | Worker |
|---|---|---|
| Platform onboarding/offboarding | `platform` | Platform team worker |
| Orchestration app internal workflows | `orch-app` | Orchestration app worker |
| WaaS workflow requested via service catalog | `team-{name}` | Orchestration app worker (dynamic) |
| Team's own internal workflows | `team-{name}` | Team's own worker (optional) |

**WaaS workflows run in the requesting team's namespace but are executed by the orchestration app's worker.** This means:
- Teams get isolated history and visibility without needing to run workers
- The orchestration app's worker dynamically connects to each team namespace as WaaS workflows are registered
- Teams that want their own internal orchestration can run their own worker in their namespace alongside the orchestration worker — task queue naming prevents collision

```
namespace: team-data-eng
├── task queue: waas              ← orchestration app's worker listens here
└── task queue: data-eng-internal ← team's own worker listens here (optional)
```

---

## Isolation Model

Each team gets a dedicated Temporal namespace. This provides:

- Isolated workflow history and execution state
- Independent retention policies
- Separate visibility in the UI
- No cross-team data leakage

Namespaces are created on team onboarding and deprecated on offboarding.

---

## Team Onboarding / Offboarding

Namespace lifecycle is automated via platform workflows running in the `platform` namespace.

**Onboarding** (triggered when a team is provisioned on the platform):
1. Create ECS service + URL namespace (existing platform flow)
2. Create Temporal namespace for the team
3. Inject `TEMPORAL_NAMESPACE` into the team's ECS environment
4. Register team in the service catalog

**Offboarding**:
1. Drain or terminate in-flight workflows (configurable grace period)
2. Delete schedules
3. Deprecate namespace (data expires per retention policy)
4. Remove from service catalog

---

## Workflow Definition Format

Teams author YAML workflow definitions. The platform parses and executes them as durable Temporal workflows — teams do not write Temporal code.

```yaml
name: provision-database
namespace: team-data-eng
version: 1
description: "Provision a managed database instance"

inputs:
  - id: environment
    label: "Target Environment"
    type: select
    options: [dev, staging, prod]
    required: true

  - id: db_name
    label: "Database Name"
    type: text
    pattern: "^[a-z][a-z0-9_]{2,30}$"
    required: true

  - id: size
    label: "Instance Size"
    type: select
    options: [small, medium, large]
    default: small

  - id: justification
    label: "Business Justification"
    type: textarea
    required: true
    visible_when: "inputs.environment == 'prod'"

steps:
  - id: open_chg
    type: create_change_request
    params:
      title: "Provision DB: ${inputs.db_name}"
      environment: "${inputs.environment}"

  - id: manager_approval
    type: approval_gate
    params:
      approvers: ["${inputs.manager_email}"]
      timeout: 48h

  - id: provision
    type: service_call
    params:
      service: team-data-eng
      endpoint: /databases
      method: POST
      body: "${inputs}"

  - id: notify
    type: send_notification
    params:
      to: "${inputs.requester_email}"
      message: "Database ready: ${steps.provision.output.connection_string}"

on_failure:
  - type: create_incident
    params:
      title: "DB provision failed: ${inputs.db_name}"
      linked_chg: "${steps.open_chg.output.chg_id}"
```

### Input Field Types

| Type | Use case |
|---|---|
| `text` | Single-line string with optional regex validation |
| `textarea` | Multi-line text (justifications, descriptions) |
| `select` | Enum from a fixed list |
| `multi-select` | Multiple values from a list |
| `boolean` | Checkbox |
| `number` | Numeric with optional min/max |
| `date` | Date picker |
| `user` | People picker (resolves to email/ID) |

Fields support `visible_when` for conditional rendering and `required_when` for conditional validation.

---

## Primitive Library

Platform-maintained Temporal activities available to all workflow definitions.

| Primitive | Description |
|---|---|
| `approval_gate` | Suspends workflow, notifies approvers, resumes on approve/reject signal |
| `service_call` | Authenticated HTTP call to a registered team service |
| `create_change_request` | Creates a CHG in the ITSM system |
| `create_incident` | Creates an INC; typically used in `on_failure` |
| `send_notification` | Email, Slack, or Teams message |
| `condition` | Branches workflow based on expression over prior step output |
| `parallel` | Fan-out multiple steps; waits for all (or first) to complete |
| `wait` | Pauses for a duration or until a specified time |
| `transform` | Maps/reshapes data between steps without a service call |

---

## Service Registry

Teams register their services so they can be referenced by name in `service_call` steps. The registry stores:

- Service name (used in workflow definitions)
- Base URL (resolved to the team's platform URL namespace)
- Auth method (IAM role on orchestration ECS task, or per-service token in Secrets Manager)
- Available endpoints + expected request/response schema (optional, enables validation)

Teams can register services via the platform API or a config file in their repo.

---

## Service Catalog & Self-Service Portal

Every registered workflow definition automatically appears in the catalog.

```
Request something
├── Data Engineering
│   └── Provision Database        → [generated form] → submit → triggers workflow
├── Platform
│   └── Create ECS Service        → [generated form]
└── Security
    └── Request Elevated Access   → [generated form]
```

The form is generated from the workflow's `inputs` block. On submit, the platform validates input, starts the Temporal workflow, and returns a request ID the user can track.

Users also have an **approval inbox** — pending `approval_gate` steps appear here with context and approve/reject actions.

---

## Open Design Decisions

### 1. Dynamic vs. compiled workflows
**Dynamic**: one generic Temporal workflow class interprets the step list at runtime. Simpler to build; step definitions can change without redeploying workers.  
**Compiled**: code-generate a Temporal workflow class per definition. Better replay safety (Temporal requires deterministic history); more complex build pipeline.  
*Lean: dynamic to start, with versioning on the definition to handle replay.*

### 2. Workflow definition storage
Where do teams store and version their YAML definitions?  
- **Git-backed** (PR = deploy): best for auditability, fits GitOps culture  
- **API/UI managed**: lower friction for non-engineers  
- **Hybrid**: Git as source of truth, UI for read/trigger only

### 3. Approval UX
Where do approvers take action?  
- **Platform UI** (simplest to build)  
- **Email link** (most convenient, no login required)  
- **Slack** (highest adoption for internal tools)

### 4. Service call auth
When the orchestration service calls a team's registered endpoint:  
- **IAM role** on the orchestration ECS task (cleanest if teams are on the same AWS account/org)  
- **Per-service API tokens** stored in Secrets Manager (works cross-account)

### 5. Workflow versioning
When a team updates a workflow definition, in-flight executions should complete on the version they started with. The definition layer needs versioning independent of Temporal's internal versioning.

### 6. Team-owned workers
Teams that need internal orchestration beyond what WaaS provides can run a Temporal worker in their ECS service. They connect to the platform cluster using their injected `TEMPORAL_HOST` and `TEMPORAL_NAMESPACE`, choose a task queue name that doesn't conflict with `waas`, and own their workflow/activity code entirely. The platform provides the namespace — teams bring the code.

---

## What's Not in Scope (Yet)

- Cross-team workflow composition (one team's workflow calling another team's workflow)
- External trigger sources (webhooks, schedule-based, event bus)
- Workflow analytics / SLA tracking
- Multi-region or HA Temporal cluster
