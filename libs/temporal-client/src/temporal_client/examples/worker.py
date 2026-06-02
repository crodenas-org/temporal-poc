"""
Run the example order workflow worker.

    TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default uv run python -m temporal_client.examples.worker
"""
import asyncio
import logging
from temporal_client import get_client, build_worker
from .activities import charge_payment, fulfill_order, release_inventory, reserve_inventory
from .workflows import OrderWorkflow

TASK_QUEUE = "order-queue"


async def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    client = await get_client()
    worker = build_worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[OrderWorkflow],
        activities=[reserve_inventory, release_inventory, charge_payment, fulfill_order],
    )
    print(f"Worker running — namespace: {client.namespace}  task queue: {TASK_QUEUE}")
    print("Press Ctrl+C to stop.")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
