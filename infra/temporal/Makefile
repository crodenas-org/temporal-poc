COMPOSE  = podman compose
LIB_DIR  = ../../libs/temporal-client
T_HOST   = localhost:7233
T_NS     = default

.DEFAULT_GOAL := help

.PHONY: help up down reset logs ps ui \
        example-worker example-start example-approve example-reject example-result

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

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

## Example workflow (requires: make up && make example-worker in a separate terminal)
example-worker: ## Run the example order workflow worker
	cd $(LIB_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) \
	  uv run python -m temporal_client.examples.worker

example-start: ## Start a new order workflow
	cd $(LIB_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) \
	  uv run python -m temporal_client.examples.starter start

example-approve: ## Approve a workflow  (make example-approve ID=order-ORD-XXXXXXXX)
	@test -n "$(ID)" || (echo "Usage: make example-approve ID=<workflow-id>"; exit 1)
	cd $(LIB_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) \
	  uv run python -m temporal_client.examples.starter approve $(ID)

example-reject: ## Reject a workflow   (make example-reject ID=order-ORD-XXXXXXXX)
	@test -n "$(ID)" || (echo "Usage: make example-reject ID=<workflow-id>"; exit 1)
	cd $(LIB_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) \
	  uv run python -m temporal_client.examples.starter reject $(ID)

example-result: ## Fetch workflow result (make example-result ID=order-ORD-XXXXXXXX)
	@test -n "$(ID)" || (echo "Usage: make example-result ID=<workflow-id>"; exit 1)
	cd $(LIB_DIR) && TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) \
	  uv run python -m temporal_client.examples.starter result $(ID)
