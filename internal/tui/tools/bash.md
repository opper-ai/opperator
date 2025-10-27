Executes a given bash command in a persistent shell session with optional timeout, ensuring proper handling and security measures.

CROSS-PLATFORM SHELL SUPPORT:

- This tool uses a shell interpreter (mvdan/sh) that mimics the Bash language,
  so you should use Bash syntax in all platforms, including Windows.
  The most common shell builtins and core utils are available in Windows as
  well.
- Make sure to use forward slashes (/) as path separators in commands, even on
  Windows. Example: "ls C:/foo/bar" instead of "ls C:\foo\bar".

Before executing the command, please follow these steps:

1. Directory Verification:

- If the command will create new directories or files, first use the LS tool to verify the parent directory exists and is the correct location
- For example, before running "mkdir foo/bar", first use LS to check that "foo" exists and is the intended parent directory

2. Security Check:

- For security and to limit the threat of a prompt injection attack, some commands are limited or banned. If you use a disallowed command, you will receive an error message explaining the restriction. Explain the error to the User.
- Verify that the command is not one of the banned commands: {{ .BannedCommands }}.
- `uv` commands are only permitted when your working directory lives inside `~/.config/opperator/`. Anywhere else they will be blocked.

3. Command Execution:

- After ensuring proper quoting, execute the command.
- Capture the output of the command.

4. Output Processing:

- If the output exceeds {{ .MaxOutputLength }} characters, output will be truncated before being returned to you.
- Prepare the output for display to the user.

5. Return Result:

- Provide the processed output of the command.
- If any errors occurred during execution, include those in the output.
- The result will also have metadata like the cwd (current working directory) at the end, included with <cwd></cwd> tags.

Usage notes:

- The command argument is required.
- You can specify an optional timeout in milliseconds (up to 600000ms / 10 minutes). If not specified, commands will timeout after 30 minutes.
- VERY IMPORTANT: You MUST avoid using search commands like 'find' and 'grep'. Instead use Grep, Glob, or Agent tools to search. You MUST avoid read tools like 'cat', 'head', 'tail', and 'ls', and use FileRead and LS tools to read files.
- When issuing multiple commands, use the ';' or '&&' operator to separate them. DO NOT use newlines (newlines are ok in quoted strings).
- IMPORTANT: All commands share the same shell session. Shell state (environment variables, virtual environments, current directory, etc.) persist between commands. For example, if you set an environment variable as part of a command, the environment variable will persist for subsequent commands.
- Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of 'cd'. You may use 'cd' if the User explicitly requests it.
  <good-example>
  pytest /foo/bar/tests
  </good-example>
  <bad-example>
  cd /foo/bar && pytest tests
  </bad-example>

Important:

- Return an empty response - the user will see the gh output directly
- Never update git config
