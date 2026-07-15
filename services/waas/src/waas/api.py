"""WaaS orchestration API (DESIGN.md §11 shape).

    cd services/waas
    TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default uv run uvicorn waas.api:app --reload --port 8004

Endpoints follow the orchestration surface: requests are made against a catalog
item, edited before approval, and approved per step.
"""
import uuid
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from temporalio.client import Client
from temporalio.service import RPCError

from temporal_client import get_client

from .store import init_db, create, update_inputs, get_inputs
from .workflows import ProvisionWorkflow

TASK_QUEUE = "waas-queue"
UI_BASE = "http://localhost:8080/namespaces/default/workflows"

_client: Client | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _client
    init_db()
    _client = await get_client()
    yield


app = FastAPI(title="WaaS Orchestration Service", lifespan=lifespan)


def _workflow_id(request_id: str) -> str:
    return f"waas-{request_id}"


@app.post("/catalog/{item_id}/requests", status_code=201)
async def create_request(item_id: str, inputs: dict):
    """Submit a request against a catalog item; starts the delivery workflow."""
    request_id = str(uuid.uuid4())
    create(request_id, item_id, inputs)
    handle = await _client.start_workflow(
        ProvisionWorkflow.run,
        request_id,
        id=_workflow_id(request_id),
        task_queue=TASK_QUEUE,
    )
    return {
        "request_id": request_id,
        "catalog_item": item_id,
        "workflow_id": handle.id,
        "inputs": inputs,
        "ui": f"{UI_BASE}/{handle.id}",
    }


@app.patch("/requests/{request_id}")
async def edit_request(request_id: str, patch: dict):
    """Edit request inputs before the approval cutoff (no cutoff enforcement yet)."""
    existing = get_inputs(request_id)
    if existing is None:
        raise HTTPException(status_code=404, detail="Request not found")
    merged = {**existing, **patch}
    update_inputs(request_id, merged)
    return {"request_id": request_id, "inputs": merged}


@app.post("/requests/{request_id}/approvals/{step_id}/approve")
async def approve(request_id: str, step_id: str):
    """Approve a specific approval gate (per-step, per DESIGN.md §11)."""
    handle = _client.get_workflow_handle(_workflow_id(request_id))
    try:
        await handle.signal(ProvisionWorkflow.approve)
    except RPCError:
        raise HTTPException(status_code=404, detail="Request not found")
    return {"request_id": request_id, "step_id": step_id, "action": "approved"}


@app.get("/requests/{request_id}")
async def get_request(request_id: str):
    inputs = get_inputs(request_id)
    if inputs is None:
        raise HTTPException(status_code=404, detail="Request not found")
    state = "unknown"
    handle = _client.get_workflow_handle(_workflow_id(request_id))
    try:
        approved = await handle.query(ProvisionWorkflow.approved)
        state = "pending_approval" if not approved else "provisioning"
    except RPCError:
        pass
    return {"request_id": request_id, "inputs": inputs, "state": state}
