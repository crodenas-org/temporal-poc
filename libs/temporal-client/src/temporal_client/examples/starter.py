"""
Usage:
  uv run python -m temporal_client.examples.starter start [ORDER_ID]
  uv run python -m temporal_client.examples.starter approve WORKFLOW_ID
  uv run python -m temporal_client.examples.starter reject  WORKFLOW_ID
  uv run python -m temporal_client.examples.starter result  WORKFLOW_ID
"""
import asyncio
import logging
import sys
import uuid
from temporal_client import get_client
from .models import Order
from .workflows import OrderWorkflow

TASK_QUEUE = "order-queue"
UI_BASE = "http://localhost:8080/namespaces/default/workflows"


async def cmd_start(order_id: str | None) -> None:
    client = await get_client()
    order_id = order_id or f"ORD-{uuid.uuid4().hex[:8].upper()}"
    order = Order(order_id=order_id, customer_id="CUST-001", items=["Widget A", "Gadget B"], total_amount=99.99)
    workflow_id = f"order-{order_id}"
    handle = await client.start_workflow(OrderWorkflow.run, order, id=workflow_id, task_queue=TASK_QUEUE)
    print(f"Workflow started")
    print(f"  Workflow ID : {handle.id}")
    print(f"  Namespace   : {client.namespace}")
    print(f"  UI          : {UI_BASE}/{handle.id}")
    print()
    print(f"Approve : uv run python -m temporal_client.examples.starter approve {handle.id}")
    print(f"Reject  : uv run python -m temporal_client.examples.starter reject  {handle.id}")


async def cmd_signal(workflow_id: str, signal: str) -> None:
    client = await get_client()
    handle = client.get_workflow_handle(workflow_id)
    if signal == "approve":
        await handle.signal(OrderWorkflow.approve)
        print(f"APPROVE signal sent to {workflow_id}")
    else:
        await handle.signal(OrderWorkflow.reject)
        print(f"REJECT signal sent to {workflow_id}")
    print(f"  Result : uv run python -m temporal_client.examples.starter result {workflow_id}")


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
        await cmd_start(args[1] if len(args) > 1 else None)
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
