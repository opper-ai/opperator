"""Thin wrappers around op CLI commands for agent-to-agent communication."""

import subprocess
import json
import sys
import shutil
from typing import Optional, Callable, Dict, Any, List
from dataclasses import dataclass
from enum import Enum


class AgentInvocationError(Exception):
    """Base exception for agent invocation errors."""
    pass


class AgentNotFoundError(AgentInvocationError):
    """Raised when target agent is not found."""
    pass


class DaemonNotRunningError(AgentInvocationError):
    """Raised when the daemon is not running."""
    pass


def _find_op_binary() -> str:
    """Find the op binary path in system PATH.

    Returns:
        Path to op binary

    Raises:
        FileNotFoundError: If op or opperator binary not found in PATH
    """
    # Try both 'op' and 'opperator' in system PATH
    for binary_name in ["op", "opperator"]:
        op_path = shutil.which(binary_name)
        if op_path:
            return op_path

    raise FileNotFoundError(
        "Could not find 'op' or 'opperator' binary. Please ensure it is in PATH."
    )


class EventType(str, Enum):
    """Event types emitted by op exec --json."""
    # Session events
    SESSION_STARTED = "session.started"
    SESSION_COMPLETED = "session.completed"
    SESSION_FAILED = "session.failed"
    # Turn events
    TURN_STARTED = "turn.started"
    TURN_COMPLETED = "turn.completed"
    TURN_FAILED = "turn.failed"
    # Item events
    ITEM_STARTED = "item.started"
    ITEM_UPDATED = "item.updated"
    ITEM_COMPLETED = "item.completed"
    # Sub-agent events
    SUBAGENT_STARTED = "subagent.started"
    SUBAGENT_COMPLETED = "subagent.completed"
    SUBAGENT_FAILED = "subagent.failed"
    SUBAGENT_TURN_STARTED = "subagent.turn.started"
    SUBAGENT_TURN_COMPLETED = "subagent.turn.completed"
    SUBAGENT_ITEM_STARTED = "subagent.item.started"
    SUBAGENT_ITEM_UPDATED = "subagent.item.updated"
    SUBAGENT_ITEM_COMPLETED = "subagent.item.completed"
    # Async task events
    ASYNC_TASK_SCHEDULED = "async_task.scheduled"
    ASYNC_TASK_SNAPSHOT = "async_task.snapshot"
    ASYNC_TASK_PROGRESS = "async_task.progress"
    ASYNC_TASK_COMPLETED = "async_task.completed"
    ASYNC_TASK_FAILED = "async_task.failed"
    ASYNC_TASK_DELETED = "async_task.deleted"
    # Command progress events
    COMMAND_PROGRESS = "command.progress"


class ItemType(str, Enum):
    """Item types in events."""
    AGENT_MESSAGE = "agent_message"
    TOOL_CALL = "tool_call"
    SUB_AGENT = "sub_agent"


class AgentType(str, Enum):
    """Agent types."""
    CORE = "core"
    MANAGED = "managed"


@dataclass
class Item:
    """Represents an item in an event (agent message, tool call, or sub-agent)."""
    id: str
    type: ItemType
    status: str
    text: Optional[str] = None
    name: Optional[str] = None
    display_name: Optional[str] = None
    arguments: Optional[Dict[str, Any]] = None
    output: Optional[str] = None
    error: Optional[str] = None
    duration_ms: Optional[int] = None
    exit_code: Optional[int] = None
    agent_name: Optional[str] = None
    prompt: Optional[str] = None
    result: Optional[str] = None
    depth: Optional[int] = None


@dataclass
class ExecEvent:
    """Represents a JSON event from op exec."""
    type: EventType
    session_id: str
    raw: Dict[str, Any]

    # Session events
    conversation_title: Optional[str] = None
    agent_name: Optional[str] = None
    agent_type: Optional[AgentType] = None
    is_resumed: Optional[bool] = None
    has_history: Optional[bool] = None
    message_count: Optional[int] = None
    final_response: Optional[str] = None
    total_turns: Optional[int] = None
    total_tool_calls: Optional[int] = None
    duration_ms: Optional[int] = None
    error: Optional[str] = None
    error_type: Optional[str] = None

    # Turn events
    turn_number: Optional[int] = None
    round_count: Optional[int] = None
    has_tool_calls: Optional[bool] = None

    # Item events
    item: Optional[Item] = None

    # Sub-agent events
    subagent_id: Optional[str] = None
    parent_item_id: Optional[str] = None
    task_definition: Optional[str] = None
    prompt: Optional[str] = None
    result: Optional[str] = None
    transcript: Optional[List[Dict[str, Any]]] = None
    metadata: Optional[Dict[str, Any]] = None

    # Async task events
    task_id: Optional[str] = None
    call_id: Optional[str] = None
    tool_name: Optional[str] = None
    command_name: Optional[str] = None
    command_args: Optional[str] = None
    status: Optional[str] = None
    daemon: Optional[str] = None
    working_dir: Optional[str] = None
    context: Optional[Dict[str, Any]] = None
    created_at: Optional[str] = None
    updated_at: Optional[str] = None
    completed_at: Optional[str] = None
    progress_count: Optional[int] = None
    progress_summary: Optional[List[Dict[str, Any]]] = None

    # Command progress
    item_id: Optional[str] = None
    command_id: Optional[str] = None
    progress: Optional[Dict[str, Any]] = None


@dataclass
class ExecResult:
    """Result of an exec invocation."""
    success: bool
    response: str
    resume_id: Optional[str]
    events: List[ExecEvent]
    error: Optional[str] = None


class ExecClient:
    """Typed wrapper around 'op exec --json' command.

    This client provides a clean Python API for invoking op exec with full
    event streaming support.

    Example:
        client = ExecClient()

        def on_event(event: ExecEvent):
            if event.type == EventType.ITEM_UPDATED:
                print(f"Agent: {event.item.text}")

        result = client.exec(
            "What is the weather?",
            agent="weather-agent",
            event_callback=on_event
        )

        print(f"Resume ID: {result.resume_id}")
        print(f"Response: {result.response}")
    """

    def __init__(self, op_binary_path: Optional[str] = None):
        """Initialize exec client.

        Args:
            op_binary_path: Path to op binary. If None, auto-detected from PATH.
        """
        self.op_path = op_binary_path or _find_op_binary()

    def exec(
        self,
        message: str,
        agent: Optional[str] = None,
        resume: Optional[str] = None,
        no_save: bool = False,
        event_callback: Optional[Callable[[ExecEvent], None]] = None
    ) -> ExecResult:
        """Execute a message via another agent.

        This is equivalent to: op exec <message> --json [--agent <agent>] [--resume <id>] [--no-save]

        Args:
            message: Message to send to agent
            agent: Target agent name (optional, uses default if not specified)
            resume: Resume existing conversation ID
            no_save: Don't save conversation to database
            event_callback: Called for each streaming event

        Returns:
            ExecResult with response, resume_id, and all events

        Raises:
            FileNotFoundError: If op binary not found
            RuntimeError: If op exec command fails

        Example:
            result = client.exec(
                "Hello world",
                agent="my-agent",
                event_callback=lambda e: print(f"Event: {e.type}")
            )
            print(f"Response: {result.response}")
            print(f"Resume with: {result.resume_id}")
        """
        # Build command
        cmd = [self.op_path, "exec", message, "--json"]

        if agent:
            cmd.extend(["--agent", agent])

        if resume:
            cmd.extend(["--resume", resume])

        if no_save:
            cmd.append("--no-save")

        # Run command
        process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1
        )

        events: List[ExecEvent] = []
        resume_id: Optional[str] = None
        final_response: Optional[str] = None

        # Read output line by line
        for line in iter(process.stdout.readline, ''):
            if not line:
                break

            line = line.strip()
            if not line:
                continue

            # Try to parse as JSON event
            try:
                raw_event = json.loads(line)
                event = self._parse_event(raw_event)
                events.append(event)

                # Extract resume_id from session_started
                if event.type == EventType.SESSION_STARTED:
                    resume_id = event.session_id

                # Extract final_response from session_completed
                if event.type == EventType.SESSION_COMPLETED:
                    final_response = event.final_response

                # Call event callback
                if event_callback:
                    event_callback(event)

            except json.JSONDecodeError:
                # Not JSON - pass through to stderr
                print(line, file=sys.stderr, flush=True)

        # Wait for process to complete
        return_code = process.wait()

        if return_code != 0:
            error_msg = f"op exec failed with exit code {return_code}"
            return ExecResult(
                success=False,
                response="",
                resume_id=resume_id,
                events=events,
                error=error_msg
            )

        return ExecResult(
            success=True,
            response=final_response or "",
            resume_id=resume_id,
            events=events
        )

    def _parse_event(self, raw: Dict[str, Any]) -> ExecEvent:
        """Parse raw JSON event into typed ExecEvent."""
        event_type = EventType(raw.get("type", ""))
        session_id = raw.get("session_id", "")

        # Parse item if present
        item = None
        if "item" in raw:
            item_data = raw["item"]
            item = Item(
                id=item_data.get("id", ""),
                type=ItemType(item_data.get("type", "")),
                status=item_data.get("status", ""),
                text=item_data.get("text"),
                name=item_data.get("name"),
                display_name=item_data.get("display_name"),
                arguments=item_data.get("arguments"),
                output=item_data.get("output"),
                error=item_data.get("error"),
                duration_ms=item_data.get("duration_ms"),
                exit_code=item_data.get("exit_code"),
                agent_name=item_data.get("agent_name"),
                prompt=item_data.get("prompt"),
                result=item_data.get("result"),
                depth=item_data.get("depth")
            )

        # Parse agent_type if present
        agent_type = None
        if "agent_type" in raw:
            agent_type = AgentType(raw["agent_type"])

        return ExecEvent(
            type=event_type,
            session_id=session_id,
            raw=raw,
            # Session events
            conversation_title=raw.get("conversation_title"),
            agent_name=raw.get("agent_name"),
            agent_type=agent_type,
            is_resumed=raw.get("is_resumed"),
            has_history=raw.get("has_history"),
            message_count=raw.get("message_count"),
            final_response=raw.get("final_response"),
            total_turns=raw.get("total_turns"),
            total_tool_calls=raw.get("total_tool_calls"),
            duration_ms=raw.get("duration_ms"),
            error=raw.get("error"),
            error_type=raw.get("error_type"),
            # Turn events
            turn_number=raw.get("turn_number"),
            round_count=raw.get("round_count"),
            has_tool_calls=raw.get("has_tool_calls"),
            # Item events
            item=item,
            # Sub-agent events
            subagent_id=raw.get("subagent_id"),
            parent_item_id=raw.get("parent_item_id"),
            task_definition=raw.get("task_definition"),
            prompt=raw.get("prompt"),
            result=raw.get("result"),
            transcript=raw.get("transcript"),
            metadata=raw.get("metadata"),
            # Async task events
            task_id=raw.get("task_id"),
            call_id=raw.get("call_id"),
            tool_name=raw.get("tool_name"),
            command_name=raw.get("command_name"),
            command_args=raw.get("command_args"),
            status=raw.get("status"),
            daemon=raw.get("daemon"),
            working_dir=raw.get("working_dir"),
            context=raw.get("context"),
            created_at=raw.get("created_at"),
            updated_at=raw.get("updated_at"),
            completed_at=raw.get("completed_at"),
            progress_count=raw.get("progress_count"),
            progress_summary=raw.get("progress_summary"),
            # Command progress
            item_id=raw.get("item_id"),
            command_id=raw.get("command_id"),
            progress=raw.get("progress")
        )


@dataclass
class CommandResult:
    """Result of a command invocation."""
    success: bool
    result: Any
    error: Optional[str] = None


def command(
    agent: str,
    command_name: str,
    args: Optional[Dict[str, Any]] = None,
    progress_callback: Optional[Callable[[str], None]] = None,
    op_binary_path: Optional[str] = None
) -> CommandResult:
    """Invoke a command on another agent.

    Thin wrapper around: op agent command <agent> <command> --args=<json>

    Args:
        agent: Target agent name
        command_name: Command name to invoke
        args: Command arguments dictionary
        progress_callback: Called with progress text updates
        op_binary_path: Path to op binary (auto-detected if None)

    Returns:
        CommandResult with success status and result

    Raises:
        FileNotFoundError: If op binary not found

    Example:
        result = command(
            "processor-agent",
            "process_file",
            args={"path": "/tmp/data.csv"},
            progress_callback=lambda t: print(f"Progress: {t}")
        )
        if result.success:
            print(f"Result: {result.result}")
    """
    # Find op binary
    if not op_binary_path:
        op_binary_path = _find_op_binary()

    # Build command
    cmd = [op_binary_path, "agent", "command", agent, command_name]

    if args:
        cmd.extend(["--args", json.dumps(args)])

    try:
        # Run command and capture output
        process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True
        )

        # Read stderr for progress updates
        stderr_lines = []
        if progress_callback:
            for line in iter(process.stderr.readline, ''):
                if not line:
                    break
                line = line.strip()
                if line:
                    stderr_lines.append(line)
                    if not line.startswith("{"):  # Not JSON
                        progress_callback(line)

        # Get stdout and wait for completion
        stdout, stderr_rest = process.communicate()
        if stderr_rest:
            stderr_lines.extend(stderr_rest.strip().split('\n'))

        return_code = process.returncode

        if return_code != 0:
            error_output = '\n'.join(stderr_lines) if stderr_lines else stdout
            return CommandResult(
                success=False,
                result=None,
                error=error_output
            )

        # Parse JSON response
        try:
            result = json.loads(stdout)
            return CommandResult(
                success=True,
                result=result,
                error=None
            )
        except json.JSONDecodeError:
            # Return raw output if not JSON
            return CommandResult(
                success=True,
                result=stdout.strip(),
                error=None
            )

    except Exception as e:
        return CommandResult(
            success=False,
            result=None,
            error=str(e)
        )
