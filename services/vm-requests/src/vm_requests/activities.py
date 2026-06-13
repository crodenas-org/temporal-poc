from temporalio import activity
from .models import VMRequest


@activity.defn
async def send_confirmation_email(req: VMRequest) -> None:
    activity.logger.info(
        f"[{req.request_id}] Confirmation → {req.requester_email}: "
        f"{req.instance_type} ({req.os}) for '{req.purpose}' submitted, pending approval"
    )


@activity.defn
async def provision_vm(req: VMRequest) -> str:
    # stub: real impl would call EC2 / Terraform / Ansible
    instance_id = f"i-{req.request_id[:8]}"
    activity.logger.info(
        f"[{req.request_id}] Provisioning {req.instance_type} ({req.os}) → {instance_id}"
    )
    return instance_id


@activity.defn
async def send_provisioned_email(req: VMRequest, instance_id: str) -> None:
    activity.logger.info(
        f"[{req.request_id}] Ready → {req.requester_email}: "
        f"VM {instance_id} is available"
    )


@activity.defn
async def send_rejection_email(req: VMRequest) -> None:
    activity.logger.info(
        f"[{req.request_id}] Rejected → {req.requester_email}: "
        f"request for {req.instance_type} was not approved"
    )
