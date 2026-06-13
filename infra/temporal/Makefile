COMPOSE     = podman compose
VM_DIR      = ../../services/vm-requests
BUCKET_DIR  = ../../services/bucket-requests
CERTS_DIR   = $(shell pwd)/certs
T_HOST      = localhost:7233
T_NS        = default
T_TLS_ENV   = TEMPORAL_TLS_CA=$(CERTS_DIR)/ca.crt \
              TEMPORAL_TLS_CERT=$(CERTS_DIR)/dev-client.crt \
              TEMPORAL_TLS_KEY=$(CERTS_DIR)/dev-client.key

.DEFAULT_GOAL := help

.PHONY: help up down reset logs ps ui \
        certs issue-cert \
        vm-worker vm-start vm-approve vm-reject vm-result \
        bucket-worker bucket-start bucket-approve bucket-reject bucket-status

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  %-24s %s\n", $$1, $$2}'

## Stack
up: ## Start Temporal stack (detached)
	$(COMPOSE) up -d

down: ## Stop stack, keep data
	$(COMPOSE) down

reset: ## Stop stack and wipe postgres volume
	$(COMPOSE) down -v

logs: ## Follow all container logs
	$(COMPOSE) logs -f

ps: ## Show Temporal container status
	@podman ps -a --filter name=temporal-dev --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

ui: ## Open Temporal UI in browser
	open http://localhost:8080

## Certificates
certs: ## Generate local dev CA, server cert, and dev-client cert (run once)
	./scripts/gen-certs.sh dev-client

issue-cert: ## Issue a cert for a new service  (make issue-cert SVC=svc-name)
	@test -n "$(SVC)" || (echo "Usage: make issue-cert SVC=<service-name>"; exit 1)
	./scripts/gen-certs.sh $(SVC)

## VM requests service (worker only — requires: make up && make vm-worker in a separate terminal)
vm-worker: ## Run the vm-requests worker
	cd $(VM_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run python -m vm_requests.worker

vm-start: ## Submit a new VM request
	cd $(VM_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run python -m vm_requests.starter start

vm-approve: ## Approve a VM request  (make vm-approve ID=<workflow-id>)
	@test -n "$(ID)" || (echo "Usage: make vm-approve ID=<workflow-id>"; exit 1)
	cd $(VM_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run python -m vm_requests.starter approve $(ID)

vm-reject: ## Reject a VM request   (make vm-reject ID=<workflow-id>)
	@test -n "$(ID)" || (echo "Usage: make vm-reject ID=<workflow-id>"; exit 1)
	cd $(VM_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run python -m vm_requests.starter reject $(ID)

vm-result: ## Fetch VM request result (make vm-result ID=<workflow-id>)
	@test -n "$(ID)" || (echo "Usage: make vm-result ID=<workflow-id>"; exit 1)
	cd $(VM_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run python -m vm_requests.starter result $(ID)

## Bucket requests service (API + worker — requires: make up && make bucket-worker + bucket-api)
bucket-worker: ## Run the bucket-requests worker
	cd $(BUCKET_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run python -m bucket_requests.worker

bucket-api: ## Run the bucket-requests API (http://localhost:8002)
	cd $(BUCKET_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV) \
	  uv run uvicorn bucket_requests.api:app --reload --port 8002

bucket-start: ## Submit a new bucket request (requires bucket-api running)
	cd $(BUCKET_DIR) && $(MAKE) create-request

bucket-approve: ## Approve a bucket request  (make bucket-approve ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make bucket-approve ID=<request_id>"; exit 1)
	cd $(BUCKET_DIR) && $(MAKE) approve ID=$(ID)

bucket-reject: ## Reject a bucket request   (make bucket-reject ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make bucket-reject ID=<request_id>"; exit 1)
	cd $(BUCKET_DIR) && $(MAKE) reject ID=$(ID)

bucket-status: ## Get bucket request status  (make bucket-status ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make bucket-status ID=<request_id>"; exit 1)
	cd $(BUCKET_DIR) && $(MAKE) status ID=$(ID)
