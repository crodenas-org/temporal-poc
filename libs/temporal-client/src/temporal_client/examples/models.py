from dataclasses import dataclass


@dataclass
class Order:
    order_id: str
    customer_id: str
    items: list[str]
    total_amount: float
