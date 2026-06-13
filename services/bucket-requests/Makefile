T_HOST  = localhost:7233
T_NS    = default
T_ENV   = TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS)

.DEFAULT_GOAL := help

.PHONY: help install worker api create-request approve reject status

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

install: ## Install dependencies
	uv sync

worker: ## Run the Temporal worker
	$(T_ENV) uv run python -m bucket_requests.worker

api: ## Run the FastAPI app (http://localhost:8002)
	$(T_ENV) uv run uvicorn bucket_requests.api:app --reload --port 8002

create-request: ## Submit a new bucket request
	curl -s -X POST http://localhost:8002/requests \
	  -H "Content-Type: application/json" \
	  -d '{"requester_email":"alice@example.com","bucket_name":"my-data-bucket","region":"us-east-1","purpose":"data lake landing zone","classification":"internal"}' \
	  | python3 -m json.tool

approve: ## Approve a request  (make approve ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make approve ID=<request_id>"; exit 1)
	curl -s -X POST http://localhost:8002/requests/$(ID)/approve | python3 -m json.tool

reject: ## Reject a request   (make reject ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make reject ID=<request_id>"; exit 1)
	curl -s -X POST http://localhost:8002/requests/$(ID)/reject | python3 -m json.tool

status: ## Get request status  (make status ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make status ID=<request_id>"; exit 1)
	curl -s http://localhost:8002/requests/$(ID) | python3 -m json.tool
