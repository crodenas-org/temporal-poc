import httpx
from temporalio import activity

from .store import get_inputs


@activity.defn
async def load_inputs(request_id: str) -> dict:
    """Read request inputs from the store (thin-payload pattern)."""
    inputs = get_inputs(request_id)
    activity.logger.info(f"[{request_id}] loaded inputs: {inputs}")
    return inputs or {}


@activity.defn
async def api_call(method: str, url: str, body: dict | None = None) -> dict:
    """Generic HTTP call to a domain-service endpoint (DESIGN.md §6 api_call).

    This is the ONLY way WaaS reaches domain behavior. There is no domain logic
    in WaaS — it lives in the service that owns `url`. In production this activity
    also attaches the orchestration app's Entra token; here it is a plain call.
    """
    async with httpx.AsyncClient(timeout=10) as client:
        resp = await client.request(method, url, json=body)
        resp.raise_for_status()
        result = resp.json() if resp.content else {}
    activity.logger.info(f"api_call {method} {url} -> {result}")
    return result


@activity.defn
async def send_notification(recipient: str, subject: str, body: str) -> dict:
    """Platform notification primitive (DESIGN.md §send_notification).

    Orchestration-owned, not a domain service — it carries no business logic, only
    delivery of template-rendered content using orchestration credentials. In
    production this sends via SES / Graph sendMail; here delivery is SIMULATED by
    logging the rendered message. Callers treat it as best-effort (non-fatal).
    """
    activity.logger.info(
        "send_notification (SIMULATED) to=%s subject=%r\n%s", recipient, subject, body
    )
    return {"delivered": True, "recipient": recipient, "subject": subject}
