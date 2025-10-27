# Python Dependencies

Managing Python packages for Opperator agents requires understanding isolated virtual environments. This guide shows the correct workflow for installing dependencies.

## Critical Concept: Isolated Environments

**Each Opperator agent has its own isolated virtual environment (VENV).**

- Agent VENV location: `~/.config/opperator/agents/<AGENT_NAME>/.venv/`
- Builder agent environment: Different from agent environments
- System Python: Different from all agent environments

**CRITICAL**: Installing packages in the wrong environment is the most common dependency error. Packages must be installed **inside the specific agent's VENV**, not in the system Python or Builder environment.

## Standard Installation Workflow

Follow this 4-step process for every agent dependency change:

### 1. Stop the Agent

```bash
stop_agent(agent_name="my_agent")
```

Always stop the agent before modifying files or dependencies.

### 2. Define Requirements

Create or update `requirements.txt` in the agent's workspace:

**File: ~/.config/opperator/agents/my_agent/requirements.txt**
```
requests==2.31.0
pandas>=2.0.0
python-dotenv==1.0.0
```

**Best practices:**
- Pin exact versions for stability (`==2.31.0`)
- Use minimum versions for flexibility (`>=2.0.0`)
- List one package per line
- Include all dependencies explicitly

### 3. Install Into Agent's VENV

**MANDATORY command pattern:**

```bash
~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/<AGENT_NAME>/requirements.txt
```

**Example for agent named "sora-agent":**

```bash
~/.config/opperator/agents/sora-agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/sora-agent/requirements.txt
```

**Why this works:**
- Uses the agent's isolated Python interpreter (`.venv/bin/python`)
- Runs `pip` as a module (`-m pip`) for reliability
- Installs packages directly into the agent's VENV
- Reads from the agent's requirements file

**Why other approaches fail:**
- `pip install -r requirements.txt` - Uses wrong Python/environment
- `python -m pip install` - Uses Builder or system Python
- Installing packages manually - Inconsistent, hard to reproduce

### 4. Restart the Agent

```bash
restart_agent(agent_name="my_agent")
```

The agent loads the new dependencies when it starts.

## Complete Example

**Scenario**: Add `requests` library to existing agent

```bash
# 1. Stop the agent
stop_agent(agent_name="webhook_agent")

# 2. Create requirements.txt
view("~/.config/opperator/agents/webhook_agent/requirements.txt")
# If file doesn't exist, create it
write(
    path="~/.config/opperator/agents/webhook_agent/requirements.txt",
    content="requests==2.31.0\n"
)

# 3. Install into agent's VENV
bash("""
~/.config/opperator/agents/webhook_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/webhook_agent/requirements.txt
""")

# 4. Restart agent
restart_agent(agent_name="webhook_agent")

# 5. Test the import
focus_agent(agent_name="webhook_agent")
# Agent can now: import requests
```

## Troubleshooting

### "ModuleNotFoundError" After Installation

**Symptom**: Agent fails with `ModuleNotFoundError: No module named 'requests'`

**Cause**: Package installed in wrong environment

**Solution**: Verify you used the agent's VENV Python:

```bash
# Check what's installed in agent VENV
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip list

# Reinstall using correct path
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/my_agent/requirements.txt
```

### Corrupted or Missing VENV

**Symptom**: VENV directory missing or `pip` not found

**Cause**: VENV was deleted, corrupted, or never created

**Solution**: Rebuild the VENV from scratch:

```bash
# 1. Remove corrupted VENV
bash("rm -rf ~/.config/opperator/agents/my_agent/.venv")

# 2. Create fresh VENV
bash("python -m venv ~/.config/opperator/agents/my_agent/.venv")

# 3. Install dependencies
bash("""
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/my_agent/requirements.txt
""")

# 4. Restart agent
restart_agent(agent_name="my_agent")
```

**Note**: `bootstrap_new_agent` creates the VENV automatically. Manual recreation is only needed for recovery.

### Package Installation Fails

**Symptom**: `pip install` returns errors or fails to complete

**Common causes:**
- Network issues (pypi.org unavailable)
- Version conflicts (incompatible package versions)
- Missing system dependencies (C libraries for compiled packages)
- Disk space exhausted

**Solutions:**

```bash
# Check VENV Python and pip versions
~/.config/opperator/agents/my_agent/.venv/bin/python --version
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip --version

# Upgrade pip first
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install --upgrade pip

# Install with verbose output to see errors
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/my_agent/requirements.txt -v

# Try installing packages one at a time
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install requests==2.31.0
```

### "Permission Denied" Errors

**Symptom**: Installation fails with permission errors

**Cause**: File ownership or permissions issue in VENV directory

**Solution**:

```bash
# Check ownership
ls -la ~/.config/opperator/agents/my_agent/.venv/

# Fix permissions if needed
chmod -R u+w ~/.config/opperator/agents/my_agent/.venv/

# Retry installation
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/my_agent/requirements.txt
```

## Advanced Patterns

### Using pyproject.toml

For agents with complex dependency management:

**File: ~/.config/opperator/agents/my_agent/pyproject.toml**
```toml
[project]
name = "my-agent"
version = "1.0.0"
dependencies = [
    "requests>=2.31.0",
    "pandas>=2.0.0",
    "python-dotenv>=1.0.0"
]

[project.optional-dependencies]
dev = [
    "pytest>=7.0.0",
    "black>=23.0.0"
]
```

**Install from pyproject.toml:**

```bash
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -e \
~/.config/opperator/agents/my_agent/
```

### Development Dependencies

Separate production and development dependencies:

**requirements.txt** (production):
```
requests==2.31.0
pandas==2.0.3
```

**requirements-dev.txt** (development):
```
-r requirements.txt
pytest==7.4.0
black==23.7.0
mypy==1.5.0
```

**Install development dependencies:**

```bash
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/my_agent/requirements-dev.txt
```

### Checking Installed Packages

```bash
# List all installed packages
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip list

# Show specific package details
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip show requests

# Check for outdated packages
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip list --outdated
```

### Upgrading Packages

```bash
# Upgrade specific package
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install --upgrade requests

# Upgrade all packages (use with caution)
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip list --outdated --format=freeze | \
grep -v '^\-e' | cut -d = -f 1 | \
xargs -n1 ~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install --upgrade
```

## Common Mistakes to Avoid

**DON'T: Install in wrong environment**
```bash
# Wrong - uses system Python
pip install requests

# Wrong - uses Builder environment
python -m pip install requests

# Wrong - uses uv in Builder context
uv pip install requests
```

**DO: Use agent's VENV Python**
```bash
# Correct - uses agent's isolated environment
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/my_agent/requirements.txt
```

**DON'T: Forget to restart agent**
```python
# Agent won't see new packages until restarted
# Must call restart_agent() after installation
```

**DO: Always restart after dependency changes**
```bash
# Complete workflow
stop_agent(agent_name="my_agent")
# ... install dependencies ...
restart_agent(agent_name="my_agent")
```

**DON'T: Skip requirements.txt**
```bash
# Fragile - not reproducible
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip install requests pandas numpy
```

**DO: Maintain requirements.txt**
```bash
# Reproducible - source of truth
# Always update requirements.txt first, then install from it
```

## Quick Reference

**Standard installation command:**
```bash
~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/<AGENT_NAME>/requirements.txt
```

**VENV rebuild (corruption recovery):**
```bash
rm -rf ~/.config/opperator/agents/<AGENT_NAME>/.venv && \
python -m venv ~/.config/opperator/agents/<AGENT_NAME>/.venv && \
~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/<AGENT_NAME>/requirements.txt
```

**Check installed packages:**
```bash
~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python -m pip list
```

## Summary

**Key principles:**
1. Each agent has its own isolated VENV
2. Always use the agent's VENV Python for installations
3. Maintain requirements.txt as source of truth
4. Stop agent before installing, restart after

**Standard workflow:**
1. Stop agent
2. Update requirements.txt
3. Install using agent's VENV Python
4. Restart agent

**Command pattern:**
```bash
~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python -m pip install -r \
~/.config/opperator/agents/<AGENT_NAME>/requirements.txt
```

Following this workflow ensures dependencies are correctly installed in the agent's isolated environment and available when the agent runs.
