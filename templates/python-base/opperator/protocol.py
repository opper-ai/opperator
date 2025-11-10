"""Protocol definitions for communication with Opperator process manager."""

import json
import re
import sys
from dataclasses import dataclass, asdict, replace
from datetime import datetime, timezone
from enum import Enum
from typing import Any, Dict, Iterable, List, Optional, Sequence, Set, Union


class MessageType(str, Enum):
    """Message types for process communication"""
    # Lifecycle messages
    READY = "ready"

    # Logging messages
    LOG = "log"

    # Lifecycle event messages (manager → process)
    LIFECYCLE_EVENT = "lifecycle_event"

    # Command messages
    COMMAND = "command"
    RESPONSE = "response"
    COMMAND_REGISTRY = "command_registry"
    SYSTEM_PROMPT = "system_prompt"
    AGENT_DESCRIPTION = "agent_description"
    COMMAND_PROGRESS = "command_progress"

    # Sidebar messages
    SIDEBAR_SECTION = "sidebar_section"
    SIDEBAR_SECTION_REMOVAL = "sidebar_section_removal"

    # Error messages
    ERROR = "error"


class LogLevel(str, Enum):
    """Log severity levels"""
    DEBUG = "debug"
    INFO = "info"
    WARNING = "warning"
    ERROR = "error"
    FATAL = "fatal"


@dataclass
class Message:
    """Base message structure"""
    type: MessageType
    timestamp: str
    data: Optional[Dict[str, Any]] = None
    
    def to_json(self) -> str:
        """Convert message to JSON string"""
        return json.dumps({
            'type': self.type.value if isinstance(self.type, Enum) else self.type,
            'timestamp': self.timestamp,
            'data': self.data
        })
    
    @classmethod
    def from_json(cls, json_str: str) -> 'Message':
        """Create message from JSON string"""
        data = json.loads(json_str)
        return cls(
            type=MessageType(data['type']),
            timestamp=data.get('timestamp', datetime.now(timezone.utc).isoformat()),
            data=data.get('data')
        )


@dataclass
class ReadyMessage:
    """Sent when process is ready"""
    pid: int
    version: Optional[str] = None
    
    def to_dict(self) -> Dict[str, Any]:
        return {k: v for k, v in asdict(self).items() if v is not None}


@dataclass
class LogMessage:
    """Structured log message"""
    level: LogLevel
    message: str
    fields: Optional[Dict[str, Any]] = None
    
    def to_dict(self) -> Dict[str, Any]:
        data = {
            'level': self.level.value if isinstance(self.level, Enum) else self.level,
            'message': self.message
        }
        if self.fields:
            data['fields'] = self.fields
        return data


@dataclass
class LifecycleEventMessage:
    """Lifecycle event from manager to process"""
    event_type: str
    data: Optional[Dict[str, Any]] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'LifecycleEventMessage':
        return cls(
            event_type=data.get('event_type', ''),
            data=data.get('data')
        )


@dataclass
class CommandMessage:
    """Command from manager to process"""
    command: str
    args: Optional[Dict[str, Any]] = None
    id: Optional[str] = None
    working_dir: Optional[str] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> 'CommandMessage':
        return cls(
            command=data.get('command', ''),
            args=data.get('args'),
            id=data.get('id'),
            working_dir=data.get('working_dir')
        )


@dataclass
class ResponseMessage:
    """Response to a command"""
    command_id: Optional[str] = None
    success: bool = True
    result: Optional[Any] = None
    error: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        data = {
            'success': self.success,
        }
        if self.command_id:
            data['command_id'] = self.command_id
        if self.result is not None:
            data['result'] = self.result
        if self.error:
            data['error'] = self.error
        return data


@dataclass
class CommandProgress:
    """Progress update for a long-running command."""

    command_id: Optional[str] = None
    text: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None
    status: Optional[str] = None
    progress: Optional[float] = None

    def to_dict(self) -> Dict[str, Any]:
        data: Dict[str, Any] = {}
        if self.command_id:
            data['command_id'] = self.command_id
        if self.text:
            data['text'] = self.text
        if self.metadata:
            data['metadata'] = self.metadata
        if self.status:
            data['status'] = self.status
        if self.progress is not None:
            data['progress'] = float(self.progress)
        return data


class CommandExposure(str, Enum):
    """Defines how a command should be exposed to the manager."""

    AGENT_TOOL = "agent_tool"
    SLASH_COMMAND = "slash_command"


class SlashCommandScope(str, Enum):
    """Controls where a slash command should be displayed within the TUI."""

    LOCAL = "local"
    GLOBAL = "global"


@dataclass
class CommandArgument:
    """Typed argument definition for a command."""

    name: str
    type: str = "string"
    description: Optional[str] = None
    required: bool = False
    default: Any = None
    enum: Optional[Sequence[Any]] = None
    items: Optional[Dict[str, Any]] = None  # Schema for array items
    properties: Optional[Dict[str, Any]] = None  # Schema for object properties

    def normalized(self) -> 'CommandArgument':
        name = str(self.name or "").strip()
        if not name:
            raise ValueError("argument name cannot be empty")

        arg_type = (self.type or "string").strip().lower()
        valid_types = {"string", "integer", "number", "boolean", "array", "object"}
        if arg_type not in valid_types:
            arg_type = "string"

        description = (self.description or "").strip() or None
        required = bool(self.required)

        enum: Optional[List[Any]] = None
        if self.enum:
            enum_values: List[Any] = []
            for value in self.enum:
                if value is None:
                    continue
                enum_values.append(value)
            if enum_values:
                enum = enum_values

        default = self.default

        return replace(
            self,
            name=name,
            type=arg_type,
            description=description,
            required=required,
            default=default,
            enum=enum,
            items=self.items,
            properties=self.properties,
        )

    def to_dict(self) -> Dict[str, Any]:
        normalized = self.normalized()
        data: Dict[str, Any] = {
            "name": normalized.name,
            "type": normalized.type,
        }
        if normalized.description:
            data["description"] = normalized.description
        if normalized.required:
            data["required"] = True
        if normalized.default is not None:
            data["default"] = normalized.default
        if normalized.enum:
            data["enum"] = list(normalized.enum)
        if normalized.items:
            data["items"] = normalized.items
        if normalized.properties:
            data["properties"] = normalized.properties
        return data


@dataclass
class CommandDefinition:
    """Describes a command that the managed process exposes."""

    name: str
    title: Optional[str] = None
    description: Optional[str] = None
    expose_as: Optional[Sequence[CommandExposure]] = None
    slash_command: Optional[str] = None
    slash_scope: Optional[Union[str, SlashCommandScope]] = None
    argument_hint: Optional[str] = None
    argument_required: bool = False
    arguments: Optional[Sequence[CommandArgument]] = None
    async_enabled: bool = False
    progress_label: Optional[str] = None
    hidden: bool = False

    def normalized(self) -> 'CommandDefinition':
        name = str(self.name).strip()
        if not name:
            name = str(self.name)

        title = (self.title or '').strip()
        if not title:
            title = self._derive_title(name) or name

        exposures: List[CommandExposure] = []
        if self.expose_as:
            seen = set()
            for value in self._iter_exposures(self.expose_as):
                if isinstance(value, CommandExposure):
                    exposure = value
                else:
                    try:
                        exposure = CommandExposure(str(value).strip().lower())
                    except ValueError:
                        continue
                if exposure not in seen:
                    exposures.append(exposure)
                    seen.add(exposure)
        if not exposures:
            exposures = [CommandExposure.AGENT_TOOL]

        slash_name: Optional[str] = None
        if self.slash_command:
            slash_name = self._normalize_slash(self.slash_command)
            if CommandExposure.SLASH_COMMAND not in exposures:
                exposures.append(CommandExposure.SLASH_COMMAND)
        elif CommandExposure.SLASH_COMMAND in exposures:
            slash_name = self._normalize_slash(name)

        scope = self._normalize_scope(self.slash_scope)
        hint = (self.argument_hint or '').strip()
        required = bool(self.argument_required)
        arguments = self._normalize_arguments(self.arguments)
        if arguments:
            required = required or any(arg.required for arg in arguments)

        async_enabled = bool(self.async_enabled)
        progress_label = (self.progress_label or '').strip() or None
        hidden = bool(self.hidden)

        return replace(
            self,
            name=name,
            title=title,
            expose_as=exposures,
            slash_command=slash_name,
            slash_scope=scope,
            argument_hint=hint,
            argument_required=required,
            arguments=arguments,
            async_enabled=async_enabled,
            progress_label=progress_label,
            hidden=hidden,
        )

    @staticmethod
    def _normalize_slash(value: str) -> str:
        candidate = str(value).strip().lstrip('/')
        if not candidate:
            candidate = "command"
        cleaned = []
        for ch in candidate:
            if ch.isalnum() or ch in {'_', '-', ':'}:
                cleaned.append(ch.lower())
            elif ch.isspace() and (not cleaned or cleaned[-1] != '_'):
                cleaned.append('_')
            elif cleaned and cleaned[-1] != '_':
                cleaned.append('_')
        slug = ''.join(cleaned).strip('_')
        if not slug:
            slug = candidate.replace(' ', '_').lower()
        return f"/{slug}"

    @staticmethod
    def _normalize_scope(value: Optional[Union[str, SlashCommandScope]]) -> str:
        if isinstance(value, SlashCommandScope):
            trimmed = value.value
        else:
            trimmed = (value or '').strip().lower()
        if trimmed == 'global':
            return 'global'
        return 'local'

    @staticmethod
    def _iter_exposures(values: Sequence[Any]) -> Iterable[Any]:
        if isinstance(values, (CommandExposure, str)):
            return (values,)
        try:
            iterator = iter(values)  # type: ignore[arg-type]
        except TypeError:
            return (values,)  # type: ignore[return-value]
        return iterator

    @staticmethod
    def _derive_title(name: str) -> str:
        trimmed = str(name or "").strip()
        if not trimmed:
            return ""

        cleaned = re.sub(r"[_\-\.:]+", " ", trimmed)
        cleaned = re.sub(r"([a-z0-9])([A-Z])", r"\1 \2", cleaned)
        cleaned = re.sub(r"([A-Z]+)([A-Z][a-z])", r"\1 \2", cleaned)
        words = cleaned.split()
        if not words:
            return trimmed

        def transform(word: str) -> str:
            lower = word.lower()
            if lower.isdigit():
                return lower
            if len(word) > 1 and word.isupper():
                return word
            return lower.capitalize()

        return " ".join(transform(word) for word in words)

    def to_dict(self) -> Dict[str, Any]:
        normalized = self.normalized()
        data: Dict[str, Any] = {"name": normalized.name}
        if normalized.title:
            data["title"] = normalized.title
        if normalized.description:
            data["description"] = normalized.description
        if normalized.expose_as:
            data["expose_as"] = [exp.value if isinstance(exp, Enum) else str(exp) for exp in normalized.expose_as]
        if normalized.slash_command:
            data["slash_command"] = normalized.slash_command
        if normalized.slash_scope:
            data["slash_scope"] = normalized.slash_scope
        if normalized.argument_hint:
            data["argument_hint"] = normalized.argument_hint
        if normalized.argument_required:
            data["argument_required"] = True
        if normalized.arguments:
            data["arguments"] = [argument.to_dict() for argument in normalized.arguments]
        if normalized.async_enabled:
            data["async"] = True
        if normalized.progress_label:
            data["progress_label"] = normalized.progress_label
        if normalized.hidden:
            data["hidden"] = True
        return data

    @staticmethod
    def _normalize_arguments(values: Optional[Sequence[CommandArgument]]) -> Optional[List[CommandArgument]]:
        if not values:
            return None
        normalized: List[CommandArgument] = []
        seen: Set[str] = set()
        for value in values:
            try:
                if isinstance(value, CommandArgument):
                    argument = value.normalized()
                elif isinstance(value, dict):
                    argument = CommandArgument(**value).normalized()
                else:
                    continue
            except Exception:
                continue
            if argument.name in seen:
                continue
            normalized.append(argument)
            seen.add(argument.name)
        return normalized or None


@dataclass
class CommandRegistryMessage:
    """List of commands exposed by the process"""
    commands: Optional[List[Dict[str, Any]]] = None

    def to_dict(self) -> Dict[str, Any]:
        return {
            'commands': self.commands or []
        }


@dataclass
class AgentDescriptionMessage:
    """Runtime description metadata for the managed agent."""

    description: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        return {'description': (self.description or '').strip()}


@dataclass
class SidebarSectionMessage:
    """Custom sidebar section for displaying agent-specific information."""

    section_id: str
    title: str
    content: str
    collapsed: bool = False

    def to_dict(self) -> Dict[str, Any]:
        return {
            'section_id': str(self.section_id).strip(),
            'title': str(self.title).strip(),
            'content': str(self.content),
            'collapsed': bool(self.collapsed)
        }


@dataclass
class SidebarSectionRemovalMessage:
    """Message to remove a custom sidebar section."""

    section_id: str

    def to_dict(self) -> Dict[str, Any]:
        return {
            'section_id': str(self.section_id).strip()
        }


@dataclass
class ErrorMessage:
    """Error reporting message"""
    error: str
    code: Optional[int] = None
    details: Optional[str] = None
    
    def to_dict(self) -> Dict[str, Any]:
        data = {'error': self.error}
        if self.code is not None:
            data['code'] = self.code
        if self.details:
            data['details'] = self.details
        return data


class Protocol:
    """Protocol handler for process communication"""
    
    @staticmethod
    def send_message(msg_type: MessageType, data: Optional[Dict[str, Any]] = None):
        """Send a message to the process manager via stdout"""
        message = Message(
            type=msg_type,
            timestamp=datetime.now(timezone.utc).isoformat(),
            data=data
        )
        sys.stdout.write(message.to_json() + '\n')
        sys.stdout.flush()
    
    @staticmethod
    def send_ready(pid: int, version: Optional[str] = None):
        """Send ready message"""
        msg = ReadyMessage(pid=pid, version=version)
        Protocol.send_message(MessageType.READY, msg.to_dict())
    
    
    @staticmethod
    def send_log(level: LogLevel, message: str, fields: Optional[Dict[str, Any]] = None):
        """Send log message"""
        msg = LogMessage(level=level, message=message, fields=fields)
        Protocol.send_message(MessageType.LOG, msg.to_dict())

    @staticmethod
    def send_error(error: str, code: Optional[int] = None, details: Optional[str] = None):
        """Send error message"""
        msg = ErrorMessage(error=error, code=code, details=details)
        Protocol.send_message(MessageType.ERROR, msg.to_dict())

    @staticmethod
    def send_response(success: bool, command_id: Optional[str] = None,
                      result: Optional[Any] = None, error: Optional[str] = None):
        """Send response to a command"""
        msg = ResponseMessage(
            command_id=command_id,
            success=success,
            result=result,
            error=error
        )
        Protocol.send_message(MessageType.RESPONSE, msg.to_dict())

    @staticmethod
    def send_command_progress(command_id: Optional[str], *, text: Optional[str] = None,
                              metadata: Optional[Dict[str, Any]] = None,
                              status: Optional[str] = None,
                              progress: Optional[float] = None) -> None:
        payload = CommandProgress(
            command_id=command_id,
            text=text,
            metadata=metadata,
            status=status,
            progress=progress,
        ).to_dict()
        if payload:
            Protocol.send_message(MessageType.COMMAND_PROGRESS, payload)

    @staticmethod
    def send_command_registry(commands: Iterable[Union[CommandDefinition, Dict[str, Any], str]]):
        """Send the list of available commands to the manager."""
        payload: List[Dict[str, Any]] = []
        for cmd in commands:
            if isinstance(cmd, CommandDefinition):
                payload.append(cmd.to_dict())
            elif isinstance(cmd, dict):
                name = cmd.get('name') or ''
                definition = CommandDefinition(
                    name=str(name),
                    title=cmd.get('title'),
                    description=cmd.get('description'),
                    expose_as=cmd.get('expose_as'),
                    slash_command=cmd.get('slash_command'),
                    slash_scope=cmd.get('slash_scope'),
                    argument_hint=cmd.get('argument_hint'),
                    argument_required=cmd.get('argument_required', False),
                    arguments=cmd.get('arguments'),
                )
                payload.append(definition.to_dict())
            elif isinstance(cmd, str):
                payload.append(CommandDefinition(name=cmd).to_dict())
        msg = CommandRegistryMessage(commands=payload)
        Protocol.send_message(MessageType.COMMAND_REGISTRY, msg.to_dict())

    @staticmethod
    def send_system_prompt(prompt: Optional[str], *, replace: bool = False) -> None:
        """Publish the managed agent's system prompt to the manager."""

        payload = {'prompt': (prompt or '').strip()}
        if replace:
            payload['replace'] = True
        Protocol.send_message(MessageType.SYSTEM_PROMPT, payload)

    @staticmethod
    def send_agent_description(description: Optional[str]) -> None:
        """Publish the managed agent's human-readable description."""

        payload = AgentDescriptionMessage(description=description).to_dict()
        Protocol.send_message(MessageType.AGENT_DESCRIPTION, payload)

    @staticmethod
    def send_sidebar_section(section_id: str, title: str, content: str, collapsed: bool = False) -> None:
        """Send or update a custom sidebar section."""

        msg = SidebarSectionMessage(
            section_id=section_id,
            title=title,
            content=content,
            collapsed=collapsed
        )
        Protocol.send_message(MessageType.SIDEBAR_SECTION, msg.to_dict())

    @staticmethod
    def send_sidebar_section_removal(section_id: str) -> None:
        """Remove a custom sidebar section."""

        msg = SidebarSectionRemovalMessage(section_id=section_id)
        Protocol.send_message(MessageType.SIDEBAR_SECTION_REMOVAL, msg.to_dict())

    @staticmethod
    def read_message() -> Optional[Message]:
        """Read a message from stdin"""
        try:
            line = sys.stdin.readline()
            if line:
                return Message.from_json(line.strip())
        except (json.JSONDecodeError, ValueError):
            pass
        return None
