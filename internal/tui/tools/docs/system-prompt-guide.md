# System Prompt

The system prompt appends to the base LLM
prompt. It informs the LLM about agent state,
capabilities, and context.

## Setting System Prompt

**On initialization:**
```python
def initialize(self):
    self.set_system_prompt("""
You are a webhook processing agent.

Current state: Initializing
Webhooks processed: 0
Queue size: 0
    """.strip())
```

**Update anytime:**
```python
def process_webhook(self, webhook):
    self.processed_count += 1

    # Update LLM with new state
    self.set_system_prompt(f"""
You are a webhook processing agent.

Current state: Running
Webhooks processed: {self.processed_count}
Queue size: {len(self.queue)}
Last webhook: {webhook['id']}
    """.strip())
```

## When to Update

**On significant state changes:**
```python
def connect_to_database(self):
    self.db = Database.connect()

    self.set_system_prompt(f"""
Database agent ready.

Connection: Active
Tables: {len(self.db.tables())}
Mode: {self.db_mode}
    """)
```

**After commands execute:**
```python
def start_monitoring_command(self, args):
    target = args["target"]
    self.monitoring_targets.add(target)

    self.set_system_prompt(f"""
Monitoring agent tracking {len(self.monitoring_targets)} targets.

Targets: {', '.join(self.monitoring_targets)}
Alert threshold: {self.threshold}
    """)
```

**On lifecycle events:**
```python
def on_agent_activated(self, previous_agent, conversation_id):
    self.set_system_prompt(f"""
Task agent now active.

Conversation: {conversation_id}
Previous agent: {previous_agent or 'None'}
Pending tasks: {len(self.tasks)}
    """)
```

## Keep it Concise

**Bad: Too verbose**
```python
self.set_system_prompt("""
You are a file processing agent responsible for
monitoring a directory and processing files as
they arrive. You should process each file
according to its type and then move it to the
appropriate output directory. Currently you are
monitoring /data/inbox and have processed 42
files so far. The last file processed was
document.pdf which was processed at 14:30:22
and took 1.2 seconds to complete. The current
queue has 3 files waiting: image.jpg, data.csv,
and report.docx. Your processing rate is
currently 35 files per hour.
""")
```

**Good: Concise summary**
```python
self.set_system_prompt(f"""
File processing agent.

Status: Running
Processed: 42 files
Queue: 3 pending
Rate: 35/hour
    """.strip())
```

## Structure Guidelines

**Use sections for clarity:**
```python
self.set_system_prompt(f"""
Database query agent.

CONNECTION:
- Status: Active
- Database: {self.db_name}
- Tables: {len(self.tables)}

CURRENT OPERATION:
- Query: {self.current_query[:50]}...
- Duration: {elapsed}s

STATS:
- Queries executed: {self.query_count}
- Cache hit rate: {self.cache_rate}%
    """.strip())
```

**Key information first:**
```python
# Good: Important info at top
self.set_system_prompt(f"""
Status: ERROR - Database connection lost
Last successful query: 5 minutes ago
Retry attempts: 3/5
    """)

# Bad: Buried the error
self.set_system_prompt(f"""
Database agent version 1.2.0
Uptime: 2 hours
Queries processed: 1,234
Status: ERROR - Database connection lost
    """)
```

## Dynamic State Updates

**Real-time state tracking:**
```python
class MonitoringAgent(OpperatorAgent):
    def initialize(self):
        self.alerts = []
        self.update_prompt()

    def update_prompt(self):
        alert_summary = "None"
        if self.alerts:
            recent = self.alerts[-3:]  # Last 3
            alert_summary = "\n".join([
                f"- {a['severity']}: {a['message']}"
                for a in recent
            ])

        self.set_system_prompt(f"""
Monitoring agent.

Active alerts: {len(self.alerts)}
{alert_summary}

Monitors: {len(self.monitors)} active
        """.strip())

    def add_alert(self, severity, message):
        self.alerts.append({
            'severity': severity,
            'message': message,
            'time': time.time()
        })
        self.update_prompt()  # Update immediately
```

## Capability Listing

**Inform LLM of available actions:**
```python
def initialize(self):
    self.set_system_prompt(f"""
API client agent.

Capabilities:
- GET/POST/PUT/DELETE requests
- OAuth2 authentication
- Rate limiting: {self.rate_limit}/min
- Retry logic: {self.max_retries} attempts

API: {self.api_base_url}
Auth: {'Active' if self.token else 'Not configured'}
    """.strip())
```

**Update when capabilities change:**
```python
def authenticate(self, token):
    self.token = token
    self.authenticated = True

    self.set_system_prompt(f"""
API client agent.

Status: Authenticated ✓
Endpoints: {len(self.endpoints)} available
Rate limit: {self.remaining}/{self.rate_limit}
    """.strip())
```

## Context-Aware Prompts

**Different contexts, different prompts:**
```python
def on_agent_activated(self, previous_agent, conversation_id):
    # User just switched to this agent
    self.set_system_prompt(f"""
Build agent now active.

Recent builds: {self.recent_builds_summary()}
Current branch: {self.get_current_branch()}
    """.strip())

def on_agent_deactivated(self, next_agent):
    # User switched away - minimal state
    self.set_system_prompt("""
Build agent (inactive).
    """)
```

## Error States

**Clearly communicate errors:**
```python
def on_connection_error(self, error):
    self.set_system_prompt(f"""
⚠ API agent - CONNECTION ERROR

Error: {error}
Last success: {self.last_success_time}
Retry in: {self.retry_delay}s

Status: Will auto-reconnect
    """.strip())
```

**Recovery updates:**
```python
def on_reconnect(self):
    self.set_system_prompt(f"""
API agent - Reconnected ✓

Connection: Restored
Backlog: {len(self.pending_requests)} requests
Processing: Resuming
    """.strip())
```

## Complete Example

Agent that updates prompt throughout lifecycle:

```python
class TaskQueueAgent(OpperatorAgent):
    def initialize(self):
        self.tasks = []
        self.processed = 0
        self.failed = 0

        self.set_system_prompt("""
Task queue agent initializing...
        """)

    def start(self):
        self.update_prompt()

    def update_prompt(self):
        """Central prompt update"""
        status = "Running" if self.running else "Stopped"

        # Build task summary
        if not self.tasks:
            task_info = "No pending tasks"
        else:
            next_task = self.tasks[0]
            task_info = f"""
Next: {next_task['type']}
Queue: {len(self.tasks)} pending"""

        # Build stats
        total = self.processed + self.failed
        success_rate = 0
        if total > 0:
            success_rate = int((self.processed / total) * 100)

        self.set_system_prompt(f"""
Task queue agent.

STATUS: {status}
{task_info}

STATS:
- Completed: {self.processed}
- Failed: {self.failed}
- Success rate: {success_rate}%
        """.strip())

    def add_task(self, task):
        self.tasks.append(task)
        self.update_prompt()

    def process_next_task(self):
        if not self.tasks:
            return

        task = self.tasks.pop(0)

        try:
            self.execute_task(task)
            self.processed += 1
        except Exception as exc:
            self.log(LogLevel.ERROR, "Task failed", error=str(exc))
            self.failed += 1

        self.update_prompt()

    def on_shutdown(self):
        self.set_system_prompt(f"""
Task queue agent shutting down.

Pending tasks: {len(self.tasks)}
Session stats: {self.processed} completed, {self.failed} failed
        """.strip())
```

## Prompt Templates

**Reusable template pattern:**
```python
class DatabaseAgent(OpperatorAgent):
    def initialize(self):
        self.status = "initializing"
        self.connection = None
        self.queries_count = 0

    def _build_prompt(self):
        """Centralized prompt builder"""
        conn_status = "Connected" if self.connection else "Disconnected"

        prompt = f"Database agent.\n\nStatus: {self.status}\n"
        prompt += f"Connection: {conn_status}\n"

        if self.connection:
            prompt += f"Database: {self.connection.name}\n"
            prompt += f"Queries: {self.queries_count}\n"

        if self.status == "error":
            prompt += f"\nERROR: {self.last_error}\n"

        return prompt.strip()

    def update_status(self, status):
        """Update status and refresh prompt"""
        self.status = status
        self.set_system_prompt(self._build_prompt())

    def execute_query(self, query):
        result = self.connection.execute(query)
        self.queries_count += 1
        self.set_system_prompt(self._build_prompt())
        return result
```

## Best Practices

**Update on state changes, not constantly:**
```python
# Good: Update when something changes
def add_item(self, item):
    self.items.append(item)
    self.update_prompt()

# Bad: Update every iteration
def main_loop(self):
    while self.running:
        self.do_work()
        self.set_system_prompt(...)  # Too frequent!
        time.sleep(0.1)
```

**Use helper method for updates:**
```python
# Good: Centralized
def update_prompt(self):
    self.set_system_prompt(self._build_prompt())

def _build_prompt(self):
    return f"Agent state: {self.state}"

# Bad: Duplicated logic
def method_a(self):
    self.set_system_prompt(f"Agent state: {self.state}")

def method_b(self):
    self.set_system_prompt(f"Agent state: {self.state}")
```

**Summarize lists, don't enumerate:**
```python
# Good: Summary
items = ["file1.txt", "file2.txt", ..., "file50.txt"]
prompt = f"Processing {len(items)} files"

# Bad: Full list
prompt = f"Processing: {', '.join(items)}"  # Way too long!
```

**Show recent items only:**
```python
# Good: Last 3 items
recent = self.history[-3:]
summary = "\n".join([f"- {item}" for item in recent])

# Bad: Full history
summary = "\n".join([f"- {item}" for item in self.history])
```

## Formatting Tips

**Use bullet points:**
```python
self.set_system_prompt("""
Agent status:
- Active connections: 5
- Pending requests: 12
- Error rate: 0.1%
""")
```

**Use key-value pairs:**
```python
self.set_system_prompt(f"""
Status: Running
Queue size: {len(self.queue)}
Workers: {self.worker_count}
""")
```

**Group related info:**
```python
self.set_system_prompt(f"""
File processor.

INPUT:
- Directory: {self.input_dir}
- Files pending: {len(self.pending)}

OUTPUT:
- Directory: {self.output_dir}
- Files processed: {self.processed_count}
""")
```

## Common Mistakes

**❌ Too much detail:**
```python
# Bad
self.set_system_prompt(f"""
Processing file located at /very/long/path/to/file.txt
which was created on 2024-01-15 at 14:23:45 and has
a size of 1,234,567 bytes and contains 45,678 lines...
""")
```

**✓ High-level summary:**
```python
# Good
self.set_system_prompt(f"""
Processing: file.txt (1.2MB)
Progress: 45%
""")
```

**❌ Static information:**
```python
# Bad - doesn't change
self.set_system_prompt("""
This agent processes webhooks from external services.
It validates the payload, stores it in the database,
and triggers downstream processes.
""")
```

**✓ Dynamic state:**
```python
# Good - updates with state
self.set_system_prompt(f"""
Webhook agent.
Processed: {self.count}
Active: {self.active}
""")
```

## Summary

**System prompt purpose:**
- Inform LLM of current agent state
- List available capabilities
- Show context and recent activity
- Communicate errors or warnings

**Update triggers:**
- Significant state changes
- After command execution
- On lifecycle events (activated, etc.)
- When capabilities change

**Keep it concise:**
- Summarize, don't enumerate
- Show recent items only (last 3-5)
- Key information first
- Use sections for organization

**Best practices:**
- Centralize prompt building logic
- Update on changes, not constantly
- Use helper methods
- Group related information

The system prompt gives the LLM real-time
awareness of agent state and capabilities.
