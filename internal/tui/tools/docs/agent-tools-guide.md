# Agent Tools

Agents register commands that expose as tools
for the LLM to call. Tools let the LLM interact
with your agent's functionality.

## Register a Command

**Simple command - just name and handler:**
```python
def initialize(self):
    # Minimal registration
    self.register_command(
        "ping",
        self._cmd_ping,
    )

def _cmd_ping(self, args):
    """Handler receives args dict, returns result"""
    return {"message": "pong", "timestamp": time.time()}
```

**Full-featured command - all options:**
```python
from opperator import CommandExposure, SlashCommandScope

def initialize(self):
    self.register_command(
        "get_status",
        self._cmd_get_status,
        title="Get Agent Status",
        description="Returns the current status of the agent",
        expose_as=[
            CommandExposure.AGENT_TOOL,      # LLM can call it
            CommandExposure.SLASH_COMMAND,   # User can /command
        ],
        slash_command="status",
        slash_scope=SlashCommandScope.LOCAL,  # LOCAL or GLOBAL
    )

def _cmd_get_status(self, args):
    return {
        "status": "running",
        "uptime": self.get_uptime()
    }
```

## Command Parameters

**Positional (required):**
- First: Command name (string)
- Second: Handler function (callable)

**Keyword arguments:**
- `title` - Display name (optional)
- `description` - What it does (recommended)
- `expose_as` - List of `CommandExposure` values
- `slash_command` - Slash command name
- `slash_scope` - `SlashCommandScope.LOCAL` or `GLOBAL`
- `arguments` - List of `CommandArgument` objects
- `async_enabled` - Run in thread pool (bool)
- `progress_label` - Label for progress updates

## Exposing Commands

**Import the enum:**
```python
from opperator import CommandExposure
```

**Expose as LLM tool only:**
```python
expose_as=[CommandExposure.AGENT_TOOL]
```

**Expose as slash command only:**
```python
expose_as=[CommandExposure.SLASH_COMMAND]
```

**Expose as both:**
```python
expose_as=[
    CommandExposure.AGENT_TOOL,
    CommandExposure.SLASH_COMMAND,
]
```

## Typed Arguments

Use `CommandArgument` for typed, validated args:

**Import:**
```python
from opperator import CommandArgument
```

**String argument:**
```python
self.register_command(
    "greet",
    self._cmd_greet,
    description="Generate a greeting message",
    expose_as=[CommandExposure.AGENT_TOOL],
    arguments=[
        CommandArgument(
            name="name",
            type="string",
            description="Name of the person to greet",
            required=True,
        ),
    ],
)

def _cmd_greet(self, args):
    name = args["name"]  # Guaranteed to be string
    return {"greeting": f"Hello, {name}!"}
```

**Integer argument:**
```python
CommandArgument(
    name="count",
    type="integer",
    description="Number of items",
    required=True,
)
```

**Number (float) argument:**
```python
CommandArgument(
    name="threshold",
    type="number",
    description="Threshold value",
    required=False,
    default=0.5,
)
```

**Boolean argument:**
```python
CommandArgument(
    name="enabled",
    type="boolean",
    description="Enable feature",
    default=False,
)
```

**Multiple arguments:**
```python
arguments=[
    CommandArgument(
        name="name",
        type="string",
        description="Name of the person to greet",
        required=True,
    ),
    CommandArgument(
        name="enthusiasm",
        type="integer",
        description="Enthusiasm level from 1-10",
        required=False,
        default=5,
    ),
]
```

## Array Arguments

**Array of strings:**
```python
CommandArgument(
    name="tags",
    type="array",
    items={"type": "string"},
    description="List of tags to add",
    required=True,
)

def _cmd_add_tags(self, args):
    tags = args["tags"]  # List of strings
    # tags = ["python", "web", "api"]
```

**Array of integers:**
```python
CommandArgument(
    name="user_ids",
    type="array",
    items={"type": "integer"},
    description="List of user IDs",
    required=True,
)

def _cmd_batch_update(self, args):
    user_ids = args["user_ids"]  # List of integers
    # user_ids = [1, 2, 3, 4, 5]
```

**Array of objects:**
```python
CommandArgument(
    name="users",
    type="array",
    items={
        "type": "object",
        "properties": {
            "name": {"type": "string", "required": True},
            "email": {"type": "string", "required": True},
            "age": {"type": "integer"},
            "active": {"type": "boolean"},
        },
    },
    description="List of user objects to create",
    required=True,
)

def _cmd_create_users(self, args):
    users = args["users"]
    # users = [
    #   {"name": "Alice", "email": "alice@example.com", "age": 30},
    #   {"name": "Bob", "email": "bob@example.com", "active": True}
    # ]
```

**Nested arrays:**
```python
CommandArgument(
    name="matrix",
    type="array",
    items={
        "type": "array",
        "items": {"type": "number"},
    },
    description="2D matrix of numbers",
    required=True,
)

def _cmd_process_matrix(self, args):
    matrix = args["matrix"]
    # matrix = [[1, 2, 3], [4, 5, 6], [7, 8, 9]]
```

## Object Arguments

**Simple object:**
```python
CommandArgument(
    name="config",
    type="object",
    properties={
        "host": {"type": "string", "required": True},
        "port": {"type": "integer", "default": 8080},
        "ssl": {"type": "boolean", "default": False},
    },
    description="Configuration object",
    required=True,
)

def _cmd_configure(self, args):
    config = args["config"]
    # config = {"host": "localhost", "port": 8080, "ssl": True}
```

**Nested objects:**
```python
CommandArgument(
    name="webhook",
    type="object",
    properties={
        "url": {"type": "string", "required": True},
        "headers": {
            "type": "object",
            "properties": {
                "authorization": {"type": "string"},
                "content-type": {"type": "string"},
            },
        },
        "retry": {
            "type": "object",
            "properties": {
                "enabled": {"type": "boolean", "default": True},
                "max_attempts": {"type": "integer", "default": 3},
            },
        },
    },
    description="Webhook configuration",
)
```

## Async Commands

Long-running commands run in thread pool:

```python
self.register_command(
    "run_task",
    self._cmd_run_task,
    description="Simulates a long-running task",
    expose_as=[CommandExposure.AGENT_TOOL],
    arguments=[
        CommandArgument(
            name="duration",
            type="integer",
            description="How long to run (seconds)",
            required=True,
            default=10,
        ),
    ],
    async_enabled=True,          # Run in background
    progress_label="Running task",  # UI label
)

def _cmd_run_task(self, args):
    """This runs in a background thread"""
    duration = args["duration"]

    for i in range(duration):
        # Report progress
        self.report_progress(
            text=f"Step {i+1}/{duration}",
            progress=(i+1) / duration,  # 0.0 to 1.0
        )

        time.sleep(1)

    return {"success": True, "duration": duration}
```

**Configure thread pool size:**
```python
def __init__(self):
    super().__init__(
        name="my_agent",
        max_async_workers=4  # Max concurrent async commands
    )
```

## Progress Reporting

Report progress during command execution:

```python
def _cmd_batch_process(self, args):
    items = args["items"]
    total = len(items)

    for i, item in enumerate(items):
        # Do work
        result = self.process_item(item)

        # Report progress
        self.report_progress(
            text=f"Processed {i+1}/{total} items",
            progress=(i+1) / total,
            metadata={"current": item},
            status="running",
        )

    return {"processed": total}
```

**Progress parameters:**
- `text` - Status message (string)
- `progress` - Percentage (0.0 to 1.0)
- `metadata` - Extra data (dict)
- `status` - Custom status (string)

## Slash Commands

Expose commands for user to type:

```python
from opperator import CommandExposure, SlashCommandScope

self.register_command(
    "status",
    self._cmd_status,
    description="Show agent status",
    expose_as=[CommandExposure.SLASH_COMMAND],
    slash_command="status",  # User types: /status
    slash_scope=SlashCommandScope.GLOBAL,  # or LOCAL
)

def _cmd_status(self, args):
    return f"Status: {self.status}"
```

**Slash scopes:**
```python
SlashCommandScope.GLOBAL  # Available in all agents
SlashCommandScope.LOCAL   # Only when this agent is active
```

**With arguments:**
```python
self.register_command(
    "query",
    self._cmd_query,
    description="Run a database query",
    expose_as=[CommandExposure.SLASH_COMMAND],
    slash_command="query",
    argument_hint="<SQL query>",
    argument_required=True,
)
```

## Handler Return Values

**Return structured data:**
```python
def _cmd_get_user(self, args):
    return {
        "id": 123,
        "name": "Alice",
        "email": "alice@example.com"
    }
```

**Return simple values:**
```python
def _cmd_count(self, args):
    return 42  # Just a number

def _cmd_get_name(self, args):
    return "MyAgent"  # Just a string
```

**Return None:**
```python
def _cmd_clear_cache(self, args):
    self.cache.clear()
    return None  # Or just: return
```

**Raise exceptions:**
```python
def _cmd_delete(self, args):
    item_id = args["item_id"]

    if item_id not in self.items:
        raise ValueError(f"Item {item_id} not found")

    del self.items[item_id]
    return {"deleted": item_id}
```

## Working Directory

Commands receive working directory context:

```python
def _cmd_read_file(self, args):
    file_path = args["file_path"]

    # Get current working directory
    working_dir = self.get_working_directory()

    # Resolve relative paths
    if not os.path.isabs(file_path):
        file_path = os.path.join(working_dir, file_path)

    with open(file_path) as f:
        return f.read()
```

## Complete Example

Full agent with multiple command patterns:

```python
from opperator import (
    OpperatorAgent,
    LogLevel,
    CommandArgument,
    CommandExposure,
    SlashCommandScope,
)
import time

class TaskAgent(OpperatorAgent):
    def __init__(self):
        super().__init__(
            name="task_agent",
            max_async_workers=4,
        )
        self.tasks = {}
        self.next_id = 1

    def initialize(self):
        # Simple command - no args
        self.register_command(
            "ping",
            self._cmd_ping,
        )

        # Tool with typed arguments
        self.register_command(
            "create_task",
            self._cmd_create_task,
            description="Create a new task",
            expose_as=[CommandExposure.AGENT_TOOL],
            arguments=[
                CommandArgument(
                    name="title",
                    type="string",
                    required=True,
                ),
                CommandArgument(
                    name="priority",
                    type="integer",
                    default=0,
                    description="Priority (0-10)",
                ),
            ],
        )

        # Tool + slash command
        self.register_command(
            "complete_task",
            self._cmd_complete_task,
            description="Mark task as completed",
            expose_as=[
                CommandExposure.AGENT_TOOL,
                CommandExposure.SLASH_COMMAND,
            ],
            slash_command="complete",
            slash_scope=SlashCommandScope.LOCAL,
            arguments=[
                CommandArgument(
                    name="task_id",
                    type="integer",
                    required=True,
                ),
            ],
        )

        # Async command with progress
        self.register_command(
            "process_all",
            self._cmd_process_all,
            description="Process all pending tasks",
            expose_as=[CommandExposure.AGENT_TOOL],
            async_enabled=True,
            progress_label="Processing tasks",
        )

        # Slash command only
        self.register_command(
            "status",
            self._cmd_status,
            description="Show task status",
            expose_as=[CommandExposure.SLASH_COMMAND],
            slash_command="status",
            slash_scope=SlashCommandScope.GLOBAL,
        )

    def start(self):
        self.log(LogLevel.INFO, "Task agent ready")

    def _cmd_ping(self, args):
        return {"message": "pong"}

    def _cmd_create_task(self, args):
        task_id = self.next_id
        self.next_id += 1

        task = {
            "id": task_id,
            "title": args["title"],
            "priority": args["priority"],
            "status": "pending",
        }

        self.tasks[task_id] = task
        self.log(LogLevel.INFO, "Task created", task_id=task_id)

        return task

    def _cmd_complete_task(self, args):
        task_id = args["task_id"]

        if task_id not in self.tasks:
            raise ValueError(f"Task {task_id} not found")

        self.tasks[task_id]["status"] = "completed"
        self.log(LogLevel.INFO, "Task completed", task_id=task_id)

        return {"task_id": task_id, "status": "completed"}

    def _cmd_process_all(self, args):
        pending = [t for t in self.tasks.values()
                   if t["status"] == "pending"]
        total = len(pending)

        for i, task in enumerate(pending):
            # Process task
            self.process_task(task)

            # Report progress
            self.report_progress(
                text=f"Processed {i+1}/{total}: {task['title']}",
                progress=(i+1) / total,
            )

            task["status"] = "completed"

        return {"processed": total}

    def _cmd_status(self, args):
        pending = len([t for t in self.tasks.values()
                       if t["status"] == "pending"])
        completed = len([t for t in self.tasks.values()
                        if t["status"] == "completed"])

        return f"Tasks: {pending} pending, {completed} completed"

    def process_task(self, task):
        time.sleep(0.5)  # Simulate work
```

## Argument Validation

Arguments are validated automatically:

**Type coercion:**
```python
# "42" → 42 (string to int)
# "3.14" → 3.14 (string to float)
# "true" → True (string to bool)
# "[1,2,3]" → [1,2,3] (JSON string to array)
```

**Required validation:**
```python
# Missing required arg → Error returned
# LLM calls: {}
# Error: "Missing required argument 'name'"
```

**Type validation:**
```python
# Wrong type → Error returned
# LLM calls: {"count": "not a number"}
# Error: "Cannot interpret 'not a number' as integer"
```

## Best Practices

**Clear descriptions:**
```python
# Good
description="Fetch user profile by user ID"

# Bad
description="Get data"
```

**Meaningful names:**
```python
# Good
CommandArgument(name="user_id", type="string")
CommandArgument(name="include_metadata", type="boolean")

# Bad
CommandArgument(name="id", type="string")
CommandArgument(name="flag", type="boolean")
```

**Validate business logic:**
```python
def _cmd_set_threshold(self, args):
    threshold = args["threshold"]

    if threshold < 0 or threshold > 100:
        raise ValueError("Threshold must be 0-100")

    self.threshold = threshold
    return {"threshold": threshold}
```

**Return structured data:**
```python
# Good
return {"status": "success", "user": {...}}

# Bad
return "User created successfully!"
```

**Use async for long operations:**
```python
# If it takes >1 second, use async
async_enabled=True
```

## Common Patterns

**CRUD operations:**
```python
self.register_command("create_item", self._create, ...)
self.register_command("get_item", self._get, ...)
self.register_command("update_item", self._update, ...)
self.register_command("delete_item", self._delete, ...)
self.register_command("list_items", self._list, ...)
```

**Search with filters:**
```python
arguments=[
    CommandArgument(name="query", type="string", required=True),
    CommandArgument(name="limit", type="integer", default=10),
    CommandArgument(name="offset", type="integer", default=0),
]
```

**Enum for choices:**
```python
CommandArgument(
    name="status",
    type="string",
    enum=["all", "pending", "completed"],
    default="all",
)
```

## Summary

**Basic registration:**
```python
self.register_command("name", self._handler)
```

**Full registration:**
```python
self.register_command(
    "name",
    self._handler,
    description="...",
    expose_as=[CommandExposure.AGENT_TOOL],
    arguments=[CommandArgument(...)],
)
```

**Handler signature:**
```python
def _handler(self, args: Dict[str, Any]) -> Any:
    # args = validated arguments dict
    # return JSON-serializable data
    # or raise exceptions
```

**Expose options:**
- `CommandExposure.AGENT_TOOL` - LLM calls it
- `CommandExposure.SLASH_COMMAND` - User /command
- Both - Available to LLM and user

**Async for long work:**
```python
async_enabled=True
self.report_progress(text="...", progress=0.5)
```

Commands are how the LLM and users
interact with your agent's functionality.
