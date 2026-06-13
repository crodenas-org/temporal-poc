"""
Usage:
  uv run python -m vm_requests.starter start
  uv run python -m vm_requests.starter approve WORKFLOW_ID
  uv run python -m vm_requests.starter reject  WORKFLOW_ID
  uv run python -m vm_requests.starter result  WORKFLOW_ID
"""
import asyncio
import logging
import sys
import uuid
from temporal_client import get_client
from .models import VMRequest
from .workflows import VMRequestWorkflow

TASK_QUEUE = "vm-requests-queue"
UI_BASE = "http://localhost:8080/namespaces/default/workflows"


async def cmd_start() -> None:
    client = await get_client()
    request_id = uuid.uuid4().hex[:8].upper()
    req = VMRequest(
        request_id=request_id,
        requester_email="alice@example.com",
        instance_type="t3.medium",
        os="ubuntu-22.04",
        purpose="dev environment for project X",
        team="platform",
    )
    workflow_id = f"vm-request-{request_id}"
    handle = await client.start_workflow(
        VMRequestWorkflow.run, req, id=workflow_id, task_queue=TASK_QUEUE
    )
    print(f"VM request submitted")
    print(f"  Request ID  : {req.request_id}")
    print(f"  Workflow ID : {handle.id}")
    print(f"  UI          : {UI_BASE}/{handle.id}")
    print()
    print(f"Approve : uv run python -m vm_requests.starter approve {handle.id}")
    print(f"Reject  : uv run python -m vm_requests.starter reject  {handle.id}")


async def cmd_signal(workflow_id: str, signal: str) -> None:
    client = await get_client()
    handle = client.get_workflow_handle(workflow_id)
    if signal == "approve":
        await handle.signal(VMRequestWorkflow.approve)
        print(f"APPROVE signal sent to {workflow_id}")
    else:
        await handle.signal(VMRequestWorkflow.reject)
        print(f"REJECT signal sent to {workflow_id}")
    print(f"  Result : uv run python -m vm_requests.starter result {workflow_id}")


async def cmd_result(workflow_id: str) -> None:
    client = await get_client()
    handle = client.get_workflow_handle(workflow_id)
    result = await handle.result()
    print(f"Result: {result}")


async def main() -> None:
    logging.basicConfig(level=logging.WARNING)
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        sys.exit(1)
    cmd = args[0]
    if cmd == "start":
        await cmd_start()
    elif cmd in ("approve", "reject"):
        if len(args) < 2:
            print(f"Usage: starter.py {cmd} WORKFLOW_ID")
            sys.exit(1)
        await cmd_signal(args[1], cmd)
    elif cmd == "result":
        if len(args) < 2:
            print("Usage: starter.py result WORKFLOW_ID")
            sys.exit(1)
        await cmd_result(args[1])
    else:
        print(__doc__)
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
