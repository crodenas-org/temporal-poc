"""DNS domain service API.

    cd services/dns-svc
    uv run uvicorn dns_svc.api:app --reload --port 8010
"""
import uuid

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

app = FastAPI(title="DNS Service")

# trivial in-memory store; a real service owns a real backend
_reservations: dict[str, dict] = {}


class ReserveBody(BaseModel):
    hostname: str


@app.post("/ip-reservations", status_code=201)
async def reserve_ip(body: ReserveBody):
    """Reserve an IP for a hostname. This 'logic' lives here, not in WaaS."""
    reservation_id = f"res-{uuid.uuid4().hex[:8]}"
    ip_address = f"10.0.0.{len(_reservations) + 10}"
    record = {"reservation_id": reservation_id, "ip_address": ip_address, "hostname": body.hostname}
    _reservations[reservation_id] = record
    return record


@app.delete("/ip-reservations/{reservation_id}", status_code=204)
async def release_ip(reservation_id: str):
    """Compensation hook — release a reservation."""
    _reservations.pop(reservation_id, None)
