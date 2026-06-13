from dataclasses import dataclass
from enum import Enum


class DataClassification(str, Enum):
    PUBLIC = "public"
    INTERNAL = "internal"
    CONFIDENTIAL = "confidential"


@dataclass
class BucketRequest:
    request_id: str
    requester_email: str
    bucket_name: str
    region: str
    purpose: str
    classification: DataClassification
