from temporalio.client import Client
from temporalio.worker import Worker


def build_worker(
    client: Client,
    task_queue: str,
    workflows: list,
    activities: list,
    **kwargs,
) -> Worker:
    """
    Build a Worker bound to the client's namespace and the given task queue.

    All keyword arguments are forwarded to temporalio.worker.Worker, so
    concurrency limits, interceptors, etc. can be passed through.
    """
    return Worker(
        client,
        task_queue=task_queue,
        workflows=workflows,
        activities=activities,
        **kwargs,
    )
