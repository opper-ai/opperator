# Python Dependencies

Managing Python packages for Opperator agents using modern tooling with `uv` and `pyproject.toml`. This guide shows the correct workflow for installing dependencies into isolated agent environments.

## Critical Concept: Isolated Environments

**Each Opperator agent has its own isolated virtual environment (VENV).**

- Agent workspace: `~/.config/opperator/agents/<AGENT_NAME>/`
- Agent VENV: `~/.config/opperator/agents/<AGENT_NAME>/.venv/`
- Builder environment: Different from agent environments
- System Python: Different from all agent environments

**CRITICAL**: Installing packages in the wrong environment is the most common dependency error. Packages must be installed **inside the specific agent's VENV**, not in the system Python or Builder environment.

## Standard Installation Workflow

Follow this 4-step process for every agent dependency change:

### 1. Stop the Agent

```bash
stop_agent(agent_name="my_agent")
```

Always stop the agent before modifying files or dependencies.

### 2. Define Dependencies

Create or update `pyproject.toml` in the agent's workspace:

**File: ~/.config/opperator/agents/my_agent/pyproject.toml**
```toml
[project]
name = "my-agent"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
    "requests>=2.31.0",
    "pandas>=2.0.0",
    "python-dotenv>=1.0.0",
]
```

**Best practices:**
- Use minimum version constraints (`>=2.31.0`) for flexibility
- Specify `requires-python` to ensure Python version compatibility
- Group related packages together
- Use optional dependencies for dev/test packages (see Advanced Patterns)

### 3. Install Dependencies

Install packages into the agent's VENV using `uv pip install`:

```bash
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**What this does:**
- Resolves all dependencies from `pyproject.toml`
- Installs packages into the agent's `.venv/`
- Fast, reliable dependency resolution (10-100x faster than pip)
- Targets the specific agent's isolated environment

**Why this works:**
- `--python` flag targets the agent's specific VENV Python interpreter
- First argument (`~/.config/opperator/agents/my_agent/`) is the directory containing `pyproject.toml`
- `uv` automatically reads dependencies from `pyproject.toml`
- No chance of installing into wrong environment

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

# 2. Create or update pyproject.toml
write(
    path="~/.config/opperator/agents/webhook_agent/pyproject.toml",
    content="""[project]
name = "webhook-agent"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
    "requests>=2.31.0",
]
"""
)

# 3. Install dependencies
bash("""
uv pip install ~/.config/opperator/agents/webhook_agent/ \\
  --python ~/.config/opperator/agents/webhook_agent/.venv/bin/python
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

**Cause**: Package not installed or wrong environment used

**Solution**: Verify you used the correct `--python` flag:

```bash
# Check what's installed in agent VENV
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip list

# Reinstall with correct command
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python

# Restart agent
restart_agent(agent_name="my_agent")
```

### Corrupted or Missing VENV

**Symptom**: VENV directory missing or errors about missing Python

**Cause**: VENV was deleted, corrupted, or never created

**Solution**: Recreate the VENV and reinstall dependencies:

```bash
# 1. Remove corrupted VENV
bash("rm -rf ~/.config/opperator/agents/my_agent/.venv")

# 2. Create fresh VENV
bash("uv venv ~/.config/opperator/agents/my_agent/.venv")

# 3. Install dependencies
bash("""
uv pip install ~/.config/opperator/agents/my_agent/ \\
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
""")

# 4. Restart agent
restart_agent(agent_name="my_agent")
```

**Note**: `bootstrap_new_agent` creates the VENV automatically. Manual recreation is only needed for recovery.

### Dependency Conflicts

**Symptom**: Installation fails with version conflict errors

**Common causes:**
- Incompatible version constraints in dependencies
- Conflicting transitive dependencies
- Outdated or incorrect version specifications

**Solutions:**

```bash
# Run with verbose output to see conflict details
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python \
  --verbose

# Try relaxing version constraints in pyproject.toml
# Change: requests==2.31.0
# To:     requests>=2.31.0
```

### Installation Fails

**Symptom**: Installation returns errors or fails to complete

**Common causes:**
- Network issues (pypi.org unavailable)
- Missing system dependencies (C libraries for compiled packages)
- Disk space exhausted

**Solutions:**

```bash
# Run with verbose output
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python \
  --verbose

# Check disk space
df -h

# For packages requiring system dependencies (e.g., psycopg2, pillow),
# install system libraries first:
# macOS: brew install postgresql libpq
# Ubuntu: apt-get install libpq-dev python3-dev
```

### Permission Denied Errors

**Symptom**: Installation fails with permission errors

**Cause**: File ownership or permissions issue in workspace

**Solution**:

```bash
# Check ownership
ls -la ~/.config/opperator/agents/my_agent/

# Fix permissions if needed
chmod -R u+w ~/.config/opperator/agents/my_agent/

# Retry installation
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

## Advanced Patterns

### Optional Dependencies

Organize dependencies by purpose:

**File: ~/.config/opperator/agents/my_agent/pyproject.toml**
```toml
[project]
name = "my-agent"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
    "requests>=2.31.0",
    "pandas>=2.0.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.4.0",
    "black>=23.7.0",
    "mypy>=1.5.0",
]
test = [
    "pytest>=7.4.0",
    "pytest-cov>=4.0.0",
]
docs = [
    "sphinx>=7.0.0",
    "sphinx-rtd-theme>=1.0.0",
]
```

**Install with optional dependencies:**

```bash
# Install with dev dependencies
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python \
  --extra dev

# Install with multiple extras
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python \
  --extra dev --extra test

# Install all optional dependencies
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python \
  --all-extras
```

### Development vs Production

Keep production lean while having rich dev tools:

```toml
[project]
dependencies = [
    # Only production dependencies here
    "requests>=2.31.0",
    "pandas>=2.0.0",
]

[project.optional-dependencies]
dev = [
    # Development tools
    "pytest>=7.4.0",
    "black>=23.7.0",
    "ruff>=0.1.0",
    "ipdb>=0.13.0",
]
```

**Production install (default):**
```bash
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**Development install:**
```bash
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python \
  --extra dev
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

### Installing Single Packages

For quick one-off package additions without updating `pyproject.toml`:

```bash
# Install a single package
uv pip install requests \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python

# Install specific version
uv pip install "requests>=2.31.0" \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**Note**: This doesn't update `pyproject.toml`. For reproducibility, always add the package to `pyproject.toml` and reinstall from the project directory.

### Upgrading Packages

```bash
# Upgrade specific package to latest version
uv pip install --upgrade requests \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python

# Upgrade all packages from pyproject.toml to latest compatible versions
uv pip install --upgrade ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

### Freezing Exact Versions

To capture exact versions currently installed (for debugging or reproduction):

```bash
# Generate requirements.txt with exact versions
~/.config/opperator/agents/my_agent/.venv/bin/python -m pip freeze > \
  ~/.config/opperator/agents/my_agent/requirements-frozen.txt

# Reinstall from frozen versions
uv pip install -r ~/.config/opperator/agents/my_agent/requirements-frozen.txt \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**Note**: `pyproject.toml` remains the source of truth. Use frozen requirements only for temporary reproduction needs.

## Common Mistakes to Avoid

**DON'T: Install in wrong environment**
```bash
# Wrong - uses system/Builder environment
pip install requests
python -m pip install requests
uv pip install requests  # Missing --python flag!
```

**DO: Always use --python flag**
```bash
# Correct - targets agent's isolated environment
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**DON'T: Forget to reinstall after editing pyproject.toml**
```toml
# Just editing pyproject.toml doesn't install packages
# Must run: uv pip install with --python flag
```

**DO: Always reinstall after dependency changes**
```bash
# Complete workflow
stop_agent(agent_name="my_agent")
# ... edit pyproject.toml ...
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
restart_agent(agent_name="my_agent")
```

**DON'T: Use wrong path in first argument**
```bash
# Wrong - points to venv instead of project directory
uv pip install ~/.config/opperator/agents/my_agent/.venv/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**DO: Point to project directory containing pyproject.toml**
```bash
# Correct - first arg is project dir, --python is venv
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python
```

**DON'T: Forget to restart agent**
```bash
# Agent won't see new packages until restarted
# Must call restart_agent() after installation
```

**DO: Always restart after dependency changes**
```bash
stop_agent(agent_name="my_agent")
# ... install dependencies ...
restart_agent(agent_name="my_agent")
```

## Quick Reference

**Standard workflow:**
```bash
# 1. Stop agent
stop_agent(agent_name="my_agent")

# 2. Edit dependencies
# Edit: ~/.config/opperator/agents/my_agent/pyproject.toml

# 3. Install
uv pip install ~/.config/opperator/agents/my_agent/ \
  --python ~/.config/opperator/agents/my_agent/.venv/bin/python

# 4. Restart
restart_agent(agent_name="my_agent")
```

**VENV rebuild (corruption recovery):**
```bash
rm -rf ~/.config/opperator/agents/<AGENT_NAME>/.venv
uv venv ~/.config/opperator/agents/<AGENT_NAME>/.venv
uv pip install ~/.config/opperator/agents/<AGENT_NAME>/ \
  --python ~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python
```

**Check installed packages:**
```bash
~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python -m pip list
```

**Install with extras:**
```bash
uv pip install ~/.config/opperator/agents/<AGENT_NAME>/ \
  --python ~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python \
  --extra dev
```

**Upgrade all packages:**
```bash
uv pip install --upgrade ~/.config/opperator/agents/<AGENT_NAME>/ \
  --python ~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python
```

## Why This Approach Works

### Modern Python Packaging
- **`pyproject.toml`**: Industry standard (PEP 621) for Python projects
- **`uv`**: Next-generation package manager (10-100x faster than pip)
- **Explicit targeting**: `--python` flag ensures correct environment every time

### Better Than pip
- **Speed**: Fast dependency resolution
- **Reliability**: Better conflict detection
- **Simplicity**: One command installs everything from `pyproject.toml`

### Isolated Environments
- Each agent has its own VENV
- No dependency conflicts between agents
- Builder environment stays clean
- System Python untouched

### Maintainability
- `pyproject.toml` is single source of truth
- Easy to see what dependencies each agent needs
- Optional dependencies keep production lean
- Upgrades are explicit and controlled

## Summary

**Key principles:**
1. Each agent has its own isolated VENV
2. Always use `uv pip install` with `--python` flag
3. Maintain `pyproject.toml` as source of truth
4. Stop agent before installing, restart after

**Standard workflow:**
1. Stop agent
2. Update `pyproject.toml`
3. Run `uv pip install <project-dir> --python <venv-python>`
4. Restart agent

**Command pattern:**
```bash
uv pip install ~/.config/opperator/agents/<AGENT_NAME>/ \
  --python ~/.config/opperator/agents/<AGENT_NAME>/.venv/bin/python
```

Following this workflow ensures dependencies are correctly installed in the agent's isolated environment using modern, fast tooling.
