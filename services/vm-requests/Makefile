T_HOST  = localhost:7233
T_NS    = default
T_ENV   = TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS)

.DEFAULT_GOAL := help

.PHONY: help install worker start approve reject result

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

install: ## Install dependencies
	uv sync

worker: ## Run the Temporal worker
	$(T_ENV) uv run python -m vm_requests.worker

start: ## Submit a new VM request
	$(T_ENV) uv run python -m vm_requests.starter start

approve: ## Approve a request  (make approve ID=<workflow-id>)
	@test -n "$(ID)" || (echo "Usage: make approve ID=<workflow-id>"; exit 1)
	$(T_ENV) uv run python -m vm_requests.starter approve $(ID)

reject: ## Reject a request   (make reject ID=<workflow-id>)
	@test -n "$(ID)" || (echo "Usage: make reject ID=<workflow-id>"; exit 1)
	$(T_ENV) uv run python -m vm_requests.starter reject $(ID)

result: ## Fetch result        (make result ID=<workflow-id>)
	@test -n "$(ID)" || (echo "Usage: make result ID=<workflow-id>"; exit 1)
	$(T_ENV) uv run python -m vm_requests.starter result $(ID)
