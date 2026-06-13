import asyncio
from datetime import timedelta
from typing import Optional
from temporalio import workflow
from temporalio.common import RetryPolicy

with workflow.unsafe.imports_passed_through():
    from .models import VMRequest
    from .activities import (
        send_confirmation_email,
        provision_vm,
        send_provisioned_email,
        send_rejection_email,
    )

_ACTIVITY_OPTIONS = dict(
    start_to_close_timeout=timedelta(seconds=60),
    retry_policy=RetryPolicy(maximum_attempts=3),
)

APPROVAL_TIMEOUT = timedelta(days=7)


@workflow.defn
class VMRequestWorkflow:
    def __init__(self) -> None:
        self._decision: Optional[bool] = None

    @workflow.signal
    async def approve(self) -> None:
        self._decision = True

    @workflow.signal
    async def reject(self) -> None:
        self._decision = False

    @workflow.query
    def status(self) -> str:
        if self._decision is None:
            return "pending"
        return "approved" if self._decision else "rejected"

    @workflow.run
    async def run(self, req: VMRequest) -> dict:
        workflow.logger.info(f"VM request started: {req.request_id} ({req.instance_type})")

        await workflow.execute_activity(send_confirmation_email, req, **_ACTIVITY_OPTIONS)

        try:
            await workflow.wait_condition(
                lambda: self._decision is not None,
                timeout=APPROVAL_TIMEOUT,
            )
        except asyncio.TimeoutError:
            return {"status": "expired", "request_id": req.request_id}

        if not self._decision:
            await workflow.execute_activity(send_rejection_email, req, **_ACTIVITY_OPTIONS)
            return {"status": "rejected", "request_id": req.request_id}

        instance_id = await workflow.execute_activity(provision_vm, req, **_ACTIVITY_OPTIONS)
        await workflow.execute_activity(send_provisioned_email, req, instance_id, **_ACTIVITY_OPTIONS)

        return {
            "status": "provisioned",
            "request_id": req.request_id,
            "instance_id": instance_id,
        }
