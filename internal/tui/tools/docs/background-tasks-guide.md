# Background Tasks

Agents run background threads for periodic work,
monitoring, and async operations. Use threading
patterns for safe concurrent execution.

## Starting Background Threads

**In start() method:**
```python
import threading

def __init__(self):
    super().__init__(name="my_agent")
    self._stop_worker = threading.Event()
    self._worker_thread = None

def start(self):
    """Start background worker"""
    self._stop_worker.clear()
    self._worker_thread = threading.Thread(
        target=self._worker_loop,
        name="worker-thread",
        daemon=True,  # Don't prevent shutdown
    )
    self._worker_thread.start()

    self.log(LogLevel.INFO, "Background worker started")

def _worker_loop(self):
    """Background worker function"""
    while not self._stop_worker.is_set():
        # Do periodic work
        self.do_work()

        # Wait before next iteration
        # Use wait() instead of sleep() for clean shutdown
        self._stop_worker.wait(timeout=10.0)
```

**Why daemon=True:**
- Daemon threads don't prevent process exit
- Process can shutdown even if thread is running
- Still need graceful shutdown in cleanup()

## Graceful Shutdown

**Signal worker to stop:**
```python
def on_shutdown(self):
    """Called when shutdown signal received"""
    self.log(LogLevel.INFO, "Stopping background worker")
    self._stop_worker.set()  # Signal worker to stop

def cleanup(self):
    """Wait for worker to finish"""
    self._stop_worker.set()

    if self._worker_thread and self._worker_thread.is_alive():
        # Wait up to 5 seconds for thread to finish
        self._worker_thread.join(timeout=5)

        if self._worker_thread.is_alive():
            self.log(LogLevel.WARNING, "Worker thread did not stop in time")

    super().cleanup()  # Always call parent cleanup
```

**Why use Event.wait() instead of time.sleep():**
```python
# Good: Can be interrupted
while not self._stop_worker.is_set():
    self._stop_worker.wait(timeout=10.0)  # Wakes on set()

# Bad: Can't be interrupted
while not self._stop_worker.is_set():
    time.sleep(10.0)  # Always sleeps full 10 seconds
```

## Thread Safety

**Protect shared state with locks:**
```python
import threading

def __init__(self):
    super().__init__(name="my_agent")
    self._state_lock = threading.Lock()
    self.counter = 0

def _worker_loop(self):
    """Background thread increments counter"""
    while not self._stop_worker.is_set():
        # Acquire lock before modifying shared state
        with self._state_lock:
            self.counter += 1

        self._stop_worker.wait(timeout=1.0)

def _cmd_get_count(self, args):
    """Command handler reads counter"""
    # Acquire lock before reading shared state
    with self._state_lock:
        count = self.counter

    return {"count": count}
```

**Why locks are needed:**
- Background threads and command handlers run concurrently
- Without locks, race conditions cause incorrect values
- Python's GIL doesn't prevent all race conditions

## Common Patterns

**Periodic task:**
```python
def _heartbeat_worker(self):
    """Send heartbeat every 30 seconds"""
    while not self._stop_worker.is_set():
        # Send heartbeat
        with self._state_lock:
            self.heartbeat_count += 1
            count = self.heartbeat_count

        self.log(LogLevel.DEBUG, "Heartbeat", count=count)

        # Update sidebar
        self.update_section(
            "status",
            f'<c fg="green">● Running</c>\n'
            f'<b>Heartbeats:</b> {count}'
        )

        # Wait 30 seconds (or until shutdown)
        self._stop_worker.wait(timeout=30.0)
```

**File monitoring:**
```python
def _file_monitor_worker(self):
    """Monitor files for changes"""
    last_modified = {}

    while not self._stop_worker.is_set():
        # Check for file changes
        for file_path in self.watched_files:
            try:
                mtime = os.path.getmtime(file_path)

                if file_path not in last_modified:
                    last_modified[file_path] = mtime
                elif mtime > last_modified[file_path]:
                    # File changed
                    self.log(LogLevel.INFO, "File changed", path=file_path)
                    self.handle_file_change(file_path)
                    last_modified[file_path] = mtime

            except OSError:
                # File doesn't exist or can't access
                pass

        # Check every 5 seconds
        self._stop_worker.wait(timeout=5.0)
```

**Queue processing:**
```python
from queue import Queue, Empty

def __init__(self):
    super().__init__(name="queue_agent")
    self._stop_worker = threading.Event()
    self._worker_thread = None
    self.queue = Queue()

def _queue_worker(self):
    """Process items from queue"""
    while not self._stop_worker.is_set():
        try:
            # Wait for item with timeout
            item = self.queue.get(timeout=1.0)

            # Process item
            self.process_item(item)

            # Mark task as done
            self.queue.task_done()

        except Empty:
            # No items in queue, continue loop
            continue

def _cmd_add_task(self, args):
    """Add task to queue"""
    task = args["task"]
    self.queue.put(task)

    return {
        "queued": True,
        "queue_size": self.queue.qsize()
    }
```

## Multiple Background Threads

**Multiple workers:**
```python
def start(self):
    """Start multiple background workers"""
    # Heartbeat thread
    self._stop_heartbeat = threading.Event()
    self._heartbeat_thread = threading.Thread(
        target=self._heartbeat_worker,
        name="heartbeat",
        daemon=True
    )
    self._heartbeat_thread.start()

    # Monitor thread
    self._stop_monitor = threading.Event()
    self._monitor_thread = threading.Thread(
        target=self._monitor_worker,
        name="monitor",
        daemon=True
    )
    self._monitor_thread.start()

def cleanup(self):
    """Stop all workers"""
    # Signal all workers to stop
    self._stop_heartbeat.set()
    self._stop_monitor.set()

    # Wait for threads
    if self._heartbeat_thread:
        self._heartbeat_thread.join(timeout=2)
    if self._monitor_thread:
        self._monitor_thread.join(timeout=2)

    super().cleanup()
```

## Thread Pool for Commands

**Async commands use thread pool:**
```python
def __init__(self):
    super().__init__(
        name="my_agent",
        max_async_workers=4  # Thread pool size
    )

def initialize(self):
    self.register_command(
        "long_task",
        self._cmd_long_task,
        async_enabled=True,  # Runs in thread pool
    )

def _cmd_long_task(self, args):
    """This runs in background thread from pool"""
    # No need to create thread manually
    # OpperatorAgent handles it

    for i in range(10):
        self.report_progress(
            text=f"Step {i+1}/10",
            progress=(i+1) / 10
        )
        time.sleep(1)

    return {"completed": True}
```

## Complete Example

Full agent with background tasks:

```python
from opperator import OpperatorAgent, LogLevel
import threading
import time
from queue import Queue, Empty

class WorkerAgent(OpperatorAgent):
    def __init__(self):
        super().__init__(name="worker_agent")

        # Background worker control
        self._stop_worker = threading.Event()
        self._worker_thread = None

        # Shared state (protected by lock)
        self._state_lock = threading.Lock()
        self.tasks_processed = 0
        self.last_task_time = None

        # Task queue
        self.task_queue = Queue()

    def initialize(self):
        self.set_description("Background task processor")

        self.register_section(
            "status",
            "Worker Status",
            '<c fg="gray">○ Initializing</c>'
        )

        self.register_section(
            "stats",
            "Statistics",
            "<b>Tasks processed:</b> 0",
            collapsed=True
        )

        # Register command to add tasks
        self.register_command(
            "add_task",
            self._cmd_add_task,
            description="Add task to queue"
        )

    def start(self):
        """Start background worker"""
        # Start worker thread
        self._stop_worker.clear()
        self._worker_thread = threading.Thread(
            target=self._worker_loop,
            name="task-worker",
            daemon=True
        )
        self._worker_thread.start()

        # Update status
        self.update_section(
            "status",
            '<c fg="green">● Running</c>'
        )

        self.log(LogLevel.INFO, "Worker started")

    def _worker_loop(self):
        """Background worker that processes tasks"""
        while not self._stop_worker.is_set():
            try:
                # Get task from queue (with timeout)
                task = self.task_queue.get(timeout=1.0)

                # Process task
                self.log(LogLevel.INFO, "Processing task", task=task)
                self.process_task(task)

                # Update stats
                with self._state_lock:
                    self.tasks_processed += 1
                    self.last_task_time = time.time()
                    count = self.tasks_processed

                # Update sidebar
                self.update_section(
                    "stats",
                    f"<b>Tasks processed:</b> {count}\n"
                    f"<b>Queue size:</b> {self.task_queue.qsize()}"
                )

                # Mark task done
                self.task_queue.task_done()

            except Empty:
                # No tasks in queue, continue
                continue

            except Exception as exc:
                self.log(
                    LogLevel.ERROR,
                    "Task processing failed",
                    error=str(exc)
                )

    def process_task(self, task):
        """Process a single task (simulated work)"""
        time.sleep(2)  # Simulate work

    def _cmd_add_task(self, args):
        """Add task to processing queue"""
        task = args.get("task", "default")

        # Add to queue
        self.task_queue.put(task)

        queue_size = self.task_queue.qsize()

        self.log(
            LogLevel.INFO,
            "Task queued",
            task=task,
            queue_size=queue_size
        )

        return {
            "queued": True,
            "task": task,
            "queue_size": queue_size
        }

    def on_shutdown(self):
        """Signal worker to stop"""
        self.log(LogLevel.INFO, "Shutting down worker")
        self._stop_worker.set()

        # Update status
        self.update_section(
            "status",
            '<c fg="yellow">○ Stopping</c>'
        )

    def cleanup(self):
        """Wait for worker to finish"""
        self._stop_worker.set()

        if self._worker_thread and self._worker_thread.is_alive():
            self.log(LogLevel.INFO, "Waiting for worker thread")
            self._worker_thread.join(timeout=5)

            if self._worker_thread.is_alive():
                self.log(
                    LogLevel.WARNING,
                    "Worker thread did not stop in time"
                )

        super().cleanup()
```

## Thread-Local Storage

**Command-specific data:**
```python
# OpperatorAgent already uses thread-local storage
# for command context:

def _cmd_example(self, args):
    # These work because of thread-local storage:
    self.report_progress(...)  # Knows which command
    working_dir = self.get_working_directory()  # Has context
```

**Custom thread-local:**
```python
import threading

def __init__(self):
    super().__init__(name="my_agent")
    self._thread_local = threading.local()

def _worker_loop(self):
    # Set thread-local data
    self._thread_local.worker_id = "worker-1"

    while not self._stop_worker.is_set():
        # Access thread-local data
        worker_id = self._thread_local.worker_id
        self.log(LogLevel.DEBUG, "Working", worker_id=worker_id)
        self._stop_worker.wait(timeout=5.0)
```

## Best Practices

**Use Event.wait() for shutdown:**
```python
# Good: Responds quickly to shutdown
while not self._stop.is_set():
    self._stop.wait(timeout=10.0)

# Bad: Slow to shutdown
while not self._stop.is_set():
    time.sleep(10.0)
```

**Always use locks for shared state:**
```python
# Good: Thread-safe
with self._lock:
    self.counter += 1

# Bad: Race condition
self.counter += 1
```

**Set daemon=True:**
```python
# Good: Won't block shutdown
threading.Thread(target=func, daemon=True)

# Bad: Blocks shutdown
threading.Thread(target=func, daemon=False)
```

**Join threads in cleanup:**
```python
# Good: Clean shutdown
def cleanup(self):
    self._stop.set()
    self._thread.join(timeout=5)
    super().cleanup()

# Bad: Orphaned threads
def cleanup(self):
    self._stop.set()
    super().cleanup()
```

**Handle exceptions in workers:**
```python
# Good: Log and continue
def _worker_loop(self):
    while not self._stop.is_set():
        try:
            self.do_work()
        except Exception as exc:
            self.log(LogLevel.ERROR, "Worker error", error=str(exc))
        self._stop.wait(timeout=5.0)

# Bad: Crash on first error
def _worker_loop(self):
    while not self._stop.is_set():
        self.do_work()  # Uncaught exception kills thread
        self._stop.wait(timeout=5.0)
```

## Troubleshooting

**Thread doesn't stop:**
- Check `join(timeout=...)` is called
- Verify thread checks stop event
- Look for blocking operations (IO, locks)
- Use shorter timeouts in wait()

**Race conditions:**
- Add locks around shared state
- Use thread-safe data structures (Queue)
- Avoid modifying state from multiple threads

**High CPU usage:**
- Don't use tight loops without delays
- Use `Event.wait()` with reasonable timeout
- Process items in batches

## Summary

**Start background thread:**
```python
def start(self):
    self._stop = threading.Event()
    self._thread = threading.Thread(
        target=self._worker,
        daemon=True
    )
    self._thread.start()
```

**Worker loop:**
```python
def _worker(self):
    while not self._stop.is_set():
        self.do_work()
        self._stop.wait(timeout=10.0)
```

**Graceful shutdown:**
```python
def on_shutdown(self):
    self._stop.set()

def cleanup(self):
    self._stop.set()
    self._thread.join(timeout=5)
    super().cleanup()
```

**Thread safety:**
```python
with self._lock:
    self.shared_state = value
```

Background threads enable agents to perform
periodic tasks, monitoring, and async work.
