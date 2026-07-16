COMPOSE     = podman compose
WAAS_DIR    = ../../services/waas
DNS_DIR     = ../../services/dns-svc
COMPUTE_DIR = ../../services/compute-svc
CERTS_DIR   = $(shell pwd)/certs
T_HOST      = localhost:7233
T_NS        = default
# mTLS disabled for local dev — leave T_TLS_ENV empty so workers connect plaintext.
# Restore the TEMPORAL_TLS_* assignments below (and the block in compose.yml) to re-enable.
T_TLS_ENV   =
T_ENV       = TEMPORAL_HOST=$(T_HOST) TEMPORAL_NAMESPACE=$(T_NS) $(T_TLS_ENV)

.DEFAULT_GOAL := help

.PHONY: help up down reset logs ps ui \
        build rebuild namespace admin \
        entra-app auth-on auth-off \
        certs issue-cert \
        dns-svc compute-svc waas-worker waas-api \
        create-request approve status

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

## Custom server image
build: ## Build the custom Temporal server image (first `up` builds it automatically)
	$(COMPOSE) build temporal

rebuild: ## Rebuild the custom server image from scratch (after changing the mapper)
	$(COMPOSE) build --no-cache temporal

namespace: ## Create a namespace via the admin sidecar  (make namespace NS=svc-name)
	@test -n "$(NS)" || (echo "Usage: make namespace NS=<namespace>"; exit 1)
	$(COMPOSE) exec temporal-admin bash /scripts/create-namespace.sh $(NS)

admin: ## Open a shell in the admin-tools sidecar (temporal CLI lives here)
	$(COMPOSE) exec temporal-admin bash

## Auth (Entra) — see AUTHZ.md §6
entra-app: ## Create/refresh the shared Entra app registration (needs: az login)
	bash scripts/entra-app-setup.sh

auth-on: ## Recreate server+UI with Entra JWT enforcement ON (reads .env)
	@grep -q '^TEMPORAL_AUTH_CLIENT_ID=' .env 2>/dev/null || (echo "Run 'make entra-app' first (writes .env)"; exit 1)
	set -a; . ./.env; set +a; \
	export TEMPORAL_AUTH_ENABLED=true; \
	export TEMPORAL_AUTH_JWKS_URI="https://login.microsoftonline.com/$$TEMPORAL_AUTH_TENANT_ID/discovery/v2.0/keys"; \
	$(COMPOSE) up -d --force-recreate temporal temporal-ui
	@echo ">> Auth ON. Open http://localhost:8080 and sign in with Entra."

auth-off: ## Recreate server+UI plaintext (auth OFF)
	$(COMPOSE) up -d --force-recreate temporal temporal-ui
	@echo ">> Auth OFF (plaintext)."

## Certificates
certs: ## Generate local dev CA, server cert, and dev-client cert (run once)
	./scripts/gen-certs.sh dev-client

issue-cert: ## Issue a cert for a new service  (make issue-cert SVC=svc-name)
	@test -n "$(SVC)" || (echo "Usage: make issue-cert SVC=<service-name>"; exit 1)
	./scripts/gen-certs.sh $(SVC)

## Domain services (each owns its own logic; WaaS calls them via api_call)
dns-svc: ## Run the DNS domain service (http://localhost:8010)
	cd $(DNS_DIR) && uv run uvicorn dns_svc.api:app --reload --port 8010

compute-svc: ## Run the Compute domain service (http://localhost:8011)
	cd $(COMPUTE_DIR) && uv run uvicorn compute_svc.api:app --reload --port 8011

## WaaS orchestrator (requires: make up + dns-svc + compute-svc running)
waas-worker: ## Run the WaaS worker
	cd $(WAAS_DIR) && $(T_ENV) uv run python -m waas.worker

waas-api: ## Run the WaaS API (http://localhost:8004)
	cd $(WAAS_DIR) && $(T_ENV) uv run uvicorn waas.api:app --reload --port 8004

create-request: ## Submit a request against the 'linux-vm' catalog item
	cd $(WAAS_DIR) && $(MAKE) create-request

approve: ## Approve a request's gate  (make approve ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make approve ID=<request_id>"; exit 1)
	cd $(WAAS_DIR) && $(MAKE) approve ID=$(ID)

status: ## Get request status  (make status ID=<request_id>)
	@test -n "$(ID)" || (echo "Usage: make status ID=<request_id>"; exit 1)
	cd $(WAAS_DIR) && $(MAKE) status ID=$(ID)
