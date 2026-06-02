from temporalio.client import Client
from .config import temporal_host, temporal_namespace


async def get_client(namespace: str | None = None) -> Client:
    """
    Connect to Temporal using TEMPORAL_HOST and TEMPORAL_NAMESPACE from the environment.

    Pass `namespace` explicitly to override TEMPORAL_NAMESPACE — useful when a single
    process needs clients for multiple namespaces (e.g. the platform onboarding worker).
    """
    return await Client.connect(
        temporal_host(),
        namespace=namespace or temporal_namespace(),
    )
