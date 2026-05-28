from datetime import timedelta
from typing import Optional
from temporalio import workflow
from temporalio.common import RetryPolicy

with workflow.unsafe.imports_passed_through():
    from models import Order
    from activities import (
        reserve_inventory,
        release_inventory,
        charge_payment,
        fulfill_order,
    )

_ACTIVITY_OPTIONS = dict(
    start_to_close_timeout=timedelta(seconds=30),
    retry_policy=RetryPolicy(maximum_attempts=3),
)


@workflow.defn
class OrderWorkflow:
    def __init__(self) -> None:
        self._approval: Optional[bool] = None

    @workflow.signal
    async def approve(self) -> None:
        workflow.logger.info("Received APPROVE signal")
        self._approval = True

    @workflow.signal
    async def reject(self) -> None:
        workflow.logger.info("Received REJECT signal")
        self._approval = False

    @workflow.run
    async def run(self, order: Order) -> dict:
        workflow.logger.info(f"Order workflow started for {order.order_id}")

        # Step 1 — reserve inventory
        reservation_id = await workflow.execute_activity(
            reserve_inventory, order, **_ACTIVITY_OPTIONS
        )

        # Step 2 — wait for a human to approve or reject
        workflow.logger.info(f"[{order.order_id}] Waiting for manager approval signal...")
        await workflow.wait_condition(lambda: self._approval is not None)

        # Step 3 — saga: compensate on rejection, proceed on approval
        if not self._approval:
            await workflow.execute_activity(
                release_inventory, order, **_ACTIVITY_OPTIONS
            )
            return {"status": "rejected", "order_id": order.order_id}

        payment_id = await workflow.execute_activity(
            charge_payment, order, **_ACTIVITY_OPTIONS
        )
        shipment_id = await workflow.execute_activity(
            fulfill_order, order, **_ACTIVITY_OPTIONS
        )

        return {
            "status": "fulfilled",
            "order_id": order.order_id,
            "reservation_id": reservation_id,
            "payment_id": payment_id,
            "shipment_id": shipment_id,
        }
