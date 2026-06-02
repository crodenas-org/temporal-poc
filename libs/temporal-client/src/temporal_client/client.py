from pathlib import Path
from temporalio.client import Client, TLSConfig
from .config import temporal_host, temporal_namespace, tls_cert_path, tls_key_path, tls_ca_path


def _build_tls() -> TLSConfig | None:
    cert_path = tls_cert_path()
    key_path = tls_key_path()
    ca_path = tls_ca_path()

    if not any([cert_path, key_path, ca_path]):
        return None

    if bool(cert_path) != bool(key_path):
        raise RuntimeError("TEMPORAL_TLS_CERT and TEMPORAL_TLS_KEY must both be set or both be unset.")

    return TLSConfig(
        client_cert=Path(cert_path).read_bytes() if cert_path else None,
        client_private_key=Path(key_path).read_bytes() if key_path else None,
        server_root_ca_cert=Path(ca_path).read_bytes() if ca_path else None,
    )


async def get_client(namespace: str | None = None) -> Client:
    """
    Connect to Temporal using TEMPORAL_HOST and TEMPORAL_NAMESPACE from the environment.

    mTLS is enabled automatically when TEMPORAL_TLS_CERT, TEMPORAL_TLS_KEY, and
    TEMPORAL_TLS_CA are set. Omitting all three falls back to plaintext (local dev
    without certs).

    Pass `namespace` explicitly to override TEMPORAL_NAMESPACE — useful when a single
    process needs clients for multiple namespaces.
    """
    return await Client.connect(
        temporal_host(),
        namespace=namespace or temporal_namespace(),
        tls=_build_tls(),
    )
