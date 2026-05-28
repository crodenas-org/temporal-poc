"""
Usage:
  uv run python starter.py start [ORDER_ID]     — start a new order workflow
  uv run python starter.py approve WORKFLOW_ID  — send approve signal
  uv run python starter.py reject  WORKFLOW_ID  — send reject signal
  uv run python starter.py result  WORKFLOW_ID  — fetch completed result
"""

import asyncio
import logging
import sys
import uuid
from temporalio.client import Client
from models import Order
from workflows import OrderWorkflow

TASK_QUEUE = "order-queue"
UI_BASE = "http://localhost:8080/namespaces/default/workflows"


async def cmd_start(client: Client, order_id: str | None) -> None:
    order_id = order_id or f"ORD-{uuid.uuid4().hex[:8].upper()}"
    order = Order(
        order_id=order_id,
        customer_id="CUST-001",
        items=["Widget A", "Gadget B"],
        total_amount=99.99,
    )
    workflow_id = f"order-{order_id}"
    handle = await client.start_workflow(
        OrderWorkflow.run,
        order,
        id=workflow_id,
        task_queue=TASK_QUEUE,
    )
    print(f"Workflow started")
    print(f"  Workflow ID : {handle.id}")
    print(f"  Order ID    : {order_id}")
    print(f"  UI          : {UI_BASE}/{handle.id}")
    print()
    print(f"Approve : uv run python starter.py approve {handle.id}")
    print(f"Reject  : uv run python starter.py reject  {handle.id}")


async def cmd_signal(client: Client, workflow_id: str, signal: str) -> None:
    handle = client.get_workflow_handle(workflow_id)
    if signal == "approve":
        await handle.signal(OrderWorkflow.approve)
        print(f"APPROVE signal sent to {workflow_id}")
    else:
        await handle.signal(OrderWorkflow.reject)
        print(f"REJECT signal sent to {workflow_id}")
    print(f"  Result  : uv run python starter.py result {workflow_id}")


async def cmd_result(client: Client, workflow_id: str) -> None:
    handle = client.get_workflow_handle(workflow_id)
    result = await handle.result()
    print(f"Result: {result}")


async def main() -> None:
    logging.basicConfig(level=logging.WARNING)
    args = sys.argv[1:]

    if not args:
        print(__doc__)
        sys.exit(1)

    client = await Client.connect("localhost:7233")
    cmd = args[0]

    if cmd == "start":
        await cmd_start(client, args[1] if len(args) > 1 else None)
    elif cmd in ("approve", "reject"):
        if len(args) < 2:
            print(f"Usage: starter.py {cmd} WORKFLOW_ID")
            sys.exit(1)
        await cmd_signal(client, args[1], cmd)
    elif cmd == "result":
        if len(args) < 2:
            print("Usage: starter.py result WORKFLOW_ID")
            sys.exit(1)
        await cmd_result(client, args[1])
    else:
        print(__doc__)
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
