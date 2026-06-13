"""
VM requests Temporal worker.

    cd services/vm-requests
    TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default uv run python -m vm_requests.worker
"""
import asyncio
import logging
from temporal_client import get_client, build_worker
from .activities import send_confirmation_email, provision_vm, send_provisioned_email, send_rejection_email
from .workflows import VMRequestWorkflow

TASK_QUEUE = "vm-requests-queue"


async def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    client = await get_client()
    worker = build_worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[VMRequestWorkflow],
        activities=[send_confirmation_email, provision_vm, send_provisioned_email, send_rejection_email],
    )
    print(f"Worker running — namespace: {client.namespace}  task queue: {TASK_QUEUE}")
    print("Press Ctrl+C to stop.")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
