"""
Bucket requests FastAPI app.

    cd services/bucket-requests
    TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default uv run uvicorn bucket_requests.api:app --reload --port 8002
"""
import uuid
from contextlib import asynccontextmanager
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from temporalio.client import Client
from temporalio.service import RPCError
from temporal_client import get_client
from .models import BucketRequest, DataClassification
from .workflows import BucketRequestWorkflow

TASK_QUEUE = "bucket-requests-queue"
UI_BASE = "http://localhost:8080/namespaces/default/workflows"

_client: Client | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _client
    _client = await get_client()
    yield


app = FastAPI(title="Bucket Requests Service", lifespan=lifespan)


def _workflow_id(request_id: str) -> str:
    return f"bucket-request-{request_id}"


class CreateRequestBody(BaseModel):
    requester_email: str
    bucket_name: str
    region: str
    purpose: str
    classification: DataClassification


@app.post("/requests", status_code=201)
async def create_request(body: CreateRequestBody):
    request_id = str(uuid.uuid4())
    req = BucketRequest(
        request_id=request_id,
        requester_email=body.requester_email,
        bucket_name=body.bucket_name,
        region=body.region,
        purpose=body.purpose,
        classification=body.classification,
    )
    handle = await _client.start_workflow(
        BucketRequestWorkflow.run,
        req,
        id=_workflow_id(request_id),
        task_queue=TASK_QUEUE,
    )
    return {
        "request_id": request_id,
        "workflow_id": handle.id,
        "ui": f"{UI_BASE}/{handle.id}",
    }


@app.get("/requests/{request_id}")
async def get_request_status(request_id: str):
    handle = _client.get_workflow_handle(_workflow_id(request_id))
    try:
        status = await handle.query(BucketRequestWorkflow.status)
    except RPCError:
        raise HTTPException(status_code=404, detail="Request not found")
    return {"request_id": request_id, "status": status}


@app.post("/requests/{request_id}/approve")
async def approve_request(request_id: str):
    handle = _client.get_workflow_handle(_workflow_id(request_id))
    try:
        await handle.signal(BucketRequestWorkflow.approve)
    except RPCError:
        raise HTTPException(status_code=404, detail="Request not found")
    return {"request_id": request_id, "action": "approved"}


@app.post("/requests/{request_id}/reject")
async def reject_request(request_id: str):
    handle = _client.get_workflow_handle(_workflow_id(request_id))
    try:
        await handle.signal(BucketRequestWorkflow.reject)
    except RPCError:
        raise HTTPException(status_code=404, detail="Request not found")
    return {"request_id": request_id, "action": "rejected"}
