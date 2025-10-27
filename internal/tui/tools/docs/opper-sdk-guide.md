# Building AI with Opper SDK

Use the Opper SDK to add AI capabilities to your agents.
This guide shows how to make structured AI calls, stream
responses, and build intelligent features.

## Installation

```bash
pip install opperai
```

## Basic Setup

**Initialize the Opper client:**
```python
from opperai import Opper

class MyAgent(OpperatorAgent):
    def initialize(self):
        # Get API key from secrets
        api_key = self.get_secret("OPPER_API_KEY")

        # Initialize Opper client
        self.opper = Opper(http_bearer=api_key)

        self.log(LogLevel.INFO, "Opper SDK initialized")
```

## Making AI Calls

**Basic structured call:**
```python
from pydantic import BaseModel

class TaskSummary(BaseModel):
    title: str
    priority: str
    estimated_hours: int
    tags: list[str]

def analyze_task(self, task_description: str):
    completion = self.opper.call(
        name="analyze_task",
        instructions="Extract task details from description",
        input=task_description,
        output_schema=TaskSummary
    )

    result = completion.json_payload

    self.log(
        LogLevel.INFO,
        "Task analyzed",
        title=result.title,
        priority=result.priority
    )

    return result
```

**Text-only call (no schema):**
```python
def generate_response(self, prompt: str):
    completion = self.opper.call(
        name="generate_text",
        instructions="Provide a helpful response",
        input=prompt
    )

    # Access text from message
    return completion.message
```

**Call with context:**
```python
def classify_email(self, email_text: str, user_context: dict):
    completion = self.opper.call(
        name="classify_email",
        instructions="""
        Classify this email based on the user's preferences.
        Return category and suggested action.
        """,
        input={
            "email": email_text,
            "user_prefs": user_context
        },
        output_schema=EmailClassification
    )

    return completion.json_payload
```

## Streaming Responses

**Stream text incrementally:**
```python
def generate_story(self, prompt: str):
    self.log(LogLevel.INFO, "Starting story generation")

    # Start streaming
    outer = self.opper.stream(
        name="story_generator",
        instructions="Write a creative short story",
        input=prompt
    )

    # Extract event stream
    stream = next(value for key, value in outer
                  if key == "result")

    # Process events
    full_text = ""
    for event in stream:
        delta = getattr(event.data, "delta", None)
        if delta:
            full_text += delta
            # Update UI or send to client
            self.update_section("output", delta)

    self.log(
        LogLevel.INFO,
        "Story generated",
        length=len(full_text)
    )

    return full_text
```

**Stream structured data:**
```python
from pydantic import BaseModel

class Report(BaseModel):
    summary: str
    key_points: list[str]
    conclusion: str

def generate_report(self, data: str):
    outer = self.opper.stream(
        name="report_generator",
        instructions="Generate structured report",
        input=data,
        output_schema=Report
    )

    stream = next(value for key, value in outer
                  if key == "result")

    for event in stream:
        # Track which field is being populated
        json_path = getattr(event.data, "json_path", None)
        delta = getattr(event.data, "delta", None)

        if json_path and delta:
            self.log(
                LogLevel.DEBUG,
                "Streaming field",
                path=json_path,
                content=delta
            )
```

**Context manager for streaming:**
```python
def stream_with_cleanup(self, prompt: str):
    with self.opper.stream(
        name="generator",
        instructions="Generate response",
        input=prompt
    ) as event_stream:
        for event in event_stream:
            delta = getattr(event.data, "delta", None)
            if delta:
                print(delta, end="", flush=True)

    # Automatic cleanup after context exits
```

## Async Operations

**Async function calls:**
```python
async def process_batch(self, items: list[str]):
    self.log(
        LogLevel.INFO,
        "Processing batch",
        count=len(items)
    )

    # Process items concurrently
    tasks = []
    for item in items:
        task = self.opper.call_async(
            name="process_item",
            instructions="Process this item",
            input=item,
            output_schema=ProcessedItem
        )
        tasks.append(task)

    # Wait for all completions
    results = await asyncio.gather(*tasks)

    return [r.json_payload for r in results]
```

**Async with error handling:**
```python
async def safe_call(self, input_data: str):
    try:
        result = await self.opper.call_async(
            name="processor",
            instructions="Process input",
            input=input_data,
            output_schema=Output
        )
        return result.json_payload
    except Exception as exc:
        self.log(
            LogLevel.ERROR,
            "Async call failed",
            error=str(exc)
        )
        return None
```

## Registering Commands with AI

**AI-powered command:**
```python
def initialize(self):
    # Setup Opper
    api_key = self.get_secret("OPPER_API_KEY")
    self.opper = Opper(http_bearer=api_key)

    # Register AI command
    self.register_command(
        name="analyze",
        description="Analyze text with AI",
        arguments=[
            ("text", "string", "Text to analyze")
        ],
        handler=self.handle_analyze,
        type="agent_tool"
    )

def handle_analyze(self, text: str):
    """Command handler using Opper"""

    self.log(LogLevel.INFO, "Analyzing text")

    # Define schema
    class Analysis(BaseModel):
        sentiment: str
        topics: list[str]
        summary: str

    # Make AI call
    result = self.opper.call(
        name="text_analyzer",
        instructions="Analyze sentiment, topics, and summarize",
        input=text,
        output_schema=Analysis
    )

    analysis = result.json_payload

    # Return structured result
    return {
        "sentiment": analysis.sentiment,
        "topics": analysis.topics,
        "summary": analysis.summary
    }
```

**Streaming command:**
```python
def initialize(self):
    self.register_command(
        name="generate",
        description="Generate content with AI",
        arguments=[
            ("prompt", "string", "Generation prompt")
        ],
        handler=self.handle_generate,
        type="agent_tool"
    )

def handle_generate(self, prompt: str):
    """Streaming command handler"""

    self.log(LogLevel.INFO, "Starting generation")

    outer = self.opper.stream(
        name="content_generator",
        instructions="Generate creative content",
        input=prompt
    )

    stream = next(value for key, value in outer
                  if key == "result")

    chunks = []
    for event in stream:
        delta = getattr(event.data, "delta", None)
        if delta:
            chunks.append(delta)
            # Show progress
            self.update_section(
                "status",
                f"Generated {len(''.join(chunks))} chars..."
            )

    full_text = "".join(chunks)

    return {
        "content": full_text,
        "length": len(full_text)
    }
```

## Error Handling

**Handle API errors:**
```python
from opperai.core import ApiError

def safe_call(self, input_text: str):
    try:
        result = self.opper.call(
            name="processor",
            instructions="Process input",
            input=input_text
        )
        return result.message

    except ApiError as exc:
        self.log(
            LogLevel.ERROR,
            "Opper API error",
            error=str(exc),
            status_code=exc.status_code
        )
        return None

    except Exception as exc:
        self.log(
            LogLevel.ERROR,
            "Unexpected error",
            error=str(exc)
        )
        return None
```

**Retry logic:**
```python
def call_with_retry(self, input_data: str, max_retries: int = 3):
    for attempt in range(max_retries):
        try:
            result = self.opper.call(
                name="processor",
                instructions="Process data",
                input=input_data
            )
            return result.message

        except ApiError as exc:
            self.log(
                LogLevel.WARNING,
                "Call failed, retrying",
                attempt=attempt + 1,
                error=str(exc)
            )

            if attempt < max_retries - 1:
                time.sleep(2 ** attempt)
            else:
                self.log(LogLevel.ERROR, "All retries exhausted")
                raise
```

## Common Patterns

**Data extraction:**
```python
class ExtractedData(BaseModel):
    name: str
    email: str
    phone: str
    company: str

def extract_contact_info(self, text: str):
    """Extract structured data from unstructured text"""

    result = self.opper.call(
        name="contact_extractor",
        instructions="Extract contact information",
        input=text,
        output_schema=ExtractedData
    )

    return result.json_payload
```

**Classification:**
```python
class Classification(BaseModel):
    category: str
    confidence: float
    reasoning: str

def classify_document(self, document: str):
    """Classify document into categories"""

    result = self.opper.call(
        name="document_classifier",
        instructions="""
        Classify this document into one of:
        - technical, business, legal, personal
        Provide confidence score and reasoning.
        """,
        input=document,
        output_schema=Classification
    )

    return result.json_payload
```

**Text transformation:**
```python
def summarize(self, text: str, max_length: int = 100):
    """Summarize long text"""

    result = self.opper.call(
        name="summarizer",
        instructions=f"Summarize in {max_length} words or less",
        input=text
    )

    return result.message
```

**Question answering:**
```python
def answer_question(self, question: str, context: str):
    """Answer questions about provided context"""

    result = self.opper.call(
        name="qa_system",
        instructions="Answer based only on the provided context",
        input={
            "question": question,
            "context": context
        }
    )

    return result.message
```

## Complete Example

**AI-powered task agent:**
```python
from opperai import Opper
from pydantic import BaseModel
from opperator import OpperatorAgent, LogLevel

class TaskAnalysis(BaseModel):
    title: str
    priority: str
    estimated_hours: int
    tags: list[str]
    next_steps: list[str]

class TaskAgent(OpperatorAgent):
    def initialize(self):
        self.log(LogLevel.INFO, "Initializing task agent")

        # Setup Opper SDK
        api_key = self.get_secret("OPPER_API_KEY")
        self.opper = Opper(http_bearer=api_key)

        # Register AI commands
        self.register_command(
            name="analyze_task",
            description="Analyze task description with AI",
            arguments=[
                ("description", "string", "Task description")
            ],
            handler=self.analyze_task,
            type="agent_tool"
        )

        self.register_command(
            name="generate_plan",
            description="Generate project plan",
            arguments=[
                ("requirements", "string", "Project requirements")
            ],
            handler=self.generate_plan,
            type="agent_tool"
        )

        # Setup UI
        self.register_section(
            name="status",
            title="AI Status",
            content="Ready"
        )

        self.set_system_prompt(
            "Task analysis agent powered by Opper AI. "
            "Use analyze_task to extract structured task info. "
            "Use generate_plan to create project plans."
        )

    def start(self):
        self.log(LogLevel.INFO, "Task agent ready")
        self.update_section("status", "🤖 Ready for AI tasks")

    def analyze_task(self, description: str):
        """Analyze task with AI"""

        self.log(
            LogLevel.INFO,
            "Analyzing task",
            length=len(description)
        )

        self.update_section("status", "🔄 Analyzing...")

        try:
            # Make structured AI call
            result = self.opper.call(
                name="task_analyzer",
                instructions="""
                Analyze this task description and extract:
                - Clear title
                - Priority (high/medium/low)
                - Estimated hours
                - Relevant tags
                - Next steps
                """,
                input=description,
                output_schema=TaskAnalysis
            )

            analysis = result.json_payload

            self.log(
                LogLevel.INFO,
                "Task analyzed",
                title=analysis.title,
                priority=analysis.priority,
                hours=analysis.estimated_hours
            )

            self.update_section(
                "status",
                f"✅ Analyzed: {analysis.title}"
            )

            return {
                "title": analysis.title,
                "priority": analysis.priority,
                "estimated_hours": analysis.estimated_hours,
                "tags": analysis.tags,
                "next_steps": analysis.next_steps
            }

        except Exception as exc:
            self.log(
                LogLevel.ERROR,
                "Analysis failed",
                error=str(exc)
            )
            self.update_section("status", "❌ Analysis failed")
            raise

    def generate_plan(self, requirements: str):
        """Generate project plan with streaming"""

        self.log(LogLevel.INFO, "Generating plan")
        self.update_section("status", "🔄 Generating plan...")

        try:
            # Stream plan generation
            outer = self.opper.stream(
                name="plan_generator",
                instructions="""
                Create a detailed project plan with:
                - Overview
                - Phases and milestones
                - Resource requirements
                - Timeline
                - Risks and mitigation
                """,
                input=requirements
            )

            stream = next(value for key, value in outer
                         if key == "result")

            # Collect streamed content
            plan_text = ""
            for event in stream:
                delta = getattr(event.data, "delta", None)
                if delta:
                    plan_text += delta
                    # Update progress
                    self.update_section(
                        "status",
                        f"📝 Generated {len(plan_text)} chars..."
                    )

            self.log(
                LogLevel.INFO,
                "Plan generated",
                length=len(plan_text)
            )

            self.update_section("status", "✅ Plan complete")

            return {
                "plan": plan_text,
                "word_count": len(plan_text.split())
            }

        except Exception as exc:
            self.log(
                LogLevel.ERROR,
                "Plan generation failed",
                error=str(exc)
            )
            self.update_section("status", "❌ Generation failed")
            raise

    def on_shutdown(self):
        self.log(LogLevel.INFO, "Shutting down task agent")
        self.update_section("status", "💤 Offline")

if __name__ == "__main__":
    agent = TaskAgent(
        name="task_agent",
        version="1.0.0"
    )
    agent.run()
```

## Best Practices

**Use structured schemas:**
```python
# Good: Pydantic schema for validation
class Output(BaseModel):
    field1: str
    field2: int

result = opper.call(..., output_schema=Output)
validated = result.json_payload

# Bad: Unstructured text parsing
result = opper.call(..., output_schema=None)
# Now you have to parse result.message manually
```

**Keep instructions clear:**
```python
# Good: Specific, actionable instructions
instructions = """
Extract customer name, email, and order ID.
Return empty string if field not found.
"""

# Bad: Vague instructions
instructions = "Process this data"
```

**Handle streaming properly:**
```python
# Good: Extract stream correctly
outer = opper.stream(...)
stream = next(value for key, value in outer
              if key == "result")

for event in stream:
    delta = getattr(event.data, "delta", None)
    if delta:
        process(delta)

# Bad: Assuming structure
for event in opper.stream(...):
    print(event.delta)  # May not exist
```

**Cache Opper client:**
```python
# Good: Reuse client
def initialize(self):
    self.opper = Opper(http_bearer=api_key)

def process(self, data):
    self.opper.call(...)  # Reuse

# Bad: Create client each time
def process(self, data):
    opper = Opper(http_bearer=api_key)  # Wasteful
    opper.call(...)
```

## Troubleshooting

**API key errors:**
```python
# Verify key is set
api_key = self.get_secret("OPPER_API_KEY")
if not api_key:
    self.log(LogLevel.FATAL, "OPPER_API_KEY not found")
    sys.exit(1)

# Check key format
if not api_key.startswith("opper-"):
    self.log(LogLevel.WARNING, "API key format unexpected")
```

**Schema validation errors:**
```python
# Ensure schema matches expected output
class MySchema(BaseModel):
    # Use optional fields if data might be missing
    name: str
    age: int | None = None  # Optional with default
    tags: list[str] = []    # Optional list
```

**Streaming issues:**
```python
# Always extract stream first
outer = opper.stream(...)
stream = next(value for key, value in outer
              if key == "result")

# Check for delta before using
for event in stream:
    delta = getattr(event.data, "delta", None)
    if delta:
        # Safe to use delta
        process(delta)
```

## Summary

**Key Opper SDK methods:**
- `opper.call()` - Structured AI calls
- `opper.stream()` - Real-time streaming
- `opper.call_async()` - Async operations

**Common patterns:**
- Data extraction with schemas
- Text classification
- Content generation
- Question answering
- Summarization

**Best practices:**
- Use Pydantic schemas for validation
- Reuse Opper client instance
- Handle errors gracefully
- Keep instructions clear and specific
- Extract streams properly

Build intelligent agents by combining
Opperator's framework with Opper's AI SDK.
