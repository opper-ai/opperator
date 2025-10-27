#!/usr/bin/env python3
"""
COMPREHENSIVE OPPERATOR AGENT TEMPLATE

A complete reference template showcasing all Opperator Python SDK features.

HOW TO USE:
1. Copy this file and rename it
2. Uncomment the features you need
3. Delete or keep commented features for reference
4. Customize initialize() and start()
5. Add your business logic

INCLUDED FEATURES:
- ✅ Core lifecycle (initialize, start) - IMPLEMENTED
- 📝 Optional lifecycle hooks - COMMENTED
- 📝 Lifecycle event handlers - COMMENTED
- 📝 Command registration - COMMENTED
- 📝 Sidebar management - COMMENTED
- 📝 Secrets, logging, threading - COMMENTED
"""

# =============================================================================
# IMPORTS
# =============================================================================

from opperator import (
    OpperatorAgent,
    LogLevel,
    CommandArgument,
    CommandExposure,
    SlashCommandScope,
    SecretError,
)

import time
import threading
from typing import Any, Dict, Optional
from dataclasses import dataclass, field


# =============================================================================
# AGENT STATE (Optional - for organizing runtime data)
# =============================================================================

@dataclass
class AgentState:
    """Centralized state container. Use when tracking multiple runtime values."""
    heartbeat_count: int = 0
    last_activity: Optional[float] = None
    metrics: Dict[str, Any] = field(default_factory=dict)


# =============================================================================
# MAIN AGENT CLASS
# =============================================================================

class ComprehensiveAgent(OpperatorAgent):
    """Comprehensive template demonstrating all SDK features."""

    def __init__(self) -> None:
        """Initialize instance variables before initialize()."""
        super().__init__(
            name="ComprehensiveAgent",
            version="1.0.0",
            # max_async_workers=4  # Custom thread pool size
        )

        self.state = AgentState()
        self._state_lock = threading.Lock()
        self._stop_worker = threading.Event()
        self._worker_thread: Optional[threading.Thread] = None

    # ---------------------------------------------------------------------------
    # REQUIRED: initialize()
    # ---------------------------------------------------------------------------

    def initialize(self) -> None:
        """
        Initialize agent - called once before start().
        Register commands, sections, set description and system prompt.
        """

        # Set description and system prompt
        self.set_description(
            "Comprehensive template demonstrating all Opperator SDK features."
        )

        self.set_system_prompt(
            "You are a demo agent showcasing the Opperator SDK. "
            "Help users understand how to build agents."
        )

        # -----------------------------------------------------------------------
        # COMMAND REGISTRATION EXAMPLES (uncomment as needed)
        # -----------------------------------------------------------------------

        # Simple command
        # self.register_command("ping", self._cmd_ping)

        # Full-featured command with all options
        # self.register_command(
        #     "get_status",
        #     self._cmd_get_status,
        #     title="Get Agent Status",
        #     description="Returns current status including uptime and metrics",
        #     expose_as=[CommandExposure.AGENT_TOOL, CommandExposure.SLASH_COMMAND],
        #     slash_command="/status",
        #     slash_scope=SlashCommandScope.LOCAL,
        # )

        # Command with typed arguments
        # self.register_command(
        #     "greet",
        #     self._cmd_greet,
        #     title="Greet User",
        #     description="Generate personalized greeting",
        #     expose_as=[CommandExposure.AGENT_TOOL],
        #     arguments=[
        #         CommandArgument("name", "string", "Person's name", required=True),
        #         CommandArgument("enthusiasm", "integer", "Level 1-10", default=5),
        #     ],
        # )

        # Async command with progress reporting
        # self.register_command(
        #     "run_task",
        #     self._cmd_run_task,
        #     title="Run Long Task",
        #     description="Long-running task with progress updates",
        #     arguments=[
        #         CommandArgument("duration", "integer", "Task duration (seconds)", default=10),
        #     ],
        #     async_enabled=True,
        #     progress_label="Running task",
        # )

        # Array arguments - strings
        # self.register_command(
        #     "add_tags",
        #     self._cmd_add_tags,
        #     arguments=[
        #         CommandArgument("item_id", "string", "Item ID", required=True),
        #         CommandArgument("tags", "array", "List of tags",
        #                       items={"type": "string"}, required=True),
        #     ],
        # )

        # Array arguments - integers
        # self.register_command(
        #     "batch_update_ages",
        #     self._cmd_batch_update_ages,
        #     arguments=[
        #         CommandArgument("user_ids", "array", "User IDs to update",
        #                       items={"type": "integer"}, required=True),
        #         CommandArgument("increment", "integer", "Age increment", default=1),
        #     ],
        # )

        # Array arguments - objects
        # self.register_command(
        #     "create_users",
        #     self._cmd_create_users,
        #     arguments=[
        #         CommandArgument("users", "array", "User objects to create",
        #                       items={
        #                           "type": "object",
        #                           "properties": {
        #                               "name": {"type": "string", "required": True},
        #                               "email": {"type": "string", "required": True},
        #                               "age": {"type": "integer"},
        #                               "active": {"type": "boolean"},
        #                           },
        #                       }, required=True),
        #     ],
        # )

        # -----------------------------------------------------------------------
        # SIDEBAR SECTIONS (uncomment as needed)
        # -----------------------------------------------------------------------

        # self.register_section(
        #     "status",
        #     "Agent Status",
        #     '<c fg="green">● Running</c>',
        #     collapsed=False,
        # )

        # self.register_section(
        #     "metrics",
        #     "Metrics",
        #     "<b>Heartbeats:</b> 0\n<b>Last activity:</b> Never",
        #     collapsed=True,
        # )

        self.log(LogLevel.INFO, "Agent initialization complete")

    # ---------------------------------------------------------------------------
    # REQUIRED: start()
    # ---------------------------------------------------------------------------

    def start(self) -> None:
        """
        Start agent - called after initialize().
        Start background threads, open connections, begin processing.
        """

        # Start background worker (uncomment to enable)
        # self._stop_worker.clear()
        # self._worker_thread = threading.Thread(
        #     target=self._heartbeat_worker,
        #     name="heartbeat-worker",
        #     daemon=True,
        # )
        # self._worker_thread.start()

        self.log(LogLevel.INFO, "Agent started")

    # ===========================================================================
    # COMMAND HANDLERS
    # ===========================================================================

    def _cmd_ping(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """Simple command - returns pong."""
        return {"message": "pong", "timestamp": time.time()}

    def _cmd_get_status(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """Returns current status. Use locks when reading shared state."""
        with self._state_lock:
            return {
                "running": self.running,
                "heartbeat_count": self.state.heartbeat_count,
                "last_activity": self.state.last_activity,
                "metrics": self.state.metrics.copy(),
            }

    def _cmd_greet(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """Generate personalized greeting."""
        name = args.get("name", "friend")
        enthusiasm = args.get("enthusiasm", 5)
        exclamation = "!" * min(enthusiasm, 10)
        greeting = f"Hello, {name}{exclamation}"

        self.log(LogLevel.INFO, "Generated greeting", name=name, enthusiasm=enthusiasm)
        return {"greeting": greeting, "enthusiasm": enthusiasm}

    def _cmd_run_task(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """
        Long-running task with progress reporting.
        Use self.report_progress() to send updates.
        """
        duration = max(1, args.get("duration", 10))
        self.log(LogLevel.INFO, "Starting long task", duration=duration)
        start_time = time.time()

        for i in range(duration):
            self.report_progress(
                text=f"Processing step {i + 1}/{duration}",
                progress=(i + 1) / duration,
                metadata={"step": i + 1},
                status="running",
            )
            time.sleep(1)

        elapsed = time.time() - start_time
        self.log(LogLevel.INFO, "Task completed", elapsed=elapsed)
        return {"success": True, "elapsed": elapsed}

    def _cmd_add_tags(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """Add tags (array of strings)."""
        item_id = args["item_id"]
        tags = args["tags"]  # Validated as array of strings

        self.log(LogLevel.INFO, f"Adding {len(tags)} tags to {item_id}", tags=tags)
        return {"success": True, "item_id": item_id, "tags_added": tags, "count": len(tags)}

    def _cmd_batch_update_ages(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """Batch update ages (array of integers)."""
        user_ids = args["user_ids"]  # Validated as array of integers
        increment = args["increment"]

        self.log(LogLevel.INFO, f"Updating {len(user_ids)} users by {increment}")
        return {"success": True, "users_updated": len(user_ids), "increment": increment}

    def _cmd_create_users(self, args: Dict[str, Any]) -> Dict[str, Any]:
        """Create users (array of objects)."""
        users = args["users"]  # Validated as array of user objects

        self.log(LogLevel.INFO, f"Creating {len(users)} users")

        created = []
        for user in users:
            created.append({
                "id": len(created) + 1,
                "name": user["name"],
                "email": user["email"],
                "age": user.get("age"),
                "active": user.get("active", True),
            })

        return {"success": True, "users_created": len(created), "users": created}

    # ===========================================================================
    # BACKGROUND WORKER
    # ===========================================================================

    # def _heartbeat_worker(self) -> None:
    #     """Background worker - runs periodically until shutdown."""
    #     while not self._stop_worker.is_set():
    #         with self._state_lock:
    #             self.state.heartbeat_count += 1
    #             self.state.last_activity = time.time()
    #             count = self.state.heartbeat_count
    #
    #         self.log(LogLevel.DEBUG, "Heartbeat", count=count)
    #
    #         # Update sidebar
    #         self.update_section(
    #             "metrics",
    #             f"<b>Heartbeats:</b> {count}\n"
    #             f'<b>Last activity:</b> {time.strftime("%H:%M:%S")}',
    #         )
    #
    #         # Wait 30s (interruptible by shutdown)
    #         self._stop_worker.wait(timeout=30.0)

    # ===========================================================================
    # OPTIONAL LIFECYCLE HOOKS
    # ===========================================================================

    # def main_loop(self) -> None:
    #     """Override for custom event loop. Default: wait for shutdown."""
    #     self.lifecycle.wait_for_shutdown()

    # def on_shutdown(self) -> None:
    #     """Called on SIGTERM/SIGINT. Stop workers, close connections, save state."""
    #     self.log(LogLevel.INFO, "Shutting down gracefully")
    #     self._stop_worker.set()

    # def cleanup(self) -> None:
    #     """Final cleanup before exit. Always call super().cleanup()!"""
    #     self._stop_worker.set()
    #     if self._worker_thread and self._worker_thread.is_alive():
    #         self._worker_thread.join(timeout=2)
    #     super().cleanup()

    # def on_config_update(self, config: Dict[str, Any]) -> None:
    #     """Called when config reloaded (SIGHUP)."""
    #     new_level = config.get("log_level", "INFO")
    #     self.log(LogLevel.INFO, "Config updated", level=new_level)

    # def on_status(self) -> None:
    #     """Called on SIGUSR1 - log diagnostic info."""
    #     with self._state_lock:
    #         self.log(LogLevel.INFO, "Status check",
    #                  heartbeats=self.state.heartbeat_count,
    #                  running=self.running)

    # ===========================================================================
    # LIFECYCLE EVENT HANDLERS
    # ===========================================================================

    # def on_new_conversation(self, conversation_id: str, is_clear: bool) -> None:
    #     """New conversation created or /clear executed."""
    #     source = "cleared" if is_clear else "created"
    #     self.log(LogLevel.INFO, f"Conversation {source}", id=conversation_id)

    # def on_conversation_switched(self, conversation_id: str, previous_id: str, message_count: int) -> None:
    #     """User switched to different conversation."""
    #     self.log(LogLevel.INFO, "Switched conversations",
    #              from_id=previous_id, to_id=conversation_id, messages=message_count)

    # def on_conversation_deleted(self, conversation_id: str) -> None:
    #     """Conversation being deleted - clean up resources."""
    #     self.log(LogLevel.INFO, "Conversation deleted", id=conversation_id)

    # def on_agent_activated(self, previous_agent: Optional[str], conversation_id: str) -> None:
    #     """This agent became active."""
    #     self.log(LogLevel.INFO, "Agent activated",
    #              previous=previous_agent, conversation=conversation_id)
    #     self.update_section("status", '<c fg="green">● Active</c>')

    # def on_agent_deactivated(self, next_agent: Optional[str]) -> None:
    #     """User switched away from this agent."""
    #     self.log(LogLevel.INFO, "Agent deactivated", next=next_agent)
    #     self.update_section("status", '<c fg="yellow">○ Inactive</c>')

    # def on_working_directory_changed(self, old_path: str, new_path: str) -> None:
    #     """Working directory changed."""
    #     self.log(LogLevel.INFO, "Working directory changed", old=old_path, new=new_path)

    # ===========================================================================
    # UTILITY PATTERNS
    # ===========================================================================

    # def _fetch_secret_example(self) -> Optional[str]:
    #     """Fetch secret from Opperator keyring. Never log or store secrets!"""
    #     try:
    #         secret = self.get_secret("my_api_key", timeout=5.0)
    #         return secret
    #     except SecretError as e:
    #         self.log(LogLevel.ERROR, "Secret fetch failed", error=str(e))
    #         return None

    # def _get_working_directory_example(self) -> Dict[str, Any]:
    #     """Get current working directory (set per-command by daemon)."""
    #     import os
    #     cwd = self.get_working_directory()
    #     files = os.listdir(cwd)
    #     return {"cwd": cwd, "files": files}

    # def _sidebar_markup_examples(self) -> None:
    #     """
    #     Sidebar markup examples:
    #     - <c fg="color">text</c> or <c fg="#RRGGBB">text</c>
    #     - <b>text</b>
    #     - Colors: red, green, yellow, blue, cyan, magenta, white, black, #RRGGBB
    #     """
    #     self.update_section(
    #         "connection",
    #         '<c fg="green">● Connected</c>\n'
    #         '<c fg="#808080">Server: api.example.com</c>\n'
    #         '<c fg="#808080">Latency: 45ms</c>',
    #     )


# =============================================================================
# ENTRY POINT
# =============================================================================

if __name__ == "__main__":
    ComprehensiveAgent().run()
