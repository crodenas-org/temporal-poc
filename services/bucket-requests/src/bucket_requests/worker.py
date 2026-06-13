"""
Bucket requests Temporal worker.

    cd services/bucket-requests
    TEMPORAL_HOST=localhost:7233 TEMPORAL_NAMESPACE=default uv run python -m bucket_requests.worker
"""
import asyncio
import logging
from temporal_client import get_client, build_worker
from .activities import (
    send_confirmation_email,
    create_bucket,
    apply_bucket_policy,
    send_provisioned_email,
    send_rejection_email,
)
from .workflows import BucketRequestWorkflow

TASK_QUEUE = "bucket-requests-queue"


async def main() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    client = await get_client()
    worker = build_worker(
        client,
        task_queue=TASK_QUEUE,
        workflows=[BucketRequestWorkflow],
        activities=[
            send_confirmation_email,
            create_bucket,
            apply_bucket_policy,
            send_provisioned_email,
            send_rejection_email,
        ],
    )
    print(f"Worker running — namespace: {client.namespace}  task queue: {TASK_QUEUE}")
    print("Press Ctrl+C to stop.")
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
