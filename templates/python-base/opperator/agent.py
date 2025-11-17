"""Base class for Opperator-managed agents."""

import copy
import json
import os
import socket
import sys
import tempfile
import threading
import traceback
from abc import ABC, abstractmethod
from concurrent.futures import ThreadPoolExecutor
from typing import Any, Callable, Dict, Iterable, List, Optional, Sequence, Set, Union

from .protocol import (
    Protocol,
    LogLevel,
    MessageType,
    Message,
    CommandMessage,
    LifecycleEventMessage,
    CommandDefinition,
    CommandArgument,
    CommandExposure,
    SlashCommandScope,
)
from . import secrets as secret_client
from .lifecycle import LifecycleManager
from . import cli


def _fetch_invocation_directory_from_daemon(timeout: float = 2.0) -> Optional[str]:
    """Fetch the current invocation directory from the daemon.

    Returns None if unable to fetch (daemon not available, etc.)
    """
    socket_path = os.environ.get("OPPERATOR_SOCKET_PATH")
    if not socket_path:
        socket_path = os.path.join(tempfile.gettempdir(), "opperator.sock")

    payload = {"type": "get_invocation_dir"}

    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
            sock.settimeout(timeout)
            sock.connect(socket_path)
            raw = json.dumps(payload).encode("utf-8") + b"\n"
            sock.sendall(raw)

            # Read response
            with sock.makefile("r", encoding="utf-8") as reader:
                line = reader.readline()
                if not line:
                    return None

                response = json.loads(line)
                if response.get("success") and response.get("invocation_dir"):
                    return response["invocation_dir"]
                return None
    except (OSError, json.JSONDecodeError, KeyError):
        # Silently fail - invocation directory is optional at startup
        return None


class OpperatorAgent(ABC):
    """Base class for creating Opperator-managed agents."""

    def __init__(
        self,
        name: str = None,
        version: str = "1.0.0",
        *,
        max_async_workers: Optional[int] = None,
    ):
        self.name = name or self.__class__.__name__
        self.version = version
        self.config: Dict[str, Any] = {}
        self.running = False
        self._system_prompt: Optional[str] = None
        self._description: Optional[str] = None

        # Core components
        self.lifecycle = LifecycleManager()

        # Command handling
        self._command_handlers: Dict[str, Callable[[Dict[str, Any]], Any]] = {}
        self._command_metadata: Dict[str, CommandDefinition] = {}
        self._argument_schemas: Dict[str, List[CommandArgument]] = {}
        self._message_thread: Optional[threading.Thread] = None
        self._stop_reading = threading.Event()
        self._command_state = threading.local()
        self._invocation_dir: Optional[str] = None
        self._max_async_workers = (
            max_async_workers if (max_async_workers or 0) > 0 else None
        )
        self._async_executor: Optional[ThreadPoolExecutor] = None
        self._async_executor_lock = threading.Lock()

        # Sidebar sections
        self._sidebar_sections: Dict[str, Dict[str, Any]] = {}

        # Agent invocation
        self._exec_client: Optional[cli.ExecClient] = None

        # Setup lifecycle handlers
        self.lifecycle.on_shutdown(self._handle_shutdown)
        self.lifecycle.on_reload(self._handle_reload)
        self.lifecycle.on_status(self._handle_status)

    def log(self, level: LogLevel, message: str, **fields):
        """Send a log message"""
        Protocol.send_log(level, message, fields if fields else None)

    def register_command(
        self,
        name: str,
        handler: Callable[[Dict[str, Any]], Any],
        *,
        title: Optional[str] = None,
        description: Optional[str] = None,
        expose_as: Optional[Sequence[CommandExposure]] = None,
        slash_command: Optional[str] = None,
        slash_scope: Optional[Union[str, SlashCommandScope]] = None,
        argument_hint: Optional[str] = None,
        argument_required: bool = False,
        arguments: Optional[Sequence[Union[CommandArgument, Dict[str, Any]]]] = None,
        async_enabled: bool = False,
        progress_label: Optional[str] = None,
        hidden: bool = False,
    ):
        """Register a command handler."""

        if not callable(handler):
            raise TypeError("handler must be callable")

        command_name = self._normalize_command_name(name)
        exposures = self._parse_exposures(expose_as)

        definition = CommandDefinition(
            name=command_name,
            title=title,
            description=description,
            expose_as=exposures,
            slash_command=slash_command,
            slash_scope=slash_scope,
            argument_hint=argument_hint,
            argument_required=argument_required,
            arguments=arguments,
            async_enabled=async_enabled,
            progress_label=progress_label,
            hidden=hidden,
        )

        normalized_definition = definition.normalized()

        self._command_handlers[command_name] = handler
        self._command_metadata[command_name] = normalized_definition
        self._argument_schemas[command_name] = list(
            normalized_definition.arguments or []
        )
        self._publish_command_registry()

    def unregister_command(self, name: str) -> bool:
        """Unregister a previously registered command.

        Args:
            name: The name of the command to unregister

        Returns:
            True if the command was successfully unregistered, False if it didn't exist
        """
        command_name = self._normalize_command_name(name)

        if command_name not in self._command_handlers:
            self.log(
                LogLevel.WARNING,
                f"Attempted to unregister non-existent command: {command_name}"
            )
            return False

        del self._command_handlers[command_name]
        del self._command_metadata[command_name]
        del self._argument_schemas[command_name]

        self._publish_command_registry()
        return True

    def _command_definitions(self) -> Iterable[CommandDefinition]:
        for name in sorted(self._command_handlers.keys()):
            definition = self._command_metadata.get(name)
            if definition is None:
                definition = CommandDefinition(name=name)
            yield definition.normalized()

    def set_system_prompt(self, prompt: Optional[str], *, replace: bool = False) -> None:
        """Set the system prompt that should guide the managed agent.

        Args:
            prompt: The instructions to provide to the LLM.
            replace: When True, replace Opperator's base system prompt entirely.
        """

        self._system_prompt = (prompt or "").strip()
        Protocol.send_system_prompt(self._system_prompt, replace=replace)

    def set_description(self, description: Optional[str]) -> None:
        """Publish a human-readable description for the managed agent."""

        self._description = (description or "").strip()
        Protocol.send_agent_description(self._description)

    def register_section(self, section_id: str, title: str, content: str, collapsed: bool = False) -> None:
        """Register or update a custom sidebar section.

        Args:
            section_id: Unique identifier for the section
            title: Display title for the section header
            content: Section content with optional XML-like markup for colors/styling
            collapsed: Whether the section should start collapsed (default: False)

        Example:
            self.register_section(
                "metrics",
                "System Metrics",
                '<b>CPU:</b> <c fg="green">23%</c>\\n<b>Memory:</b> <c fg="yellow">67%</c>'
            )
        """
        section_id = str(section_id or "").strip()
        if not section_id:
            raise ValueError("section_id cannot be empty")

        title = str(title or "").strip()
        if not title:
            title = section_id

        self._sidebar_sections[section_id] = {
            'title': title,
            'content': str(content),
            'collapsed': bool(collapsed)
        }

        Protocol.send_sidebar_section(section_id, title, content, collapsed)

    def update_section(self, section_id: str, content: str) -> None:
        """Update the content of an existing sidebar section.

        Args:
            section_id: The ID of the section to update
            content: New content for the section

        Example:
            self.update_section("status", '<c fg="green">Connected</c> - 5 peers')
        """
        section_id = str(section_id or "").strip()
        if not section_id:
            raise ValueError("section_id cannot be empty")

        if section_id not in self._sidebar_sections:
            # If section doesn't exist yet, register it with a default title
            self.register_section(section_id, section_id, content)
            return

        # Update the stored content
        section = self._sidebar_sections[section_id]
        section['content'] = str(content)

        # Send the update
        Protocol.send_sidebar_section(
            section_id,
            section['title'],
            content,
            section['collapsed']
        )

    def unregister_section(self, section_id: str) -> bool:
        """Unregister a previously registered sidebar section.

        Args:
            section_id: The ID of the section to unregister

        Returns:
            True if the section was successfully unregistered, False if it didn't exist
        """
        section_id = str(section_id or "").strip()
        if not section_id:
            raise ValueError("section_id cannot be empty")

        if section_id not in self._sidebar_sections:
            self.log(
                LogLevel.WARNING,
                f"Attempted to unregister non-existent section: {section_id}"
            )
            return False

        del self._sidebar_sections[section_id]
        Protocol.send_sidebar_section_removal(section_id)
        return True

    def _parse_exposures(
        self, expose_as: Optional[Sequence[CommandExposure]]
    ) -> Optional[Iterable[CommandExposure]]:
        if expose_as is None:
            return None
        exposures: List[CommandExposure] = []
        items: Sequence[Any]
        if isinstance(expose_as, (list, tuple, set)):
            items = expose_as
        else:
            items = (expose_as,)
        seen: Set[CommandExposure] = set()
        for item in items:
            if isinstance(item, CommandExposure):
                exposure = item
            else:
                try:
                    exposure = CommandExposure(str(item))
                except ValueError as exc:
                    raise ValueError(f"unknown command exposure '{item}'") from exc
            if exposure not in seen:
                exposures.append(exposure)
                seen.add(exposure)
        return exposures

    @staticmethod
    def _normalize_command_name(name: str) -> str:
        trimmed = str(name or "").strip()
        if not trimmed:
            raise ValueError("command name must be a non-empty string")
        return trimmed

    def _publish_command_registry(self):
        Protocol.send_command_registry(self._command_definitions())

    def report_progress(
        self,
        text: Optional[str] = None,
        *,
        metadata: Optional[Dict[str, Any]] = None,
        status: Optional[str] = None,
        progress: Optional[float] = None,
    ) -> None:
        """Report incremental progress for the currently executing command."""

        command_id = getattr(self._command_state, "command_id", None)
        if not command_id:
            return
        Protocol.send_command_progress(
            command_id, text=text, metadata=metadata, status=status, progress=progress
        )

    def get_secret(self, name: str, *, timeout: float = 5.0) -> str:
        """Fetch a named secret from the Opperator daemon."""

        return secret_client.get_secret(name, timeout=timeout)

    def _get_exec_client(self) -> cli.ExecClient:
        """Get or create exec client (lazy initialization)."""
        if self._exec_client is None:
            self._exec_client = cli.ExecClient()
        return self._exec_client

    def invoke_agent(
        self,
        agent: str,
        message: str,
        timeout: int = 60,
        save: bool = True,
        event_callback: Optional[Callable[[Any], None]] = None
    ) -> str:
        """Invoke another agent with a message.

        Wrapper around 'op exec <message> --agent=<agent> --json'.

        Args:
            agent: Target agent name
            message: Message to send to the agent
            timeout: Timeout in seconds [not enforced in subprocess mode]
            save: Whether to save the conversation
            event_callback: Optional callback for streaming events

        Returns:
            Agent's response as a string

        Raises:
            AgentInvocationError: If invocation fails

        Example:
            result = self.invoke_agent("helper-agent", "What is the weather?")
            self.log(LogLevel.INFO, f"Helper responded: {result}")

            # With event streaming:
            def on_event(event):
                if event.type == EventType.ITEM_UPDATED:
                    print(f"Streaming: {event.item.text}")

            result = self.invoke_agent("helper-agent", "Hello", event_callback=on_event)
        """
        client = self._get_exec_client()
        result = client.exec(message=message, agent=agent, no_save=not save, event_callback=event_callback)

        if not result.success:
            raise cli.AgentInvocationError(result.error or "Unknown error")

        return result.response

    def invoke_agent_command(
        self,
        agent: str,
        command: str,
        args: Optional[Dict[str, Any]] = None,
        working_dir: Optional[str] = None,
        timeout: int = 60,
        progress_callback: Optional[Callable[[str], None]] = None
    ) -> Any:
        """Invoke a specific command on another agent.

        This provides low-level access to agent commands, equivalent to:
        op agent command <agent> <command> --args=<json>

        Use this when you need direct command invocation with full control
        over arguments and want to handle progress updates.

        Args:
            agent: Target agent name
            command: Command name to invoke
            args: Command arguments as a dictionary
            working_dir: Working directory for the command
            timeout: Timeout in seconds (default: 60)
            progress_callback: Optional callback for progress updates (receives formatted text)

        Returns:
            Command result (type depends on the command)

        Raises:
            AgentInvocationError: If invocation fails
            AgentNotFoundError: If agent doesn't exist
            DaemonNotRunningError: If daemon is not running
            TimeoutError: If invocation times out

        Example:
            # Invoke with progress tracking
            def on_progress(text):
                self.update_section("status", text)

            result = self.invoke_agent_command(
                agent="file-processor",
                command="process_file",
                args={"path": "/tmp/data.csv", "format": "json"},
                progress_callback=on_progress
            )
            self.log(LogLevel.INFO, f"Processed: {result}")
        """
        result = cli.command(
            agent=agent,
            command_name=command,
            args=args,
            progress_callback=progress_callback
        )

        if not result.success:
            if "not found" in (result.error or "").lower():
                raise cli.AgentNotFoundError(f"Agent '{agent}' not found: {result.error}")
            raise cli.AgentInvocationError(result.error or "Unknown error")

        return result.result

    def _start_message_reader(self):
        """Start background thread to process incoming messages"""

        def read_loop():
            while not self._stop_reading.is_set():
                try:
                    line = sys.stdin.readline()
                    if not line:
                        break

                    line = line.strip()
                    if not line:
                        continue

                    try:
                        msg = Message.from_json(line)
                        self._handle_message(msg)
                    except json.JSONDecodeError:
                        # Ignore non-JSON output
                        continue
                except Exception as exc:
                    Protocol.send_error(f"Error reading manager message: {exc}")
                    break

        self._stop_reading.clear()
        self._message_thread = threading.Thread(target=read_loop, daemon=True)
        self._message_thread.start()

    def _handle_message(self, msg: Message):
        if msg.type == MessageType.COMMAND and msg.data:
            cmd = CommandMessage.from_dict(msg.data)
            if cmd.command:
                self._handle_command(cmd)
        elif msg.type == MessageType.LIFECYCLE_EVENT and msg.data:
            event = LifecycleEventMessage.from_dict(msg.data)
            self._handle_lifecycle_event(event)

    def _handle_command(self, cmd: CommandMessage):
        # Safely retrieve invocation_dir (where user ran 'op' from)
        invocation_dir = self._resolve_invocation_dir(getattr(cmd, "working_dir", None))
        if invocation_dir:
            self._invocation_dir = invocation_dir

        if cmd.command == "__list_commands":
            self._set_command_context(cmd.id, invocation_dir)
            try:
                commands = [
                    definition.to_dict() for definition in self._command_definitions()
                ]
                Protocol.send_response(success=True, command_id=cmd.id, result=commands)
            finally:
                self._clear_command_context()
            return

        handler = self._command_handlers.get(cmd.command)
        if not handler:
            Protocol.send_response(
                success=False,
                command_id=cmd.id,
                error=f"Unknown command: {cmd.command}",
            )
            return

        try:
            definition = self._command_metadata.get(cmd.command)
            schema = self._argument_schemas.get(cmd.command)
            prepared_args = self._prepare_command_arguments(
                definition, schema, cmd.args
            )
        except ValueError as exc:
            Protocol.send_response(success=False, command_id=cmd.id, error=str(exc))
            return

        async_enabled = bool(definition.async_enabled) if definition else False
        if async_enabled:
            self._submit_async_command(cmd, handler, prepared_args, invocation_dir)
            return

        self._execute_command(cmd, handler, prepared_args, invocation_dir)

    def _submit_async_command(
        self,
        cmd: CommandMessage,
        handler: Callable[[Dict[str, Any]], Any],
        prepared_args: Dict[str, Any],
        invocation_dir: Optional[str],
    ) -> None:
        try:
            executor = self._ensure_async_executor()
            executor.submit(
                self._execute_command, cmd, handler, prepared_args, invocation_dir
            )
        except Exception as exc:  # pragma: no cover - defensive guard
            self.log(
                LogLevel.ERROR,
                f"Failed to schedule async command '{cmd.command}'",
                error=str(exc),
            )
            Protocol.send_response(success=False, command_id=cmd.id, error=str(exc))

    def _execute_command(
        self,
        cmd: CommandMessage,
        handler: Callable[[Dict[str, Any]], Any],
        prepared_args: Dict[str, Any],
        invocation_dir: Optional[str],
    ) -> None:
        self._set_command_context(cmd.id, invocation_dir)
        try:
            result = handler(prepared_args)
        except Exception as exc:  # pragma: no cover - handler-specific failures
            self.log(LogLevel.ERROR, f"Command '{cmd.command}' failed", error=str(exc))
            Protocol.send_response(success=False, command_id=cmd.id, error=str(exc))
        else:
            Protocol.send_response(success=True, command_id=cmd.id, result=result)
        finally:
            self._clear_command_context()

    def _set_command_context(
        self, command_id: Optional[str], invocation_dir: Optional[str]
    ) -> None:
        setattr(self._command_state, "command_id", command_id)
        setattr(self._command_state, "invocation_directory", invocation_dir)

    def _clear_command_context(self) -> None:
        setattr(self._command_state, "command_id", None)
        setattr(self._command_state, "invocation_directory", None)

    def _ensure_async_executor(self) -> ThreadPoolExecutor:
        if self._async_executor is not None:
            return self._async_executor
        with self._async_executor_lock:
            if self._async_executor is not None:
                return self._async_executor

            max_workers = self._max_async_workers
            if max_workers is None:
                cpu_count = os.cpu_count() or 1
                max_workers = max(1, min(8, cpu_count))

            prefix = self._async_thread_prefix()
            self._async_executor = ThreadPoolExecutor(
                max_workers=max_workers,
                thread_name_prefix=prefix,
            )
            return self._async_executor

    def _async_thread_prefix(self) -> str:
        base = (self.name or "opperator-agent").strip().lower().replace(" ", "-")
        if not base:
            base = "opperator-agent"
        return base[:32]

    def _shutdown_async_executor(self) -> None:
        with self._async_executor_lock:
            if self._async_executor is None:
                return
            self._async_executor.shutdown(wait=False)
            self._async_executor = None

    def _resolve_invocation_dir(self, raw_value: Optional[str]) -> Optional[str]:
        """Resolve the invocation directory (where user ran 'op' from)"""
        candidate = raw_value if raw_value else self._invocation_dir
        if not candidate:
            return None
        try:
            expanded = os.path.expanduser(str(candidate))
            return os.path.abspath(expanded)
        except (OSError, TypeError, ValueError):
            return None

    def get_invocation_directory(self) -> Optional[str]:
        """Get the directory where the user invoked 'op' or 'opperator' from.

        This returns the user's current working directory when they ran the op command,
        NOT the directory where the agent process is running.

        Returns:
            The invocation directory path, or None if not set yet
        """
        invocation_dir = getattr(self._command_state, "invocation_directory", None)
        if invocation_dir:
            return invocation_dir
        if self._invocation_dir:
            return self._invocation_dir
        return None

    def get_working_directory(self) -> str:
        """Get the directory where the agent process is running.

        This is the agent's working directory (e.g., ~/.config/opperator/agents/my-agent),
        NOT where the user invoked the 'op' command from.

        Returns:
            The agent's process working directory
        """
        return os.path.abspath(os.getcwd())

    def _prepare_command_arguments(
        self,
        definition: Optional[CommandDefinition],
        schema: Optional[Sequence[CommandArgument]],
        args: Optional[Dict[str, Any]],
    ) -> Dict[str, Any]:
        incoming = dict(args or {})
        if schema:
            return self._coerce_arguments_from_schema(list(schema), incoming)

        if definition and definition.argument_required and not incoming:
            raise ValueError("Arguments are required for this command")

        return incoming

    def _coerce_arguments_from_schema(
        self,
        schema: Sequence[CommandArgument],
        incoming: Dict[str, Any],
    ) -> Dict[str, Any]:
        normalized: Dict[str, Any] = {}
        for argument in schema:
            name = argument.name
            has_value = name in incoming and incoming[name] is not None
            value = incoming.get(name)

            if not has_value:
                if argument.default is not None:
                    value = copy.deepcopy(argument.default)
                elif argument.required:
                    raise ValueError(f"Missing required argument '{name}'")
                else:
                    continue
            else:
                value = self._coerce_argument_value(
                    argument.type, value, argument.items, argument.properties
                )

            if argument.enum and value not in argument.enum:
                raise ValueError(
                    f"Invalid value for '{name}'; expected one of {list(argument.enum)}"
                )

            normalized[name] = value

        # Preserve additional arguments that aren't in the schema
        for key, value in incoming.items():
            if key not in normalized:
                normalized[key] = value

        return normalized

    def _coerce_argument_value(
        self,
        arg_type: str,
        value: Any,
        items: Optional[Dict[str, Any]] = None,
        properties: Optional[Dict[str, Any]] = None,
    ) -> Any:
        if value is None:
            return None

        arg_type = (arg_type or "string").lower()

        if arg_type == "string":
            return str(value)

        if arg_type == "integer":
            if isinstance(value, bool):
                raise ValueError("Boolean value is not valid for integer argument")
            if isinstance(value, int):
                return value
            if isinstance(value, float) and value.is_integer():
                return int(value)
            if isinstance(value, str):
                stripped = value.strip()
                return int(stripped, 10)
            return int(value)

        if arg_type == "number":
            if isinstance(value, bool):
                raise ValueError("Boolean value is not valid for number argument")
            if isinstance(value, (int, float)):
                return float(value)
            if isinstance(value, str):
                stripped = value.strip()
                return float(stripped)
            return float(value)

        if arg_type == "boolean":
            if isinstance(value, bool):
                return value
            if isinstance(value, str):
                lowered = value.strip().lower()
                if lowered in {"true", "1", "yes", "on"}:
                    return True
                if lowered in {"false", "0", "no", "off"}:
                    return False
                raise ValueError(f"Cannot interpret '{value}' as boolean")
            if isinstance(value, (int, float)):
                return bool(value)
            raise ValueError(f"Cannot interpret '{value}' as boolean")

        if arg_type == "array":
            if isinstance(value, list):
                arr = value
            elif isinstance(value, str):
                try:
                    parsed = json.loads(value)
                except json.JSONDecodeError as exc:
                    raise ValueError(f"Cannot interpret '{value}' as array: {exc}")
                if not isinstance(parsed, list):
                    raise ValueError("Expected a list for array argument")
                arr = parsed
            else:
                raise ValueError("Expected a list for array argument")

            # Validate and coerce array items if items schema is provided
            if items:
                item_type = items.get("type", "string")
                item_items = items.get("items")
                item_properties = items.get("properties")
                coerced_arr = []
                for idx, item in enumerate(arr):
                    try:
                        coerced_item = self._coerce_argument_value(
                            item_type, item, item_items, item_properties
                        )
                        coerced_arr.append(coerced_item)
                    except (ValueError, TypeError) as exc:
                        raise ValueError(
                            f"Array item at index {idx} is invalid: {exc}"
                        )
                return coerced_arr
            return arr

        if arg_type == "object":
            if isinstance(value, dict):
                obj = value
            elif isinstance(value, str):
                try:
                    parsed = json.loads(value)
                except json.JSONDecodeError as exc:
                    raise ValueError(f"Cannot interpret '{value}' as object: {exc}")
                if not isinstance(parsed, dict):
                    raise ValueError("Expected a mapping for object argument")
                obj = parsed
            else:
                raise ValueError("Expected a mapping for object argument")

            # Validate and coerce object properties if properties schema is provided
            if properties:
                coerced_obj = {}
                for prop_name, prop_schema in properties.items():
                    if prop_name in obj:
                        prop_type = prop_schema.get("type", "string")
                        prop_items = prop_schema.get("items")
                        prop_properties = prop_schema.get("properties")
                        try:
                            coerced_value = self._coerce_argument_value(
                                prop_type, obj[prop_name], prop_items, prop_properties
                            )
                            coerced_obj[prop_name] = coerced_value
                        except (ValueError, TypeError) as exc:
                            raise ValueError(
                                f"Object property '{prop_name}' is invalid: {exc}"
                            )
                    elif prop_schema.get("required", False):
                        raise ValueError(
                            f"Object is missing required property '{prop_name}'"
                        )
                # Preserve additional properties not in schema
                for key, val in obj.items():
                    if key not in coerced_obj:
                        coerced_obj[key] = val
                return coerced_obj
            return obj

        # Fallback: pass through as-is
        return value

    def _handle_shutdown(self):
        """Handle shutdown signal"""
        self.log(LogLevel.INFO, "Shutting down...")
        self.running = False
        self.on_shutdown()

    def _handle_reload(self):
        """Handle reload signal"""
        self.log(LogLevel.INFO, "Reloading configuration...")
        self.reload_config()

    def _handle_status(self):
        """Handle status request"""
        try:
            self.on_status()
        except Exception as exc:
            self.log(LogLevel.WARNING, "Status handler failed", error=str(exc))

    def _handle_lifecycle_event(self, event: LifecycleEventMessage):
        """Handle lifecycle event from manager"""
        event_type = event.event_type
        data = event.data or {}

        try:
            if event_type == "new_conversation":
                self.on_new_conversation(
                    data.get("conversation_id", ""),
                    data.get("is_clear", False)
                )
            elif event_type == "conversation_switched":
                self.on_conversation_switched(
                    data.get("conversation_id", ""),
                    data.get("previous_id", ""),
                    data.get("message_count", 0)
                )
            elif event_type == "conversation_deleted":
                self.on_conversation_deleted(data.get("conversation_id", ""))
            elif event_type == "agent_activated":
                self.on_agent_activated(
                    data.get("previous_agent"),
                    data.get("conversation_id", "")
                )
            elif event_type == "agent_deactivated":
                self.on_agent_deactivated(data.get("next_agent"))
            elif event_type == "invocation_directory_changed":
                # Store the new invocation directory
                new_path = data.get("new_path", "")
                if new_path:
                    self._invocation_dir = new_path
                # Call the hook
                self.on_invocation_directory_changed(
                    data.get("old_path", ""),
                    new_path
                )
        except Exception as exc:
            self.log(LogLevel.ERROR, f"Lifecycle event handler failed: {exc}")

    def run(self):
        """Main entry point for the process"""
        try:
            # Setup signal handlers
            self.lifecycle.setup_signal_handlers()

            # Fetch initial invocation directory from daemon
            initial_invocation_dir = _fetch_invocation_directory_from_daemon()
            if initial_invocation_dir:
                self._invocation_dir = initial_invocation_dir

            # Load initial configuration
            self.load_config()

            # Initialize the application
            self.log(LogLevel.INFO, f"Initializing {self.name} v{self.version}")
            self.initialize()

            # Publish available commands before signaling readiness
            self._publish_command_registry()

            # Start reading incoming protocol messages
            self._start_message_reader()

            # Send ready signal
            Protocol.send_ready(pid=os.getpid(), version=self.version)

            # Start the application
            self.log(LogLevel.INFO, f"{self.name} started successfully")
            self.running = True
            self.start()

            # Main loop
            self.main_loop()

        except KeyboardInterrupt:
            self.log(LogLevel.INFO, "Received keyboard interrupt")
            self._handle_shutdown()
        except Exception as e:
            self.log(
                LogLevel.FATAL, f"Fatal error: {e}", traceback=traceback.format_exc()
            )
            Protocol.send_error(str(e), code=1, details=traceback.format_exc())
            sys.exit(1)
        finally:
            self.running = False
            self.cleanup()

    def main_loop(self):
        """Main application loop - override if needed"""
        # Default implementation: wait for shutdown
        self.lifecycle.wait_for_shutdown()

    def load_config(self):
        """Load configuration - override to implement"""
        config_file = os.environ.get("CONFIG_FILE", "config.json")
        if os.path.exists(config_file):
            try:
                with open(config_file) as f:
                    self.config = json.load(f)
                self.log(LogLevel.INFO, f"Loaded configuration from {config_file}")
            except Exception as e:
                self.log(LogLevel.WARNING, f"Failed to load config: {e}")

    def reload_config(self):
        """Reload configuration"""
        old_config = self.config.copy()
        self.load_config()
        if self.config != old_config:
            self.on_config_update(self.config)

    @abstractmethod
    def initialize(self):
        """Initialize the application - must be implemented"""
        pass

    @abstractmethod
    def start(self):
        """Start the application - must be implemented"""
        pass

    def on_shutdown(self):
        """Called before shutdown - override to implement"""
        pass

    def cleanup(self):
        """Cleanup resources - override to implement"""
        self._stop_reading.set()
        if self._message_thread and self._message_thread.is_alive():
            self._message_thread.join(timeout=1)
        self._shutdown_async_executor()

    def on_config_update(self, config: Dict[str, Any]):
        """Called when configuration is updated - override to implement"""
        self.log(LogLevel.INFO, "Configuration updated", config=config)

    def on_status(self):
        """Called when a status signal is received - override to implement"""
        pass

    def on_new_conversation(self, conversation_id: str, is_clear: bool):
        """Called when a new conversation is created or /clear is executed

        Args:
            conversation_id: Unique conversation identifier
            is_clear: True if triggered by /clear, False if new conversation
        """
        pass

    def on_conversation_switched(self, conversation_id: str, previous_id: str, message_count: int):
        """Called when user switches to a different conversation

        Args:
            conversation_id: The conversation being switched to
            previous_id: The previous conversation ID
            message_count: Number of messages in the conversation
        """
        pass

    def on_conversation_deleted(self, conversation_id: str):
        """Called when a conversation is deleted

        Args:
            conversation_id: The deleted conversation ID
        """
        pass

    def on_agent_activated(self, previous_agent: Optional[str], conversation_id: str):
        """Called when this agent becomes active

        Args:
            previous_agent: Name of previously active agent (or None)
            conversation_id: Current conversation ID
        """
        pass

    def on_agent_deactivated(self, next_agent: Optional[str]):
        """Called when user switches away from this agent

        Args:
            next_agent: Name of agent being switched to (or None)
        """
        pass

    def on_invocation_directory_changed(self, old_path: str, new_path: str):
        """Called when the invocation directory changes.

        This is triggered when the user runs 'op' or 'opperator' from a different directory.
        The invocation directory is where the user runs the command from, NOT where the
        agent process is running.

        Args:
            old_path: Previous invocation directory (empty string on first notification)
            new_path: New invocation directory
        """
        pass
