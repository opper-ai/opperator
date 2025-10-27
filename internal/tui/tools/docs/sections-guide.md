# Custom Sidebar Sections

Agents create sections in the sidebar to
show dynamic information. Sections appear
alongside built-in ones like "Plan".

## SDK Methods

**Register a section:**
```python
self.register_section(
    section_id="status",
    title="Agent Status",
    content="Starting...",
    collapsed=False
)
```

**Update a section:**
```python
self.update_section(
    "status",
    "<b>State:</b> Running"
)
```

## Colors and Formatting

**Bold:** `<b>text</b>`
**Italic:** `<i>text</i>`
**Color:** `<c fg="color">text</c>`

**Named colors:**
green, red, blue, yellow, cyan,
magenta, white, black, gray

**Hex colors:**
`<c fg="#00ff00">bright green</c>`
`<c fg="#ff6b35">orange</c>`

**Background:**
`<c bg="yellow">highlight</c>`
`<c fg="black" bg="yellow">text</c>`

## ASCII Symbols

**Status indicators:**
```
● Active     ○ Inactive
◐ Starting   ◓ Paused
✓ Success    ✗ Failed
⚠ Warning
```

**Progress bars:**
```
█████████░░░░░░░░░ 45%
```

**Charts:**
```
Mon ████████░░ 8
Tue ███████████ 11
Wed █████░░░░░ 5
```

**Sparkline:**
```
▁▂▃▄▅▆▇█ (trend over time)
```

**Spinners:**
```
◐ ◓ ◑ ◒ (rotate through these)
```

## Animating Sections

Simulate animation by updating sections
with delays between each frame.

**Loading spinner example:**
```python
def show_loading_animation(self):
    frames = ["◐", "◓", "◑", "◒"]

    for i in range(20):  # 5 seconds
        frame = frames[i % 4]
        self.update_section(
            "status",
            f"<c fg='yellow'>{frame}</c> "
            f"Loading..."
        )
        time.sleep(0.25)

    # Final state
    self.update_section(
        "status",
        "<c fg='green'>✓</c> Ready!"
    )
```

**Progress bar animation:**
```python
def process_with_progress(self, items):
    total = len(items)

    for i, item in enumerate(items):
        # Process the item
        result = self.process_item(item)

        # Update progress
        percent = int((i+1)/total * 100)
        filled = int(20 * percent / 100)
        bar = "█"*filled + "░"*(20-filled)

        self.update_section(
            "progress",
            f"{bar} {percent}%\n"
            f"Item {i+1}/{total}"
        )

        # Small delay so user sees it
        time.sleep(0.1)
```

## Agent Lifecycle Pattern

Show different content as agent moves
through lifecycle stages.

**Stage 1: Initializing**
```python
def initialize(self):
    self.register_section(
        "status",
        "Agent Status",
        "<c fg='gray'>○ Initializing...</c>"
    )
```

**Stage 2: Starting (with animation)**
```python
def start(self):
    # Show starting animation
    frames = ["◐", "◓", "◑", "◒"]
    for i in range(8):
        self.update_section(
            "status",
            f"<c fg='yellow'>{frames[i%4]}</c> "
            f"Starting..."
        )
        time.sleep(0.3)

    # Now running
    self.update_section(
        "status",
        "<c fg='green'>● Running</c>"
    )
```

**Stage 3: Running (show activity)**
```python
def handle_request(self, data):
    # Update with current activity
    self.update_section(
        "status",
        f"""
<c fg='green'>● Running</c>
<b>Requests:</b> {self.count}
<b>Last:</b> {time.strftime('%H:%M:%S')}
        """.strip()
    )
```

**Stage 4: Shutting down**
```python
def on_shutdown(self):
    self.update_section(
        "status",
        "<c fg='yellow'>○ Stopping...</c>"
    )
```

## Best Practice: Overview + Dynamic

Use two sections - one stable overview,
one that changes with commands.

**Overview section (always visible):**
```python
def initialize(self):
    # Stable info about agent
    self.register_section(
        "overview",
        "Overview",
        f"""
<b>Agent:</b> Task Processor
<b>Version:</b> 1.0
<b>Status:</b> Ready
        """.strip()
    )

    # Dynamic command status
    self.register_section(
        "activity",
        "Current Activity",
        "Idle",
        collapsed=True  # Hidden by default
    )
```

**Update dynamic section per command:**
```python
def process_task_command(self, task_id):
    # Show what we're doing
    self.update_section(
        "activity",
        f"""
<c fg='yellow'>◐</c> Processing task
<b>ID:</b> {task_id}
<b>Started:</b> {time.strftime('%H:%M:%S')}
        """.strip()
    )

    # Do the work...
    result = self.process(task_id)

    # Show completion
    self.update_section(
        "activity",
        f"""
<c fg='green'>✓</c> Task completed
<b>ID:</b> {task_id}
<b>Duration:</b> {duration}s
<b>Result:</b> {result}
        """.strip()
    )

    # Update overview status
    self.update_section(
        "overview",
        f"""
<b>Agent:</b> Task Processor
<b>Version:</b> 1.0
<b>Status:</b> Ready
<b>Last Task:</b> {task_id}
        """.strip()
    )
```

## Complete Example

Full agent showing all patterns:

```python
class WebhookAgent(OpperatorAgent):
    def initialize(self):
        self.request_count = 0
        self.last_webhook = None

        # Overview (stable)
        self.register_section(
            "overview",
            "Webhook Agent",
            """
<b>Status:</b> Initializing
<b>Webhooks:</b> 0
            """.strip()
        )

        # Activity (dynamic)
        self.register_section(
            "activity",
            "Recent Activity",
            "No activity yet",
            collapsed=True
        )

    def start(self):
        # Animate startup
        for i, frame in enumerate(
            ["◐", "◓", "◑", "◒"] * 2
        ):
            self.update_section(
                "overview",
                f"""
<c fg='yellow'>{frame}</c> Starting...
<b>Webhooks:</b> 0
                """.strip()
            )
            time.sleep(0.25)

        # Mark as running
        self.update_section(
            "overview",
            """
<c fg='green'>● Running</c>
<b>Webhooks:</b> 0
            """.strip()
        )

    def handle_webhook(self, webhook_data):
        webhook_id = webhook_data['id']

        # Show processing
        self.update_section(
            "activity",
            f"""
<c fg='yellow'>◐</c> Processing
<b>ID:</b> {webhook_id}
            """.strip()
        )

        # Process it
        result = self.process(webhook_data)
        self.request_count += 1
        self.last_webhook = webhook_id

        # Show completion
        self.update_section(
            "activity",
            f"""
<c fg='green'>✓</c> Processed
<b>ID:</b> {webhook_id}
<b>Status:</b> {result['status']}
            """.strip()
        )

        # Update overview
        self.update_section(
            "overview",
            f"""
<c fg='green'>● Running</c>
<b>Webhooks:</b> {self.request_count}
<b>Last:</b> {webhook_id[:8]}...
            """.strip()
        )

    def on_shutdown(self):
        self.update_section(
            "overview",
            f"""
<c fg='yellow'>○ Stopping</c>
<b>Webhooks:</b> {self.request_count}
            """.strip()
        )
```

## Section Collapsed State

Sections remember if user collapsed them.

**Default:** Expanded (collapsed=False)
**User Control:** Click title to toggle
**Persistent:** Stays collapsed next time
**Per-Section:** Each has own state

**When to start collapsed:**
```python
# Debug info - less important
self.register_section(
    "debug",
    "Debug Info",
    content=details,
    collapsed=True
)

# Main status - important
self.register_section(
    "status",
    "Status",
    content=status,
    collapsed=False  # Default
)
```

## Section ID Guidelines

Use descriptive, consistent IDs.

**Good IDs:**
- `status`, `overview`, `activity`
- `queue_status`, `task_progress`
- `recent_errors`, `config`

**Bad IDs:**
- `section1` (not descriptive)
- `Agent Status` (has space)
- `status!!!` (special chars)

**Rules:**
- Lowercase with underscores
- No spaces or special characters
- Same ID = same section updates
- Different agents can use same IDs

## Content Guidelines

**Length:** 5-10 lines max per section
**Format:** Use bullets, bold labels
**Order:** Most important info first
**Colors:** Highlight key information

**Update frequency:**
- On significant changes (status, event)
- Not every second (too noisy)
- Batch related updates together
- Use delays between animation frames

**Label pattern:**
```
<b>Label:</b> value
<b>Status:</b> <c fg="green">Running</c>
<b>Count:</b> 42
```

**String formatting:**
Use \n for newlines. All formats work:
- Single-line: `"line1\nline2\nline3"`
- Concatenation: `("line1\n" "line2\n")`
- Triple-quote: `"""...""".strip()`

Newlines render properly in the sidebar.

## When to Use Sections

**Good for:**
- Agent status (running, idle, error)
- Progress of long operations
- Recent activity (last 5 items)
- Important warnings/alerts
- Live metrics (request count)
- Overview of agent state

**Avoid for:**
- Static info (use docs instead)
- Very long content (no scrolling)
- Duplicate of log messages
- Info that rarely changes

## Troubleshooting

**Section not appearing?**
- Agent must be running and focused
- Section ID must be unique
- Content cannot be empty
- Check agent logs for errors

**Not updating?**
- Section ID must match exactly
- Check agent is emitting updates
- Look for exceptions in logs
- Try re-registering section

**Colors not working?**
- Use quotes: `<c fg="green">text</c>`
- Named: green, red, blue, yellow
- Hex: #RRGGBB format
- Check closing tags: `</c>`

## Summary

**Two SDK methods:**
- `self.register_section()` - first time
- `self.update_section()` - updates

**Animation pattern:**
- Update section in loop
- Add small delay (0.1-0.5s)
- Show frame progression
- End with final state

**Best practice:**
- Overview section (stable info)
- Activity section (dynamic)
- Show status across lifecycle
- Use colors and symbols
- Keep content 5-10 lines

**Lifecycle stages:**
1. Initialize (gray, ○ Initializing)
2. Start (yellow, ◐ Starting)
3. Running (green, ● Running)
4. Shutdown (yellow, ○ Stopping)

Sections give users visibility without
checking logs or running commands.
