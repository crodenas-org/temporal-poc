.DEFAULT_GOAL := help
.PHONY: help install api

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

install: ## Install dependencies
	uv sync

api: ## Run the DNS service (http://localhost:8010)
	uv run uvicorn dns_svc.api:app --reload --port 8010
