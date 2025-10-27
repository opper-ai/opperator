# Agents YAML Configuration

Agents are configured in YAML files that
define how the daemon manages them. The YAML
specifies the command, environment, and restart
behavior.

## Basic Configuration

**Minimal agent:**
```yaml
agents:
  - name: my_agent
    command: python3
    args:
      - /path/to/agent.py
    process_root: /path/to/working/directory
```

**With description:**
```yaml
agents:
  - name: webhook_agent
    description: Processes incoming webhooks
    command: python3
    args:
      - /opt/agents/webhook/agent.py
    process_root: /opt/agents/webhook
```

## Configuration Fields

**name** - Agent identifier (required)
```yaml
name: task_processor
```
- Unique identifier for the agent
- Used in commands and UI
- Should be descriptive

**description** - Human-readable description
```yaml
description: Processes background tasks from queue
```
- Shown in agent picker
- Optional but recommended

**color** - UI color code
```yaml
color: "#00ff00"
```
- Hex color for UI highlighting
- Optional

**command** - Executable path (required)
```yaml
command: python3
# or
command: /usr/local/bin/node
# or
command: ./my-agent-binary
```
- Path to executable
- Can be absolute or in PATH

**args** - Command arguments
```yaml
args:
  - agent.py
  - --verbose
  - --config=/etc/agent/config.json
```
- List of arguments passed to command
- Can be empty

**process_root** - Working directory (required)
```yaml
process_root: /opt/agents/my_agent
```
- Where the agent runs
- Used for relative paths
- Agent's `get_working_directory()` starts here

**env** - Environment variables
```yaml
env:
  LOG_LEVEL: debug
  API_URL: https://api.example.com
  TIMEOUT: "30"
```
- Key-value pairs
- Available to agent process
- Values must be strings

## Auto-Restart Configuration

**auto_restart** - Enable automatic restart
```yaml
auto_restart: true
```
- Restart if agent crashes
- Default: false

**max_restarts** - Restart limit
```yaml
auto_restart: true
max_restarts: 5
```
- Maximum restart attempts
- 0 = unlimited
- Default: 0 (unlimited)

**Example with restart:**
```yaml
agents:
  - name: monitor_agent
    command: python3
    args: [monitor.py]
    process_root: /opt/monitor
    auto_restart: true
    max_restarts: 3
```

## Daemon Startup

**start_with_daemon** - Auto-start on daemon launch
```yaml
start_with_daemon: true
```
- Start when daemon starts
- Default: false
- Useful for always-on agents

**Example:**
```yaml
agents:
  - name: system_monitor
    command: python3
    args: [monitor.py]
    process_root: /opt/monitor
    start_with_daemon: true
    auto_restart: true
```


## System Prompt

**system_prompt** - Default LLM instructions
```yaml
system_prompt: |
  You are a webhook processing agent.
  Process webhooks and trigger appropriate actions.
  Log all webhook events for audit purposes.
```
- Multi-line string supported with `|`
- Can be updated at runtime via `set_system_prompt()`
- Optional

## Complete Examples

**Simple agent:**
```yaml
agents:
  - name: hello_agent
    command: python3
    args: [agent.py]
    process_root: /opt/hello
```

**Production agent with all features:**
```yaml
agents:
  - name: webhook_processor
    description: Processes incoming webhooks from external services
    color: "#4CAF50"
    command: python3
    args:
      - webhook_agent.py
      - --config=/etc/webhook/config.json
    process_root: /opt/webhook
    env:
      LOG_LEVEL: info
      API_URL: https://api.internal.example.com
      WEBHOOK_SECRET: use_secret_manager
      MAX_WORKERS: "4"
    auto_restart: true
    max_restarts: 5
    start_with_daemon: true
    system_prompt: |
      You are the webhook processing agent.

      Capabilities:
      - Process webhooks from GitHub, Stripe, Slack
      - Validate webhook signatures
      - Trigger downstream workflows

      Current status: Ready
```


## Multiple Agents

**agents.yaml with multiple agents:**
```yaml
agents:
  - name: monitor
    description: System monitoring agent
    command: python3
    args: [monitor.py]
    process_root: /opt/monitor
    start_with_daemon: true
    auto_restart: true

  - name: webhook
    description: Webhook handler
    command: python3
    args: [webhook.py]
    process_root: /opt/webhook
    start_with_daemon: true
    auto_restart: true
    env:
      PORT: "8080"

  - name: backup
    description: Daily backup agent
    command: python3
    args: [backup.py]
    process_root: /opt/backup
```

## Environment Variables

The `env` field in agents.yaml allows you to set environment
variables for your agent process. These are inherited by the
agent when the daemon starts it.

**Example:**
```yaml
env:
  LOG_LEVEL: debug
  API_URL: https://api.example.com
  ENABLE_WEBHOOKS: "true"
```

**Note:** For secrets like API keys, use the secret manager
with `self.get_secret()` instead of environment variables.

## Configuration File Location

**Default location:**
```bash
~/.config/opperator/agents.yaml
```

**Custom location:**
```bash
opperator daemon --config /path/to/agents.yaml
```

**Check current config:**
```bash
opperator config show
```

## Validation

**Required fields:**
- `name` - Must be unique
- `command` - Must be executable
- `process_root` - Must exist

**Optional fields:**
- `description` - Recommended
- `color` - Defaults to theme color
- `args` - Defaults to empty
- `env` - Defaults to empty
- `auto_restart` - Defaults to false
- `max_restarts` - Defaults to 0 (unlimited)
- `start_with_daemon` - Defaults to false
- `system_prompt` - Defaults to empty

**Invalid configurations:**
```yaml
# Missing required fields
agents:
  - name: bad_agent
    # Missing command!
    process_root: /opt/agent

# Duplicate names
agents:
  - name: agent1
    command: python3
    args: [a.py]
    process_root: /opt/a
  - name: agent1  # ERROR: duplicate!
    command: python3
    args: [b.py]
    process_root: /opt/b
```

## Reloading Configuration

**Hot reload:**
```bash
# Send SIGHUP to daemon
kill -HUP $(pgrep opperator-daemon)

# Or use command
opperator daemon reload
```

**What happens:**
- Daemon reads configuration file
- New agents are started
- Removed agents are stopped
- Modified agents are restarted
- Running agents with unchanged config continue

**Agent receives:**
```python
def on_config_update(self, config):
    """Called when config is reloaded"""
    self.log(LogLevel.INFO, "Config updated", config=config)
```

## Best Practices

**Use absolute paths:**
```yaml
# Good
command: /usr/bin/python3
process_root: /opt/agents/webhook

# Bad - brittle
command: python3  # Which python3?
process_root: ./agents  # Relative to what?
```

**Descriptive names:**
```yaml
# Good
name: github_webhook_processor
description: Processes webhooks from GitHub

# Bad
name: agent1
description: Agent
```

**Enable auto-restart for services:**
```yaml
# Service agents that should always run
- name: monitor
  auto_restart: true
  start_with_daemon: true

# One-time tasks
- name: backup
  auto_restart: false
```

**Don't store secrets in YAML:**
```yaml
# Bad - secrets in config!
env:
  API_KEY: sk-1234567890abcdef

# Good - use secret manager
env:
  USE_SECRETS: "true"
# Then in agent:
# api_key = self.get_secret("api_key")
```

## Troubleshooting

**Agent not starting:**
- Check `command` path is correct
- Check `process_root` exists
- Check file permissions
- Look at daemon logs

**Agent crashes immediately:**
- Check agent logs
- Verify environment variables
- Test command manually:
  ```bash
  cd /opt/agent
  python3 agent.py
  ```

**Config changes not applying:**
- Reload daemon: `kill -HUP $(pgrep opperator-daemon)`
- Check YAML syntax: `yamllint agents.yaml`
- Check daemon logs for errors

## Summary

**Minimal agent configuration:**
```yaml
agents:
  - name: agent_name
    command: /path/to/executable
    args: [arg1, arg2]
    process_root: /working/directory
```

**Common optional fields:**
- `description` - Human-readable description
- `env` - Environment variables
- `auto_restart` - Crash recovery
- `start_with_daemon` - Auto-start
- `system_prompt` - Default LLM instructions

**Configuration file:**
- Default: `~/.config/opperator/agents.yaml`
- Hot reload: `kill -HUP <daemon-pid>`
- Validation on startup

YAML configuration controls how the daemon
manages and monitors agents.
