import asyncio
from temporalio import activity
from models import Order


@activity.defn
async def reserve_inventory(order: Order) -> str:
    activity.logger.info(f"[{order.order_id}] Reserving inventory for items: {order.items}")
    await asyncio.sleep(0.5)
    reservation_id = f"RSV-{order.order_id}"
    activity.logger.info(f"[{order.order_id}] Inventory reserved: {reservation_id}")
    return reservation_id


@activity.defn
async def release_inventory(order: Order) -> None:
    activity.logger.info(f"[{order.order_id}] Releasing inventory (order rejected)")
    await asyncio.sleep(0.2)
    activity.logger.info(f"[{order.order_id}] Inventory released")


@activity.defn
async def charge_payment(order: Order) -> str:
    activity.logger.info(f"[{order.order_id}] Charging ${order.total_amount:.2f} to customer {order.customer_id}")
    await asyncio.sleep(0.5)
    payment_id = f"PAY-{order.order_id}"
    activity.logger.info(f"[{order.order_id}] Payment captured: {payment_id}")
    return payment_id


@activity.defn
async def fulfill_order(order: Order) -> str:
    activity.logger.info(f"[{order.order_id}] Fulfilling order, preparing shipment")
    await asyncio.sleep(0.5)
    shipment_id = f"SHIP-{order.order_id}"
    activity.logger.info(f"[{order.order_id}] Shipment created: {shipment_id}")
    return shipment_id
