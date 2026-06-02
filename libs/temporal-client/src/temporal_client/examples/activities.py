import asyncio
from temporalio import activity
from .models import Order


@activity.defn
async def reserve_inventory(order: Order) -> str:
    activity.logger.info(f"[{order.order_id}] Reserving inventory for items: {order.items}")
    await asyncio.sleep(0.5)
    return f"RSV-{order.order_id}"


@activity.defn
async def release_inventory(order: Order) -> None:
    activity.logger.info(f"[{order.order_id}] Releasing inventory (order rejected)")
    await asyncio.sleep(0.2)


@activity.defn
async def charge_payment(order: Order) -> str:
    activity.logger.info(f"[{order.order_id}] Charging ${order.total_amount:.2f}")
    await asyncio.sleep(0.5)
    return f"PAY-{order.order_id}"


@activity.defn
async def fulfill_order(order: Order) -> str:
    activity.logger.info(f"[{order.order_id}] Fulfilling order")
    await asyncio.sleep(0.5)
    return f"SHIP-{order.order_id}"
