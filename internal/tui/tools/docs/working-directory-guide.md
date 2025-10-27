# Working Directory

Commands receive working directory context
from the LLM. Use `self.get_working_directory()`
to access it in command handlers.

## Getting Working Directory

**In command handlers:**
```python
def _cmd_list_files(self, args):
    # Get current working directory
    working_dir = self.get_working_directory()

    # List files in working directory
    files = os.listdir(working_dir)

    return {
        "working_directory": working_dir,
        "files": files,
        "count": len(files)
    }
```

**Returns:**
- Absolute path to working directory (string)
- Falls back to `os.getcwd()` if no context set

## Resolving Relative Paths

**Make relative paths absolute:**
```python
def _cmd_read_file(self, args):
    file_path = args["file_path"]
    working_dir = self.get_working_directory()

    # Resolve relative paths
    if not os.path.isabs(file_path):
        file_path = os.path.join(working_dir, file_path)

    # Now file_path is absolute
    with open(file_path) as f:
        return f.read()
```

**Using Path objects:**
```python
from pathlib import Path

def _cmd_find_python_files(self, args):
    working_dir = Path(self.get_working_directory())

    # Find all Python files
    python_files = list(working_dir.glob("**/*.py"))

    return {
        "working_directory": str(working_dir),
        "python_files": [str(f) for f in python_files],
        "count": len(python_files)
    }
```

## When to Use

**File operations:**
```python
def _cmd_analyze_project(self, args):
    working_dir = self.get_working_directory()

    # Scan project files
    project_files = []
    for root, dirs, files in os.walk(working_dir):
        for file in files:
            if file.endswith((".py", ".js", ".ts")):
                project_files.append(os.path.join(root, file))

    return {
        "project_root": working_dir,
        "files_found": len(project_files)
    }
```

**Relative path arguments:**
```python
from opperator import CommandArgument

def initialize(self):
    self.register_command(
        "process_file",
        self._cmd_process_file,
        arguments=[
            CommandArgument(
                name="file_path",
                type="string",
                description="Path to file (relative or absolute)",
                required=True
            )
        ]
    )

def _cmd_process_file(self, args):
    file_path = args["file_path"]
    working_dir = self.get_working_directory()

    # Handle both relative and absolute paths
    if not os.path.isabs(file_path):
        file_path = os.path.join(working_dir, file_path)

    # Process file
    with open(file_path) as f:
        data = f.read()

    return {"processed": file_path}
```

**Project context:**
```python
def _cmd_get_project_info(self, args):
    working_dir = Path(self.get_working_directory())

    # Check for project markers
    info = {
        "directory": str(working_dir),
        "name": working_dir.name,
        "has_git": (working_dir / ".git").exists(),
        "has_package_json": (working_dir / "package.json").exists(),
        "has_requirements_txt": (working_dir / "requirements.txt").exists(),
    }

    return info
```

## Working Directory Changes

**Detect directory changes:**
```python
def on_working_directory_changed(self, old_path: str, new_path: str):
    """Called when working directory changes"""
    self.log(
        LogLevel.INFO,
        "Working directory changed",
        old=old_path,
        new=new_path
    )

    # Reload project-specific configuration
    self.load_project_config(new_path)
```

**React to new project:**
```python
def on_working_directory_changed(self, old_path: str, new_path: str):
    # Scan for project files
    python_files = list(Path(new_path).glob("**/*.py"))
    js_files = list(Path(new_path).glob("**/*.js"))

    # Update system prompt with context
    self.set_system_prompt(f"""
Code analysis agent.

Working directory: {os.path.basename(new_path)}
Python files: {len(python_files)}
JavaScript files: {len(js_files)}
    """)

    # Update sidebar
    self.update_section(
        "project",
        f"""
<b>Directory:</b> {os.path.basename(new_path)}
<b>Python:</b> {len(python_files)} files
<b>JavaScript:</b> {len(js_files)} files
        """.strip()
    )
```

## Path Validation

**Check if path exists:**
```python
def _cmd_read_file(self, args):
    file_path = args["file_path"]
    working_dir = self.get_working_directory()

    # Resolve path
    if not os.path.isabs(file_path):
        file_path = os.path.join(working_dir, file_path)

    # Validate path exists
    if not os.path.exists(file_path):
        raise ValueError(f"File not found: {file_path}")

    # Validate it's a file
    if not os.path.isfile(file_path):
        raise ValueError(f"Path is not a file: {file_path}")

    # Read file
    with open(file_path) as f:
        return f.read()
```

**Stay within working directory:**
```python
def _cmd_read_file(self, args):
    file_path = args["file_path"]
    working_dir = self.get_working_directory()

    # Resolve path
    if not os.path.isabs(file_path):
        file_path = os.path.join(working_dir, file_path)

    # Normalize to resolve .. and .
    file_path = os.path.abspath(file_path)

    # Ensure path is within working directory
    if not file_path.startswith(working_dir):
        raise ValueError(
            f"Path {file_path} is outside working directory"
        )

    with open(file_path) as f:
        return f.read()
```

## Common Patterns

**Search in working directory:**
```python
def _cmd_search_files(self, args):
    pattern = args["pattern"]
    working_dir = Path(self.get_working_directory())

    matches = []

    # Search all files
    for file_path in working_dir.rglob("*"):
        if file_path.is_file():
            try:
                with open(file_path) as f:
                    content = f.read()
                    if pattern in content:
                        matches.append(str(file_path))
            except (PermissionError, UnicodeDecodeError):
                # Skip files we can't read
                continue

    return {
        "pattern": pattern,
        "matches": matches,
        "count": len(matches)
    }
```

**Git repository operations:**
```python
def _cmd_git_status(self, args):
    working_dir = self.get_working_directory()

    # Check if in git repo
    git_dir = os.path.join(working_dir, ".git")
    if not os.path.exists(git_dir):
        return {"error": "Not a git repository"}

    # Run git status
    import subprocess
    result = subprocess.run(
        ["git", "status", "--short"],
        cwd=working_dir,
        capture_output=True,
        text=True
    )

    return {
        "working_directory": working_dir,
        "status": result.stdout,
        "modified_files": len(result.stdout.strip().split("\n"))
    }
```

**Project configuration:**
```python
def load_project_config(self):
    """Load project-specific configuration"""
    working_dir = self.get_working_directory()
    config_file = os.path.join(working_dir, ".agent-config.json")

    if os.path.exists(config_file):
        with open(config_file) as f:
            project_config = json.load(f)

        self.log(
            LogLevel.INFO,
            "Loaded project config",
            path=config_file
        )

        return project_config

    return None
```

## Complete Example

Agent using working directory context:

```python
from opperator import OpperatorAgent, LogLevel, CommandArgument
from pathlib import Path
import os
from typing import Dict, Any

class FileAgent(OpperatorAgent):
    def initialize(self):
        self.set_description("File operations agent")

        # List files command
        self.register_command(
            "list_files",
            self._cmd_list_files,
            description="List files in working directory",
        )

        # Read file command
        self.register_command(
            "read_file",
            self._cmd_read_file,
            description="Read a file",
            arguments=[
                CommandArgument(
                    name="file_path",
                    type="string",
                    description="Path to file (relative or absolute)",
                    required=True
                )
            ]
        )

        # Search command
        self.register_command(
            "search",
            self._cmd_search,
            description="Search for text in files",
            arguments=[
                CommandArgument(
                    name="pattern",
                    type="string",
                    description="Text to search for",
                    required=True
                ),
                CommandArgument(
                    name="file_pattern",
                    type="string",
                    description="File pattern to search (e.g. *.py)",
                    default="*"
                )
            ]
        )

        self.register_section("context", "Working Directory", "Not set")

    def start(self):
        # Show initial working directory
        working_dir = self.get_working_directory()
        self.update_context(working_dir)

    def _cmd_list_files(self, args: Dict[str, Any]):
        """List files in working directory"""
        working_dir = Path(self.get_working_directory())

        # List all files
        files = []
        for item in working_dir.iterdir():
            files.append({
                "name": item.name,
                "type": "dir" if item.is_dir() else "file",
                "size": item.stat().st_size if item.is_file() else 0
            })

        # Sort by name
        files.sort(key=lambda x: x["name"])

        return {
            "working_directory": str(working_dir),
            "files": files,
            "count": len(files)
        }

    def _cmd_read_file(self, args: Dict[str, Any]):
        """Read a file from working directory"""
        file_path = args["file_path"]
        working_dir = self.get_working_directory()

        # Resolve relative paths
        if not os.path.isabs(file_path):
            file_path = os.path.join(working_dir, file_path)

        # Validate file exists
        if not os.path.exists(file_path):
            raise ValueError(f"File not found: {file_path}")

        if not os.path.isfile(file_path):
            raise ValueError(f"Not a file: {file_path}")

        # Read file
        try:
            with open(file_path) as f:
                content = f.read()
        except UnicodeDecodeError:
            return {"error": "File is not text (binary file)"}

        return {
            "file_path": file_path,
            "content": content,
            "size": len(content)
        }

    def _cmd_search(self, args: Dict[str, Any]):
        """Search for text in files"""
        pattern = args["pattern"]
        file_pattern = args["file_pattern"]
        working_dir = Path(self.get_working_directory())

        matches = []

        # Search matching files
        for file_path in working_dir.glob(f"**/{file_pattern}"):
            if not file_path.is_file():
                continue

            try:
                with open(file_path) as f:
                    for line_num, line in enumerate(f, 1):
                        if pattern in line:
                            matches.append({
                                "file": str(file_path.relative_to(working_dir)),
                                "line": line_num,
                                "text": line.strip()
                            })
            except (PermissionError, UnicodeDecodeError):
                # Skip files we can't read
                continue

        return {
            "pattern": pattern,
            "file_pattern": file_pattern,
            "matches": matches,
            "count": len(matches)
        }

    def update_context(self, working_dir: str):
        """Update sidebar with working directory info"""
        path = Path(working_dir)

        # Count files
        file_count = sum(1 for _ in path.glob("*") if _.is_file())
        dir_count = sum(1 for _ in path.glob("*") if _.is_dir())

        self.update_section(
            "context",
            f"""
<b>Directory:</b> {path.name}
<b>Path:</b> {str(path)}
<b>Files:</b> {file_count}
<b>Subdirs:</b> {dir_count}
            """.strip()
        )

    def on_working_directory_changed(self, old_path: str, new_path: str):
        """Update context when directory changes"""
        self.log(
            LogLevel.INFO,
            "Working directory changed",
            old=old_path,
            new=new_path
        )

        self.update_context(new_path)
```

## Best Practices

**Always resolve relative paths:**
```python
# Good
def _cmd_read(self, args):
    path = args["path"]
    if not os.path.isabs(path):
        path = os.path.join(self.get_working_directory(), path)

# Bad
def _cmd_read(self, args):
    path = args["path"]  # Might be relative!
    with open(path) as f:  # Could fail or read wrong file
        return f.read()
```

**Use Path objects:**
```python
# Good - cleaner path operations
from pathlib import Path

working_dir = Path(self.get_working_directory())
file_path = working_dir / "subdir" / "file.txt"

# Bad - manual string concatenation
working_dir = self.get_working_directory()
file_path = working_dir + "/subdir/file.txt"  # Wrong on Windows!
```

**Validate paths:**
```python
# Good - check before using
if not os.path.exists(file_path):
    raise ValueError(f"File not found: {file_path}")

# Bad - let it crash
with open(file_path) as f:  # Cryptic error if doesn't exist
    return f.read()
```

## Summary

**Get working directory:**
```python
working_dir = self.get_working_directory()
```

**Resolve relative paths:**
```python
if not os.path.isabs(path):
    path = os.path.join(working_dir, path)
```

**React to changes:**
```python
def on_working_directory_changed(self, old_path, new_path):
    # Handle directory change
    self.load_project_config(new_path)
```

**Common uses:**
- File operations (read, write, search)
- Project context (detect git, package.json, etc.)
- Relative path resolution
- Project-specific configuration

Working directory provides context about
where the user is working in the filesystem.
