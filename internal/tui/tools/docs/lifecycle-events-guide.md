# Lifecycle Events

Opperator sends lifecycle events to notify
agents about conversation changes, agent
activation, and other context updates.

## Event Handler Methods

Override these methods to react to events:

**Conversation events:**
- `on_new_conversation(conversation_id, is_clear)`
- `on_conversation_switched(conversation_id, previous_id, message_count)`
- `on_conversation_deleted(conversation_id)`

**Agent events:**
- `on_agent_activated(previous_agent, conversation_id)`
- `on_agent_deactivated(next_agent)`

**Other events:**
- `on_working_directory_changed(old_path, new_path)`
- `on_config_update(config)`
- `on_status()`

## Conversation Events

**New conversation or /clear:**
```python
def on_new_conversation(self, conversation_id: str, is_clear: bool):
    """Called when new conversation created or /clear executed

    Args:
        conversation_id: Unique conversation identifier
        is_clear: True if /clear command, False if new conversation
    """
    source = "cleared" if is_clear else "created"
    self.log(LogLevel.INFO, f"Conversation {source}", id=conversation_id)

    # Reset conversation-specific state
    self.conversation_data = {}
    self.message_count = 0
```

**Use cases:**
- Clear conversation-specific caches
- Reset session state
- Initialize new conversation context
- Clear accumulated data

**Example - Chat agent:**
```python
def on_new_conversation(self, conversation_id: str, is_clear: bool):
    # Clear chat history
    self.messages = []

    # Reset context
    self.context_window = []

    # Update system prompt
    self.set_system_prompt(f"""
Chat agent ready.

Conversation: {conversation_id}
Messages: 0
    """)
```

**Switch to different conversation:**
```python
def on_conversation_switched(
    self,
    conversation_id: str,
    previous_id: str,
    message_count: int
):
    """Called when user switches conversations

    Args:
        conversation_id: Conversation being switched to
        previous_id: Previous conversation ID
        message_count: Number of messages in new conversation
    """
    self.log(
        LogLevel.INFO,
        "Switched conversations",
        from_id=previous_id,
        to_id=conversation_id,
        messages=message_count,
    )

    # Load conversation state
    self.load_conversation_state(conversation_id)
```

**Use cases:**
- Load conversation-specific state
- Restore session data
- Update UI to reflect context
- Resume conversation history

**Example - State management:**
```python
def on_conversation_switched(
    self,
    conversation_id: str,
    previous_id: str,
    message_count: int
):
    # Save previous conversation state
    if previous_id:
        self.save_state(previous_id)

    # Load new conversation state
    state = self.load_state(conversation_id)
    if state:
        self.restore_from_state(state)
    else:
        # New conversation we haven't seen
        self.initialize_conversation_state()

    # Update sidebar
    self.update_section(
        "info",
        f"Conversation: {conversation_id[:8]}...\n"
        f"Messages: {message_count}"
    )
```

**Conversation deleted:**
```python
def on_conversation_deleted(self, conversation_id: str):
    """Called before conversation is deleted

    Args:
        conversation_id: ID of conversation being deleted
    """
    self.log(LogLevel.INFO, "Conversation deleted", id=conversation_id)

    # Clean up conversation-specific resources
    cache_file = f"/tmp/conv_{conversation_id}.cache"
    if os.path.exists(cache_file):
        os.remove(cache_file)
```

**Use cases:**
- Delete cached data
- Clean up temporary files
- Remove conversation from databases
- Free memory

**Example - Cleanup:**
```python
def on_conversation_deleted(self, conversation_id: str):
    # Remove from state store
    if conversation_id in self.conversation_states:
        del self.conversation_states[conversation_id]

    # Delete temp files
    temp_dir = f"/tmp/claude/agent_{conversation_id}"
    if os.path.exists(temp_dir):
        shutil.rmtree(temp_dir)

    # Remove from database
    self.db.delete_conversation(conversation_id)
```

## Agent Activation Events

**Agent becomes active:**
```python
def on_agent_activated(
    self,
    previous_agent: Optional[str],
    conversation_id: str
):
    """Called when this agent becomes active

    Args:
        previous_agent: Previously active agent (or None)
        conversation_id: Current conversation ID
    """
    self.log(
        LogLevel.INFO,
        "Agent activated",
        previous=previous_agent,
        conversation=conversation_id,
    )

    # Update UI to show active status
    self.update_section(
        "status",
        '<c fg="green">● Active</c>'
    )
```

**Use cases:**
- Update UI to show active state
- Resume background tasks
- Initialize agent-specific context
- Update system prompt with current state

**Example - Resume monitoring:**
```python
def on_agent_activated(
    self,
    previous_agent: Optional[str],
    conversation_id: str
):
    # Update status
    self.active = True

    # Update sidebar
    self.update_section(
        "status",
        f"""
<c fg="green">● Active</c>
<b>Previous:</b> {previous_agent or 'None'}
<b>Conversation:</b> {conversation_id[:8]}...
        """.strip()
    )

    # Update system prompt with current state
    self.set_system_prompt(f"""
Monitoring agent (ACTIVE).

Watched files: {len(self.watched_files)}
Active alerts: {len(self.alerts)}
    """)

    # Resume monitoring if paused
    if self.paused:
        self.resume_monitoring()
```

**Agent becomes inactive:**
```python
def on_agent_deactivated(self, next_agent: Optional[str]):
    """Called when user switches away from this agent

    Args:
        next_agent: Agent being switched to (or None)
    """
    self.log(LogLevel.INFO, "Agent deactivated", next=next_agent)

    # Update UI to show inactive status
    self.update_section(
        "status",
        '<c fg="yellow">○ Inactive</c>'
    )
```

**Use cases:**
- Update UI to show inactive state
- Pause background tasks (optional)
- Save state
- Reduce resource usage

**Example - Pause tasks:**
```python
def on_agent_deactivated(self, next_agent: Optional[str]):
    # Update status
    self.active = False

    # Update sidebar
    self.update_section(
        "status",
        f"""
<c fg="yellow">○ Inactive</c>
<b>Switched to:</b> {next_agent or 'None'}
        """.strip()
    )

    # Update system prompt (minimal when inactive)
    self.set_system_prompt("Monitoring agent (inactive).")

    # Optionally pause expensive background tasks
    # (keep running if you want to keep collecting data)
    if self.pause_when_inactive:
        self.pause_monitoring()
```

## Working Directory Events

**Directory changed:**
```python
def on_working_directory_changed(self, old_path: str, new_path: str):
    """Called when working directory changes

    Args:
        old_path: Previous working directory
        new_path: New working directory
    """
    self.log(
        LogLevel.INFO,
        "Working directory changed",
        old=old_path,
        new=new_path,
    )

    # Reload project-specific config
    self.load_project_config(new_path)
```

**Use cases:**
- Update file watchers
- Reload project-specific configuration
- Reset path-dependent state
- Scan new directory structure

**Example - Project context:**
```python
def on_working_directory_changed(self, old_path: str, new_path: str):
    # Check for project config
    config_file = os.path.join(new_path, ".agent-config.json")
    if os.path.exists(config_file):
        with open(config_file) as f:
            project_config = json.load(f)
            self.apply_project_config(project_config)

    # Scan for relevant files
    python_files = list(Path(new_path).glob("**/*.py"))

    # Update system prompt
    self.set_system_prompt(f"""
Code analysis agent.

Working directory: {os.path.basename(new_path)}
Python files: {len(python_files)}
    """)

    # Update sidebar
    self.update_section(
        "project",
        f"""
<b>Directory:</b> {os.path.basename(new_path)}
<b>Python files:</b> {len(python_files)}
        """.strip()
    )
```

## Configuration Events

**Config reloaded:**
```python
def on_config_update(self, config: Dict[str, Any]):
    """Called when configuration is reloaded

    Args:
        config: New configuration dictionary
    """
    self.log(LogLevel.INFO, "Configuration updated", config=config)

    # Apply new settings
    new_level = config.get("log_level", "INFO")
    if new_level != self.log_level:
        self.log_level = new_level
        self.log(LogLevel.INFO, "Log level changed", level=new_level)
```

**Use cases:**
- Update runtime parameters without restart
- Reload connection settings
- Adjust logging levels
- Apply new feature flags

**Example - Hot reload:**
```python
def on_config_update(self, config: Dict[str, Any]):
    old_config = self.config.copy()

    # Update polling interval
    new_interval = config.get("poll_interval", 60)
    if new_interval != self.poll_interval:
        self.poll_interval = new_interval
        self.log(
            LogLevel.INFO,
            "Poll interval updated",
            old=self.poll_interval,
            new=new_interval
        )

    # Update API endpoint
    new_endpoint = config.get("api_endpoint")
    if new_endpoint and new_endpoint != self.api_endpoint:
        self.api_endpoint = new_endpoint
        self.reconnect_to_api()
        self.log(LogLevel.INFO, "API endpoint updated")

    # Update feature flags
    self.feature_flags = config.get("features", {})
```

## Status Signal

**Status check (SIGUSR1):**
```python
def on_status(self):
    """Called when status signal received

    Triggered by: kill -USR1 <pid>
    """
    uptime = time.time() - self.start_time

    self.log(
        LogLevel.INFO,
        "Status check",
        uptime_seconds=int(uptime),
        processed_count=self.processed_count,
        pending_tasks=len(self.tasks),
        active=self.active,
    )
```

**Use cases:**
- Log current state for debugging
- Report health metrics
- Dump diagnostic information
- Check resource usage

**Example - Health check:**
```python
def on_status(self):
    # Gather metrics
    metrics = {
        "uptime": time.time() - self.start_time,
        "requests_processed": self.request_count,
        "errors": self.error_count,
        "active_connections": len(self.connections),
        "queue_size": len(self.queue),
        "memory_mb": self.get_memory_usage_mb(),
    }

    # Log comprehensive status
    self.log(
        LogLevel.INFO,
        "Health check",
        **metrics
    )

    # Check for issues
    if metrics["queue_size"] > 1000:
        self.log(LogLevel.WARNING, "Queue size high", size=metrics["queue_size"])

    if metrics["error_count"] > 100:
        self.log(LogLevel.WARNING, "High error count", count=metrics["error_count"])
```

## Complete Example

Agent using all lifecycle events:

```python
from opperator import OpperatorAgent, LogLevel
import time
import os
from typing import Dict, Any, Optional

class LifecycleAgent(OpperatorAgent):
    def __init__(self):
        super().__init__(name="lifecycle_agent")
        self.start_time = None
        self.active = False
        self.conversation_states = {}
        self.current_conversation = None

    def initialize(self):
        self.set_description("Demonstrates lifecycle event handling")

        self.register_section(
            "status",
            "Agent Status",
            '<c fg="yellow">○ Initializing</c>',
        )

        self.register_section(
            "context",
            "Context",
            "No conversation",
            collapsed=True,
        )

    def start(self):
        self.start_time = time.time()
        self.update_section("status", '<c fg="green">● Running</c>')

    # Conversation events
    def on_new_conversation(self, conversation_id: str, is_clear: bool):
        source = "Cleared" if is_clear else "New"
        self.current_conversation = conversation_id
        self.conversation_states[conversation_id] = {
            "created": time.time(),
            "message_count": 0,
        }

        self.update_section(
            "context",
            f"""
<b>Event:</b> {source} conversation
<b>ID:</b> {conversation_id[:8]}...
            """.strip()
        )

    def on_conversation_switched(
        self,
        conversation_id: str,
        previous_id: str,
        message_count: int
    ):
        self.current_conversation = conversation_id

        self.update_section(
            "context",
            f"""
<b>Event:</b> Switched conversation
<b>From:</b> {previous_id[:8] if previous_id else 'None'}
<b>To:</b> {conversation_id[:8]}...
<b>Messages:</b> {message_count}
            """.strip()
        )

    def on_conversation_deleted(self, conversation_id: str):
        if conversation_id in self.conversation_states:
            del self.conversation_states[conversation_id]

        self.log(LogLevel.INFO, "Conversation deleted", id=conversation_id)

    # Agent activation events
    def on_agent_activated(
        self,
        previous_agent: Optional[str],
        conversation_id: str
    ):
        self.active = True

        self.update_section(
            "status",
            f"""
<c fg="green">● Active</c>
<b>Previous:</b> {previous_agent or 'None'}
            """.strip()
        )

        self.set_system_prompt(f"""
Lifecycle agent is ACTIVE.

Conversation: {conversation_id}
Active conversations: {len(self.conversation_states)}
        """)

    def on_agent_deactivated(self, next_agent: Optional[str]):
        self.active = False

        self.update_section(
            "status",
            f"""
<c fg="yellow">○ Inactive</c>
<b>Next:</b> {next_agent or 'None'}
            """.strip()
        )

        self.set_system_prompt("Lifecycle agent (inactive).")

    # Other events
    def on_working_directory_changed(self, old_path: str, new_path: str):
        self.log(
            LogLevel.INFO,
            "Directory changed",
            old=os.path.basename(old_path),
            new=os.path.basename(new_path),
        )

        self.update_section(
            "context",
            f"""
<b>Event:</b> Directory changed
<b>Path:</b> {os.path.basename(new_path)}
            """.strip()
        )

    def on_config_update(self, config: Dict[str, Any]):
        self.log(LogLevel.INFO, "Config updated", config=config)

    def on_status(self):
        uptime = time.time() - self.start_time if self.start_time else 0

        self.log(
            LogLevel.INFO,
            "Status check",
            uptime_seconds=int(uptime),
            active=self.active,
            conversations=len(self.conversation_states),
            current_conversation=self.current_conversation,
        )
```

## Event Flow Examples

**User creates new conversation:**
1. `on_new_conversation(conv_id, is_clear=False)`
2. Agent resets conversation state

**User types /clear:**
1. `on_new_conversation(conv_id, is_clear=True)`
2. Agent clears conversation data

**User switches conversations:**
1. `on_conversation_switched(new_id, old_id, msg_count)`
2. Agent loads new conversation state

**User switches agents:**
1. Current agent: `on_agent_deactivated(next_agent="other_agent")`
2. Other agent: `on_agent_activated(previous_agent="current_agent", conv_id)`

**User changes directory:**
1. `on_working_directory_changed(old, new)`
2. Agent updates project context

## Best Practices

**Don't block event handlers:**
```python
# Bad: Long operation in event handler
def on_agent_activated(self, previous_agent, conversation_id):
    self.process_all_files()  # Could take minutes!

# Good: Quick update only
def on_agent_activated(self, previous_agent, conversation_id):
    self.active = True
    self.update_section("status", "Active")
```

**Handle missing data gracefully:**
```python
def on_conversation_switched(
    self,
    conversation_id: str,
    previous_id: str,
    message_count: int
):
    # previous_id might be empty string
    if previous_id:
        self.save_state(previous_id)

    # Conversation might be new
    state = self.load_state(conversation_id)
    if state:
        self.restore_state(state)
    else:
        self.initialize_state()
```

**Log important events:**
```python
def on_new_conversation(self, conversation_id: str, is_clear: bool):
    # Always log lifecycle events for debugging
    self.log(
        LogLevel.INFO,
        "New conversation",
        id=conversation_id,
        is_clear=is_clear,
    )
```

## Summary

**Conversation events:**
- `on_new_conversation()` - New or cleared
- `on_conversation_switched()` - Switch between conversations
- `on_conversation_deleted()` - Before deletion

**Agent events:**
- `on_agent_activated()` - Agent becomes active
- `on_agent_deactivated()` - Agent becomes inactive

**Other events:**
- `on_working_directory_changed()` - Directory changes
- `on_config_update()` - Config reloaded
- `on_status()` - Status signal (SIGUSR1)

Use lifecycle events to:
- Manage conversation-specific state
- Update UI based on context
- React to agent activation changes
- Handle configuration updates
- Provide health check information

Events are called automatically by the
OpperatorAgent base class.
