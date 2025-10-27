"""Helpers for retrieving secrets through the Opperator daemon."""

from __future__ import annotations

import json
import os
import socket
import tempfile
from typing import Final, Optional

DEFAULT_SOCKET_NAME: Final[str] = "opperator.sock"
ENV_SOCKET_PATH: Final[str] = "OPPERATOR_SOCKET_PATH"
REQUEST_TYPE: Final[str] = "secret_get"


class SecretError(RuntimeError):
    """Raised when a secret cannot be retrieved from the daemon."""


def _resolve_socket_path() -> str:
    by_env = os.environ.get(ENV_SOCKET_PATH)
    if by_env:
        return by_env
    return os.path.join(tempfile.gettempdir(), DEFAULT_SOCKET_NAME)


def get_secret(name: str, *, timeout: float = 5.0) -> str:
    """Fetch a secret value from the Opperator daemon.

    Args:
        name: Identifier of the secret (case-sensitive).
        timeout: Socket timeout in seconds.

    Returns:
        The plaintext secret retrieved from the keyring.

    Raises:
        ValueError: If *name* is empty.
        SecretError: If the daemon reports an error or the request fails.
    """

    trimmed = (name or "").strip()
    if not trimmed:
        raise ValueError("secret name cannot be empty")

    payload = {
        "type": REQUEST_TYPE,
        "secret_name": trimmed,
    }

    path = _resolve_socket_path()
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
            sock.settimeout(timeout)
            sock.connect(path)
            raw = json.dumps(payload).encode("utf-8") + b"\n"
            sock.sendall(raw)

            # Read a single JSON line response
            with sock.makefile("r", encoding="utf-8") as reader:
                line: Optional[str] = reader.readline()
    except OSError as exc:
        raise SecretError(f"failed to contact daemon at {path}: {exc}") from exc

    if not line:
        raise SecretError("daemon returned no response while fetching secret")

    try:
        response = json.loads(line)
    except json.JSONDecodeError as exc:
        raise SecretError(f"invalid response from daemon: {exc}") from exc

    if not response.get("success", False):
        message = response.get("error") or "failed to retrieve secret"
        raise SecretError(message)

    if "secret" not in response:
        raise SecretError("daemon response missing secret value")

    value = response.get("secret")
    if not isinstance(value, str):
        raise SecretError("daemon returned non-string secret value")

    return value


__all__ = ["get_secret", "SecretError"]
