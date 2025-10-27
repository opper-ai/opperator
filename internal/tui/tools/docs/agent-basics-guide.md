# Agent Basics

Agents extend the OpperatorAgent base class
and implement required lifecycle methods.

## Minimal Agent

**File: agent.py**
```python
from opperator import OpperatorAgent, LogLevel

class MyAgent(OpperatorAgent):
    def initialize(self):
        """Called once during startup"""
        self.log(LogLevel.INFO, "Initializing agent")
        # Load resources, setup state
        self.counter = 0

    def start(self):
        """Called after initialization"""
        self.log(LogLevel.INFO, "Agent started")
        # Start background tasks, open connections

if __name__ == "__main__":
    agent = MyAgent(name="my_agent", version="1.0.0")
    agent.run()
```

**Run it:**
```bash
python agent.py
```

## Required Methods

**initialize()** - Setup phase
- Load configuration
- Initialize state variables
- Register commands and sections
- Called once before start()

**start()** - Activation phase
- Open connections
- Start background threads
- Begin processing
- Called after initialize()

## Optional Lifecycle Hooks

**main_loop()** - Custom event loop
```python
def main_loop(self):
    """Override default wait-for-shutdown loop"""
    while self.running:
        self.process_events()
        time.sleep(0.1)
```

**on_shutdown()** - Cleanup before exit
```python
def on_shutdown(self):
    """Called when agent receives shutdown signal"""
    self.log(LogLevel.INFO, "Saving state before shutdown")
    self.save_state()
    self.close_connections()
```

**cleanup()** - Final resource cleanup
```python
def cleanup(self):
    """Called after shutdown, guaranteed to run"""
    # Close files, release resources
    if self.db:
        self.db.close()
    # Always call parent cleanup
    super().cleanup()
```

**on_config_update(config)** - Hot reload
```python
def on_config_update(self, config):
    """Called when config file changes"""
    self.log(LogLevel.INFO, "Config updated", config=config)
    self.threshold = config.get("threshold", 100)
```

**on_status()** - Status signal handler
```python
def on_status(self):
    """Called when status signal received (SIGUSR1)"""
    self.log(LogLevel.INFO, "Status check",
             requests=self.request_count,
             uptime=time.time() - self.start_time)
```

## Logging

Use `self.log()` to send structured logs.

**Log levels:**
```python
LogLevel.DEBUG   # Detailed debug info
LogLevel.INFO    # General information
LogLevel.WARNING # Warning messages
LogLevel.ERROR   # Error conditions
LogLevel.FATAL   # Fatal errors
```

**Basic logging:**
```python
self.log(LogLevel.INFO, "Processing started")
self.log(LogLevel.WARNING, "High memory usage")
self.log(LogLevel.ERROR, "Connection failed")
```

**Structured fields:**
```python
self.log(
    LogLevel.INFO,
    "Request processed",
    request_id="abc123",
    duration_ms=245,
    status="success"
)
```

**Multiple fields:**
```python
self.log(
    LogLevel.ERROR,
    "Database error",
    error=str(exc),
    query=query,
    retries=3,
    connection=conn_id
)
```

## Configuration Loading

**Access config:**
```python
def initialize(self):
    threshold = self.config.get("threshold", 100)

    # For secrets, use get_secret()
    api_key = self.get_secret("API_KEY")
```

## Agent Metadata

**Set description:**
```python
def initialize(self):
    self.set_description(
        "Monitors webhooks and processes events"
    )
```

The description appears in the UI when
the agent is selected.

## Complete Lifecycle Example

Full agent showing all lifecycle stages:

```python
import time
from opperator import OpperatorAgent, LogLevel

class TaskAgent(OpperatorAgent):
    def __init__(self):
        super().__init__(
            name="task_agent",
            version="1.0.0"
        )
        self.tasks = []
        self.start_time = None

    def initialize(self):
        """Setup phase"""
        self.log(LogLevel.INFO, "Initializing task agent")

        self.set_description(
            "Processes background tasks from queue"
        )

        # Load config
        self.max_tasks = self.config.get("max_tasks", 10)
        self.poll_interval = self.config.get("poll_interval", 5)

        # Initialize state
        self.tasks = []
        self.processed_count = 0

        self.log(
            LogLevel.INFO,
            "Configuration loaded",
            max_tasks=self.max_tasks,
            poll_interval=self.poll_interval
        )

    def start(self):
        """Activation phase"""
        self.log(LogLevel.INFO, "Task agent starting")
        self.start_time = time.time()

        # Connect to task queue
        self.connect_to_queue()

        self.log(LogLevel.INFO, "Task agent ready")

    def main_loop(self):
        """Custom event loop"""
        while self.running:
            # Fetch tasks from queue
            new_tasks = self.fetch_tasks()

            if new_tasks:
                self.log(
                    LogLevel.INFO,
                    "Fetched tasks",
                    count=len(new_tasks)
                )
                self.tasks.extend(new_tasks)

            # Process tasks
            if self.tasks:
                task = self.tasks.pop(0)
                self.process_task(task)
                self.processed_count += 1

            # Wait before next poll
            time.sleep(self.poll_interval)

    def process_task(self, task):
        """Process a single task"""
        self.log(
            LogLevel.INFO,
            "Processing task",
            task_id=task["id"],
            task_type=task["type"]
        )

        try:
            # Do the work
            result = self.execute_task(task)

            self.log(
                LogLevel.INFO,
                "Task completed",
                task_id=task["id"],
                result=result
            )
        except Exception as exc:
            self.log(
                LogLevel.ERROR,
                "Task failed",
                task_id=task["id"],
                error=str(exc)
            )

    def on_status(self):
        """Status signal handler"""
        uptime = time.time() - self.start_time
        self.log(
            LogLevel.INFO,
            "Agent status",
            uptime_seconds=int(uptime),
            processed_count=self.processed_count,
            pending_tasks=len(self.tasks)
        )

    def on_config_update(self, config):
        """Config hot reload"""
        old_interval = self.poll_interval
        self.poll_interval = config.get("poll_interval", 5)

        if old_interval != self.poll_interval:
            self.log(
                LogLevel.INFO,
                "Poll interval updated",
                old=old_interval,
                new=self.poll_interval
            )

    def on_shutdown(self):
        """Graceful shutdown"""
        self.log(
            LogLevel.INFO,
            "Shutting down",
            pending_tasks=len(self.tasks)
        )

        # Save pending tasks
        if self.tasks:
            self.save_tasks(self.tasks)
            self.log(
                LogLevel.INFO,
                "Saved pending tasks",
                count=len(self.tasks)
            )

        # Disconnect from queue
        self.disconnect_from_queue()

    def cleanup(self):
        """Final cleanup"""
        self.log(LogLevel.INFO, "Cleanup complete")
        super().cleanup()

    # Helper methods
    def connect_to_queue(self):
        # Connect to task queue
        pass

    def disconnect_from_queue(self):
        # Disconnect from queue
        pass

    def fetch_tasks(self):
        # Fetch new tasks
        return []

    def execute_task(self, task):
        # Execute task logic
        return "success"

    def save_tasks(self, tasks):
        # Persist pending tasks
        pass

if __name__ == "__main__":
    agent = TaskAgent()
    agent.run()
```

## Constructor Parameters

**name** - Agent identifier
```python
agent = OpperatorAgent(name="webhook_agent")
```
Defaults to class name if not provided.

**version** - Agent version string
```python
agent = OpperatorAgent(version="2.1.0")
```
Defaults to "1.0.0".

**max_async_workers** - Thread pool size
```python
agent = OpperatorAgent(max_async_workers=4)
```
For async command execution (covered in commands guide).

## Running State

**self.running** - Boolean flag
```python
def main_loop(self):
    while self.running:
        self.do_work()
        time.sleep(1)
```

Set to True when agent starts, False on shutdown.

**self.config** - Configuration dict
```python
def initialize(self):
    api_url = self.config.get("api_url")
    timeout = self.config.get("timeout", 30)
```

Populated by `load_config()` method.

## Error Handling

**Fatal errors** - Exit the agent
```python
def initialize(self):
    if not os.path.exists("required_file.txt"):
        self.log(
            LogLevel.FATAL,
            "Required file missing",
            file="required_file.txt"
        )
        sys.exit(1)
```

**Recoverable errors** - Log and continue
```python
def process_item(self, item):
    try:
        result = self.do_work(item)
    except Exception as exc:
        self.log(
            LogLevel.ERROR,
            "Item processing failed",
            item_id=item["id"],
            error=str(exc)
        )
        # Continue processing other items
```

**Retry logic:**
```python
def fetch_data(self, url):
    for attempt in range(3):
        try:
            return self.http_get(url)
        except Exception as exc:
            self.log(
                LogLevel.WARNING,
                "Fetch failed, retrying",
                url=url,
                attempt=attempt+1,
                error=str(exc)
            )
            time.sleep(2 ** attempt)

    self.log(LogLevel.ERROR, "All retries failed", url=url)
    raise
```

## Execution Flow

**Startup sequence:**
1. `__init__()` - Create agent instance
2. Signal handlers setup
3. `load_config()` - Load configuration
4. `initialize()` - Setup agent
5. Publish commands
6. Start message reader thread
7. Send ready signal
8. `start()` - Activate agent
9. `main_loop()` - Run until shutdown

**Shutdown sequence:**
1. Receive SIGINT or SIGTERM
2. Set `self.running = False`
3. Call `on_shutdown()`
4. Exit main loop
5. Call `cleanup()`
6. Exit process

## Best Practices

**Separate setup from activation:**
```python
def initialize(self):
    # Load resources, don't start processing
    self.load_database_schema()
    self.load_models()

def start(self):
    # Now activate
    self.begin_accepting_requests()
```

**Use structured logging:**
```python
# Good: structured fields
self.log(LogLevel.INFO, "Request handled",
         method="POST", path="/api/task", duration_ms=42)

# Bad: string formatting
self.log(LogLevel.INFO,
         f"Handled POST /api/task in 42ms")
```

**Handle shutdown gracefully:**
```python
def on_shutdown(self):
    # Finish current work
    if self.current_task:
        self.complete_task(self.current_task)

    # Save state
    self.save_checkpoint()

    # Release resources
    self.close_connections()
```

**Don't block initialization:**
```python
# Bad: long-running work in initialize
def initialize(self):
    self.process_all_files()  # Could take minutes!

# Good: defer to main_loop
def initialize(self):
    self.files_to_process = self.scan_directory()

def main_loop(self):
    for file in self.files_to_process:
        self.process_file(file)
```

## Common Patterns

**Background thread:**
```python
def start(self):
    self.running = True
    self.worker_thread = threading.Thread(
        target=self.worker_loop,
        daemon=True
    )
    self.worker_thread.start()

def worker_loop(self):
    while self.running:
        self.do_background_work()
        time.sleep(1)

def cleanup(self):
    self.running = False
    if self.worker_thread:
        self.worker_thread.join(timeout=5)
    super().cleanup()
```

**Periodic tasks:**
```python
def main_loop(self):
    last_cleanup = time.time()

    while self.running:
        # Main work
        self.process_queue()

        # Periodic cleanup every 5 minutes
        if time.time() - last_cleanup > 300:
            self.cleanup_old_data()
            last_cleanup = time.time()

        time.sleep(1)
```

**Lazy initialization:**
```python
def initialize(self):
    self._db = None

@property
def db(self):
    if self._db is None:
        self._db = self.connect_database()
    return self._db
```

## Troubleshooting

**Agent not starting?**
- Check agent logs for errors
- Verify required files exist
- Check config.json is valid JSON
- Ensure no exceptions in initialize()

**Agent exits immediately?**
- Override main_loop() if needed
- Check for exceptions in start()
- Verify self.running stays True
- Look for sys.exit() calls

**Changes not taking effect?**
- Restart agent to reload code
- Check on_config_update() is implemented
- Verify config file is being watched
- Check file permissions

## Summary

**Required methods:**
- `initialize()` - Setup agent state
- `start()` - Begin processing

**Optional hooks:**
- `main_loop()` - Custom event loop
- `on_shutdown()` - Graceful shutdown
- `cleanup()` - Final resource cleanup
- `on_config_update()` - Hot reload
- `on_status()` - Status signal

**Key APIs:**
- `self.log(level, message, **fields)` - Logging
- `self.set_description(text)` - Set agent description
- `self.config` - Configuration dict
- `self.running` - Running state flag

**Lifecycle:**
1. Initialize (setup)
2. Start (activate)
3. Main loop (process)
4. Shutdown (cleanup)

Build agents by extending OpperatorAgent
and implementing the required methods.
