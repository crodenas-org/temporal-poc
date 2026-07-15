"""WaaS Temporal worker.

    cd services/waas
    TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default uv run python -m waas.worker
"""
import asyncio
import logging

from temporal_client import get_client, build_worker

from .activities import load_inputs, api_call
from .store import init_db
from .workflows import ProvisionWorkflow

TASK_QUEUE = "waas-queue"


async def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    init_db()
    client = await get_client()
    worker = build_worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[ProvisionWorkflow],
        activities=[load_inputs, api_call],
    )
    print(f"WaaS worker running — namespace: {client.namespace}  task queue: {TASK_QUEUE}")
    print("Press Ctrl+C to stop.")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
