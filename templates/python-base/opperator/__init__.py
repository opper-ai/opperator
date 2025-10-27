"""
Opperator Python Base Library
A framework for building managed processes that integrate with Opperator process manager.
"""

from .agent import OpperatorAgent
from .protocol import (
    Message, MessageType, LogLevel,
    ReadyMessage, LogMessage,
    CommandMessage, ResponseMessage, ErrorMessage,
    CommandDefinition, CommandArgument, CommandExposure, SlashCommandScope
)
from .lifecycle import LifecycleManager
from .secrets import get_secret, SecretError

__version__ = "1.0.0"

__all__ = [
    'OpperatorAgent',
    'Message',
    'MessageType',
    'LogLevel',
    'ReadyMessage',
    'LogMessage',
    'CommandMessage',
    'ResponseMessage',
    'ErrorMessage',
    'CommandDefinition',
    'CommandArgument',
    'CommandExposure',
    'SlashCommandScope',
    'LifecycleManager',
    'get_secret',
    'SecretError',
]
