import asyncio
from datetime import timedelta

from temporalio import workflow
from temporalio.common import RetryPolicy

with workflow.unsafe.imports_passed_through():
    from .activities import load_inputs, api_call
    from .config import DNS_URL, COMPUTE_URL

_OPTS = dict(
    start_to_close_timeout=timedelta(seconds=30),
    retry_policy=RetryPolicy(maximum_attempts=3),
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

    @workflow.run
    async def run(self, request_id: str) -> dict:
        inputs = await workflow.execute_activity(load_inputs, request_id, **_OPTS)
        workflow.logger.info(f"[{request_id}] inputs: {inputs}")

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
