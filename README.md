<div align="center">

# Opperator

**Opperator is a framework for building and running general AI agents locally from your terminal**
</br>
</br>

<img src="https://i.imgur.com/Sa6m3En.jpeg">

</br>
</br>

[![Documentation](https://img.shields.io/badge/docs-docs.opper.ai-14cdcd.svg)](https://docs.opper.ai/opperator)
[![License](https://img.shields.io/badge/license-Apache%20License%202.0-blue)](LICENSE)
<a href="https://github.com/opper-ai/opperator/releases"><img src="https://img.shields.io/github/release/opper-ai/opperator" alt="Latest Release"></a>
  <a href="https://github.com/opper-ai/opperator/stargazers"><img src="https://img.shields.io/github/stars/opper-ai/opperator?style=social" alt="GitHub stars"></a>

[Features](#features) • [Quick Start](#quick-start) • [Documentation](https://docs.opper.ai/opperator) • [Community](#community)

</div>

---

## Overview

Opperator is a managed system for building AI agents that automate your personal workflows. Agents run locally with direct access to your files, local AI models, and personal data. Code them once and they handle the tasks that are uniquely yours.

LLMs can now write focused, targeted code for any task. Opperator provides the runtime to execute that code, a managed system to operate it, and a customizable TUI to control everything from one place.

### The Problem

Building AI agents today requires solving infrastructure problems that have nothing to do with your actual workflow:

- Where do agents live? How do you manage their runtime?
- How do you handle boilerplate and scaffolding?
- Where do you store API keys and secrets?
- How do you debug agents when something breaks?
- How do you test changes without constant restarts?

### The Solution

Opperator is a managed system that solves all of this for you. Agents run locally under a built-in daemon. Code is generated from templates. Secrets are managed securely in your system keyring. Everything is debugged from one terminal UI. You iterate without restarting. **You focus on the workflow, not the infrastructure.**

## Quick Start

### System Requirements

- **Operating System**: macOS or Linux
- **Python**: 3.8 or higher
- **Recommended**: [uv](https://docs.astral.sh/uv/) for faster dependency management (not required)

### Installation

```bash
curl -fsSL https://opper.ai/opperator-install | bash
```

### Launch Opperator

```bash
op
# or the full command name
opperator
```

Throughout this README, we use `op` for brevity, but both commands work identically.

### Authentication & Credits

On first run, you'll be guided through authentication. Opperator requires an [Opper account](https://opper.ai) and uses Gemini Flash 2.5 by default for agent interactions. New accounts receive **$10 in inference credits** to get started.

> **Note**: When building agents, the Builder uses the Opper SDK by default. However, individual agents can use any LLM provider (OpenAI, Anthropic, local models, etc.) in their code. We're working on making Opperator fully standalone without requiring an Opper account.

### Create Your First Agent

**Interactive Mode (Recommended)**

1. Launch Opperator: `op`
2. Switch to Builder agent: Press `Shift+Tab` or type `/agent builder`
3. Describe your agent:
   ```
   I want to create an agent that analyzes my screenshots folder
   and renames files based on their content.
   ```
4. The Builder will interactively create, test, and deliver your agent

**CLI Mode**

```bash
# Bootstrap the agent structure
op agent bootstrap my_agent -d "Description of what my agent does"

# Navigate to the agent directory
cd ~/.config/opperator/agents/my_agent

# Edit in your preferred editor
code main.py  # VS Code, or vim, cursor, etc.

# Start the agent
op agent start my_agent
```

### Interact with Your Agent

Switch between agents with `Shift+Tab`, then interact using:

- **Natural language**: `Process the files in my downloads folder`
- **Slash commands**: `/process_files` (calls agent commands directly)


## Features

### Terminal Interface
- TUI for managing multiple agents
- Real-time status updates and logs
- Custom sidebar sections for agent-specific information
- Switch between agents with keyboard shortcuts (`Shift+Tab`)

### Agent Management
- **Builder Agent** - Creates new agents from natural language descriptions
- **Python SDK** - Framework for process management and LLM integration
- **Lifecycle Hooks** - Control initialization, startup, shutdown, and cleanup operations
- **Hot Reloading** - Test changes without restarting agents
- **Multi-Daemon Support** - Run agents across local and remote daemons

### Developer Experience
- **Scaffolding** - Bootstrap agents from CLI or TUI
- **Editor Freedom** - Write agents in VS Code, Cursor, Vim, or any editor you prefer
- **LLM-Callable Commands** - Register commands that LLMs can call as tools
- **Custom Status Display** - Show metrics and progress in the sidebar
- **Secrets Management** - System keyring integration for API keys and credentials
- **Logging** - Built-in debugging and monitoring

### Cloud Deployment
- **Deploy to VPS** - Deploy daemons to cloud providers (Hetzner, AWS, etc.)
- **Agent Migration** - Move agents between local and remote daemons
- **Remote Management** - Update cloud deployments with a single command
- **Multi-Daemon Registry** - Manage multiple daemon connections

### System Reliability
- **Process Isolation** - Agent failures don't cascade to other agents
- **Auto-Restart** - Configurable automatic restart on crashes
- **Message Persistence** - Conversation history stored in SQLite
- **Async Tasks** - Background task management and monitoring
- **IPC** - Unix sockets for TUI-daemon communication, pipes for daemon-agent communication

## Architecture

**Terminal UI (TUI)**
- Frontend interface for interacting with agents
- Handles input, displays responses, shows status information
- Remains responsive even during long-running tasks

**Daemon**
- Background process coordinating the entire system
- Routes messages between TUI and agents
- Persists conversation history to SQLite
- Manages secrets securely in system keyring
- Monitors agent health and handles restarts

**Agents**
- Independent Python processes handling specific tasks
- Run in isolation with their own dependencies and configuration
- Can register commands, display custom UI elements, and manage state
- Failures don't cascade to other components

## CLI Reference

### Core Commands
```bash
op                          # Start the Opperator TUI
op setup                    # Initialize and configure authentication
op doctor                   # Run diagnostics on your installation
```

### Agent Management
```bash
op agent list               # List all agents and their status
op agent bootstrap <name>   # Create a new agent
op agent start <name>       # Start an agent
op agent stop <name>        # Stop an agent
op agent restart <name>     # Restart an agent
op agent delete <name>      # Delete an agent and all data
op agent logs <name> -f     # Follow agent logs in real-time
op agent commands <name>    # List available commands for an agent
```

### Secret Management
```bash
op secret create <name>     # Create a new secret (prompts for value)
op secret list              # List all stored secrets
op secret read <name>       # Read a secret value
op secret update <name>     # Update an existing secret
op secret delete <name>     # Delete a secret
```

### Cloud Deployment
```bash
op cloud deploy             # Interactive wizard to deploy daemon
op cloud list               # List all cloud deployments
op cloud update <name>      # Update cloud daemon binary
op cloud destroy <name>     # Destroy cloud VPS
```

### Daemon Management
```bash
op daemon status            # Check daemon status
op daemon list              # List all configured daemons
op daemon add <name>        # Register new daemon connection
op daemon test <name>       # Test daemon connectivity
op daemon metrics           # Display daemon metrics
```

See the complete [CLI Reference](https://docs.opper.ai/opperator/cli-reference) for all commands and flags.

## Configuration

Opperator stores configuration in `~/.config/opperator/`:

```
~/.config/opperator/
├── agents.yaml           # Agent configuration
├── daemons.yaml          # Daemon connections registry
├── preferences.yaml      # User preferences
├── agent_data.json       # Agent metadata
├── opperator.db          # SQLite database (conversations, logs)
├── agents/               # Individual agent directories
│   └── {agent-name}/
│       ├── main.py       # Agent code
│       ├── venv/         # Virtual environment
│       └── requirements.txt
└── logs/                 # Log files
```

## Use Cases

Opperator excels at automating personal workflows that require:

- **File Processing** - Organize photos, rename files, convert formats
- **API Integration** - Monitor services, process webhooks, sync data
- **Content Generation** - Create videos, images, or text content with AI
- **Email Automation** - Filter messages, send notifications, process attachments
- **Data Analysis** - Parse logs, generate reports, track metrics
- **Development Workflows** - Run tests, deploy code, monitor builds
- **Custom Automation** - Any workflow unique to your needs

Check out the [Sora Video Generator Guide](https://docs.opper.ai/opperator/guides/generating-videos-with-sora) for a complete example.

## Documentation

Comprehensive documentation is available at [docs.opper.ai/opperator](https://docs.opper.ai/opperator).

### Getting Started
- [Installation](https://docs.opper.ai/opperator/installation)
- [Building Your First Agent](https://docs.opper.ai/opperator/building-your-first-agent)
- [How Opperator Works](https://docs.opper.ai/opperator/how-opperator-works)

### Core Concepts
- [Agent Lifecycle](https://docs.opper.ai/opperator/agent-lifecycle)
- [Commands & Tools](https://docs.opper.ai/opperator/commands-and-tools)
- [System Prompts](https://docs.opper.ai/opperator/system-prompts)
- [State Management](https://docs.opper.ai/opperator/state-management)
- [Custom Sidebars](https://docs.opper.ai/opperator/custom-sidebars)
- [Lifecycle Events](https://docs.opper.ai/opperator/lifecycle-events)

### Reference
- [CLI Reference](https://docs.opper.ai/opperator/cli-reference)
- [TUI Reference](https://docs.opper.ai/opperator/tui-reference)
- [agents.yaml Reference](https://docs.opper.ai/opperator/agents-yaml-reference)
- [Cloud Deployment](https://docs.opper.ai/opperator/cloud-deployment)

### Guides
- [Generating Videos with Sora](https://docs.opper.ai/opperator/guides/generating-videos-with-sora)

## Community

Join the Opperator community to get help, share agents, and stay updated:

- **Discord** - Join our community: [discord.gg/wcT3bc7Y4Y](https://discord.gg/wcT3bc7Y4Y)
- **Email** - Reach the team: [support@opper.ai](mailto:support@opper.ai)
- **Twitter/X** - Follow us: [@opperai](https://x.com/opperai)
- **LinkedIn** - Connect: [Opper AI](https://linkedin.com/company/opper-ai)
- **GitHub Issues** - Report bugs or request features
- **GitHub Discussions** - Ask questions and share ideas

## Contributing

Contributions are welcome! We appreciate bug reports, feature requests, documentation improvements, and code contributions. **PRs are welcome** - feel free to submit pull requests for any improvements you'd like to see.
