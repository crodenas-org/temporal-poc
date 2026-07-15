import asyncio
from datetime import timedelta

from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ActivityError

with workflow.unsafe.imports_passed_through():
    from .activities import load_inputs, api_call, send_notification
    from .config import DNS_URL, COMPUTE_URL, NOTIFY_TO, UI_BASE, API_BASE

_OPTS = dict(
    start_to_close_timeout=timedelta(seconds=30),
    retry_policy=RetryPolicy(maximum_attempts=3),
)

# Notifications are best-effort: a delivery hiccup must not fail provisioning, so
# use a shorter, non-blocking retry and swallow failures at the call site.
_NOTIFY_OPTS = dict(
    start_to_close_timeout=timedelta(seconds=15),
    retry_policy=RetryPolicy(maximum_attempts=2),
)

APPROVAL_TIMEOUT = timedelta(days=7)


@workflow.defn
class ProvisionWorkflow:
    """Sample WaaS workflow — orchestrates domain services via api_call only.

    Steps: load inputs -> approval gate -> reserve IP (dns-svc) -> provision VM
    (compute-svc). Step-1 output (ip_address) feeds step 2. No domain logic here.
    """

    def __init__(self) -> None:
        self._approved = False

    @workflow.signal
    async def approve(self) -> None:
        self._approved = True

    @workflow.query
    def approved(self) -> bool:
        return self._approved

    async def _notify_pending_approval(self, request_id: str, inputs: dict) -> None:
        """Render and send the 'awaiting approval' notice. Best-effort: never fatal.

        Template rendering is plain deterministic string building on request context
        (no I/O), so it stays in the workflow; the activity only delivers.
        """
        recipient = inputs.get("approver_email", NOTIFY_TO)
        hostname = inputs.get("hostname", "(unknown)")
        subject = f"[WaaS] Approval needed: linux-vm request {request_id}"
        body = (
            f"A linux-vm request is awaiting your approval.\n\n"
            f"  request_id: {request_id}\n"
            f"  hostname:   {hostname}\n\n"
            f"Approve:\n"
            f"  POST {API_BASE}/requests/{request_id}/approvals/approval/approve\n\n"
            f"Track in Temporal UI:\n"
            f"  {UI_BASE}/{workflow.info().workflow_id}\n"
        )
        try:
            await workflow.execute_activity(
                send_notification, args=[recipient, subject, body], **_NOTIFY_OPTS
            )
        except ActivityError:
            # Delivery failed after retries — log and proceed; provisioning must not
            # hinge on a notification.
            workflow.logger.warning(f"[{request_id}] approval notification failed; continuing")

    @workflow.run
    async def run(self, request_id: str) -> dict:
        inputs = await workflow.execute_activity(load_inputs, request_id, **_OPTS)
        workflow.logger.info(f"[{request_id}] inputs: {inputs}")

        # step 0 — notify that the request was received and an approval is pending.
        # In the mature model the approval_gate primitive owns the approver ping and
        # the recipient comes from the approval policy; here it's a standalone
        # best-effort send_notification with a hardcoded/echoed recipient (POC).
        await self._notify_pending_approval(request_id, inputs)

        # approval gate — edits to the store before this passes are picked up below
        workflow.logger.info(f"[{request_id}] waiting for approval")
        try:
            await workflow.wait_condition(lambda: self._approved, timeout=APPROVAL_TIMEOUT)
        except asyncio.TimeoutError:
            return {"request_id": request_id, "status": "expired"}

        # re-read inputs after approval (thin-payload edit-before-cutoff still applies)
        inputs = await workflow.execute_activity(load_inputs, request_id, **_OPTS)
        hostname = inputs["hostname"]

        # step 1 — reserve IP via the DNS domain service (HTTP, not python)
        dns = await workflow.execute_activity(
            api_call,
            args=["POST", f"{DNS_URL}/ip-reservations", {"hostname": hostname}],
            **_OPTS,
        )

        # step 2 — provision VM via the Compute domain service, feeding step-1 output
        vm = await workflow.execute_activity(
            api_call,
            args=["POST", f"{COMPUTE_URL}/vms", {"hostname": hostname, "ip": dns["ip_address"]}],
            **_OPTS,
        )

        return {
            "request_id": request_id,
            "status": "completed",
            "reservation_id": dns["reservation_id"],
            "ip_address": dns["ip_address"],
            "vm_id": vm["vm_id"],
        }
