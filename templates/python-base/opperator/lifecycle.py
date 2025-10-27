"""
Lifecycle management for Opperator processes.
"""

import signal
import threading
from typing import Callable, Iterable

from .protocol import LogLevel, Protocol


class LifecycleManager:
    """Manages process lifecycle and signal handling"""

    def __init__(self):
        self._shutdown_handlers = []
        self._reload_handlers = []
        self._status_handlers = []
        self._shutdown_event = threading.Event()
        self._setup_complete = False

    def setup_signal_handlers(self):
        """Setup signal handlers for graceful shutdown and reload"""
        if self._setup_complete:
            return

        # SIGTERM - Graceful shutdown
        signal.signal(signal.SIGTERM, self._handle_sigterm)

        # SIGHUP - Reload configuration
        signal.signal(signal.SIGHUP, self._handle_sighup)

        # SIGUSR1 - Report status
        if hasattr(signal, "SIGUSR1"):
            signal.signal(signal.SIGUSR1, self._handle_sigusr1)

        # SIGINT - Handle Ctrl+C
        signal.signal(signal.SIGINT, self._handle_sigint)

        self._setup_complete = True
        Protocol.send_log(LogLevel.DEBUG, "Signal handlers registered")

    def _handle_sigterm(self, signum, frame):
        """Handle SIGTERM for graceful shutdown"""
        Protocol.send_log(
            LogLevel.INFO, "Received SIGTERM, initiating graceful shutdown"
        )
        self._invoke_handlers(self._shutdown_handlers, "Shutdown")
        self._shutdown_event.set()

    def _handle_sighup(self, signum, frame):
        """Handle SIGHUP for configuration reload"""
        Protocol.send_log(LogLevel.INFO, "Received SIGHUP, reloading configuration")
        self._invoke_handlers(self._reload_handlers, "Reload")

    def _handle_sigusr1(self, signum, frame):
        """Handle SIGUSR1 for status reporting"""
        Protocol.send_log(LogLevel.DEBUG, "Received SIGUSR1, reporting status")
        self._invoke_handlers(self._status_handlers, "Status")

    def _handle_sigint(self, signum, frame):
        """Handle SIGINT (Ctrl+C) for immediate shutdown"""
        Protocol.send_log(LogLevel.INFO, "Received SIGINT, shutting down")
        self._handle_sigterm(signum, frame)

    def on_shutdown(self, handler: Callable[[], None]):
        """Register a shutdown handler"""
        self._shutdown_handlers.append(handler)

    def on_reload(self, handler: Callable[[], None]):
        """Register a reload handler"""
        self._reload_handlers.append(handler)

    def on_status(self, handler: Callable[[], None]):
        """Register a status handler"""
        self._status_handlers.append(handler)

    def wait_for_shutdown(self):
        """Block until shutdown signal is received"""
        self._shutdown_event.wait()

    def initiate_shutdown(self):
        """Programmatically initiate shutdown"""
        self._handle_sigterm(signal.SIGTERM, None)

    def _invoke_handlers(
        self, handlers: Iterable[Callable[[], None]], category: str
    ) -> None:
        for handler in handlers:
            try:
                handler()
            except Exception as exc:
                Protocol.send_error(f"{category} handler failed: {exc}")
