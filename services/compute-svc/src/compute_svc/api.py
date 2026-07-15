"""Compute domain service API.

    cd services/compute-svc
    uv run uvicorn compute_svc.api:app --reload --port 8011
"""
import uuid

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="Compute Service")

_vms: dict[str, dict] = {}


class ProvisionBody(BaseModel):
    hostname: str
    ip: str


@app.post("/vms", status_code=201)
async def provision_vm(body: ProvisionBody):
    """Provision a VM at a reserved IP. Logic lives here, not in WaaS."""
    vm_id = f"vm-{uuid.uuid4().hex[:8]}"
    record = {"vm_id": vm_id, "hostname": body.hostname, "ip": body.ip, "status": "running"}
    _vms[vm_id] = record
    return record
