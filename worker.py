import asyncio
import logging
from temporalio.client import Client
from temporalio.worker import Worker
from activities import charge_payment, fulfill_order, release_inventory, reserve_inventory
from workflows import OrderWorkflow

TASK_QUEUE = "order-queue"


async def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    client = await Client.connect("localhost:7233")
    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[OrderWorkflow],
        activities=[reserve_inventory, release_inventory, charge_payment, fulfill_order],
    )
    print(f"Worker running on task queue: {TASK_QUEUE}")
    print("Press Ctrl+C to stop.")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
