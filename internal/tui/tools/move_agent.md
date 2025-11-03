Move an agent from a cloud/remote daemon to the local daemon.

This tool allows you to bring agents running on remote daemons back to your local environment for development or debugging.

**Important Constraints**:
- Only cloud-to-local moves are supported (cannot move from local to cloud)
- Secrets are NOT transferred during the move (you must manage secrets separately)
- Existing local agents will NOT be overwritten (operation fails if agent already exists locally)
- Agent will automatically start on local daemon after successful move
- The agent will be REMOVED from the source daemon after successful transfer

**Use Cases**:
- Pulling a production/cloud agent to local for debugging
- Retrieving an agent from a remote daemon for modification
- Testing agent behavior in local environment
- Bringing an agent back from cloud for development

**Parameters**:
- `agent_name` (required): Name of the agent to move from remote to local

**Example**:
To move an agent named "my-agent" from a cloud daemon to local:
```
move_agent(agent_name="my-agent")
```

**Security Notes**:
- This operation requires user permission approval
- The agent is deleted from the source daemon after successful move
- Secrets must be configured separately on the local daemon
- Virtual environments are automatically recreated after transfer
