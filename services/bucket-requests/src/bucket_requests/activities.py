from temporalio import activity
from .models import BucketRequest


@activity.defn
async def send_confirmation_email(req: BucketRequest) -> None:
    activity.logger.info(
        f"[{req.request_id}] Confirmation → {req.requester_email}: "
        f"bucket '{req.bucket_name}' ({req.classification}) submitted, pending approval"
    )


@activity.defn
async def create_bucket(req: BucketRequest) -> str:
    # stub: real impl would call boto3 s3.create_bucket
    arn = f"arn:aws:s3:::{req.bucket_name}"
    activity.logger.info(f"[{req.request_id}] Created bucket {req.bucket_name} in {req.region} → {arn}")
    return arn


@activity.defn
async def apply_bucket_policy(req: BucketRequest, bucket_arn: str) -> None:
    # stub: real impl would apply encryption, versioning, block-public-access, lifecycle rules
    # based on req.classification
    activity.logger.info(
        f"[{req.request_id}] Applied {req.classification} policy to {bucket_arn}"
    )


@activity.defn
async def send_provisioned_email(req: BucketRequest, bucket_arn: str) -> None:
    activity.logger.info(
        f"[{req.request_id}] Ready → {req.requester_email}: "
        f"bucket {req.bucket_name} is available ({bucket_arn})"
    )


@activity.defn
async def send_rejection_email(req: BucketRequest) -> None:
    activity.logger.info(
        f"[{req.request_id}] Rejected → {req.requester_email}: "
        f"request for bucket '{req.bucket_name}' was not approved"
    )
