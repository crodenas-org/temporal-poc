from dataclasses import dataclass


@dataclass
class VMRequest:
    request_id: str
    requester_email: str
    instance_type: str   # e.g. "t3.medium"
    os: str              # e.g. "ubuntu-22.04"
    purpose: str
    team: str
