import os


def _require(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise RuntimeError(
            f"Required environment variable {name!r} is not set. "
            "Set it to the Temporal server address (e.g. TEMPORAL_HOST=localhost:7233) "
            "or the target namespace (e.g. TEMPORAL_NAMESPACE=svc-my-service)."
        )
    return value


def temporal_host() -> str:
    return _require("TEMPORAL_HOST")


def temporal_namespace() -> str:
    return _require("TEMPORAL_NAMESPACE")


def tls_cert_path() -> str | None:
    return os.environ.get("TEMPORAL_TLS_CERT")


def tls_key_path() -> str | None:
    return os.environ.get("TEMPORAL_TLS_KEY")


def tls_ca_path() -> str | None:
    return os.environ.get("TEMPORAL_TLS_CA")
