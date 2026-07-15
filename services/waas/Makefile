T_HOST  = localhost:7233
T_NS    = default
T_ENV   = TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS)

.DEFAULT_GOAL := help
.PHONY: help install worker api create-request edit approve status

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

install: ## Install dependencies
	uv sync

worker: ## Run the WaaS worker (needs dns-svc + compute-svc running)
	$(T_ENV) uv run python -m waas.worker

api: ## Run the WaaS API (http://localhost:8004)
	$(T_ENV) uv run uvicorn waas.api:app --reload --port 8004

create-request: ## Submit a request against the 'linux-vm' catalog item
	curl -s -X POST http://localhost:8004/catalog/linux-vm/requests \
	  -H "Content-Type: application/json" \
	  -d '{"hostname":"web01"}' \
	  | python3 -m json.tool

edit: ## Edit inputs before approval  (make edit ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make edit ID=<request_id>"; exit 1)
	curl -s -X PATCH http://localhost:8004/requests/$(ID) \
	  -H "Content-Type: application/json" \
	  -d '{"hostname":"web02"}' \
	  | python3 -m json.tool

approve: ## Approve the request's gate  (make approve ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make approve ID=<request_id>"; exit 1)
	curl -s -X POST http://localhost:8004/requests/$(ID)/approvals/approval/approve | python3 -m json.tool

status: ## Get request status  (make status ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make status ID=<request_id>"; exit 1)
	curl -s http://localhost:8004/requests/$(ID) | python3 -m json.tool
