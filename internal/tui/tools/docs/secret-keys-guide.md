# Secret Keys

Agents fetch secrets from Opperator's secure
keyring using `self.get_secret()`. Secrets are
encrypted and stored securely.

## Fetching Secrets

**Basic usage:**
```python
from opperator import SecretError

def initialize(self):
    try:
        api_key = self.get_secret("api_key", timeout=5.0)
        # Use the key immediately
        self.client.authenticate(api_key)
        # Don't store it!
    except SecretError as e:
        self.log(LogLevel.ERROR, "Failed to fetch secret", error=str(e))
```

**Parameters:**
- `name` - Secret name (string, required)
- `timeout` - Timeout in seconds (float, default: 5.0)

**Returns:**
- Secret value as string

**Raises:**
- `SecretError` - If secret not found or fetch fails

## Security Rules

**Never log secrets:**
```python
# ❌ BAD - Logs the secret!
api_key = self.get_secret("api_key")
self.log(LogLevel.INFO, "Got key", key=api_key)

# ✓ GOOD - Logs only that it succeeded
api_key = self.get_secret("api_key")
self.log(LogLevel.INFO, "API key retrieved")
```

**Never store secrets:**
```python
# ❌ BAD - Stores secret in memory
def initialize(self):
    self.api_key = self.get_secret("api_key")

# ✓ GOOD - Fetch only when needed
def make_request(self):
    api_key = self.get_secret("api_key")
    response = requests.get(url, headers={"Authorization": api_key})
    # api_key is garbage collected after function returns
```

**Use immediately and discard:**
```python
def authenticate(self):
    # Fetch secret
    password = self.get_secret("db_password")

    # Use immediately
    conn = database.connect(
        host="localhost",
        user="admin",
        password=password
    )

    # Don't keep password around
    # (Python will garbage collect it)
    return conn
```

## Error Handling

**Handle missing secrets gracefully:**
```python
def initialize(self):
    try:
        api_key = self.get_secret("api_key", timeout=5.0)
        self.client = APIClient(api_key)
        self.authenticated = True
    except SecretError as e:
        self.log(
            LogLevel.WARNING,
            "API key not configured",
            error=str(e)
        )
        self.authenticated = False
```

**Require secret or fail:**
```python
def initialize(self):
    try:
        api_key = self.get_secret("api_key")
    except SecretError:
        self.log(
            LogLevel.FATAL,
            "API key required but not configured"
        )
        raise ValueError("api_key secret is required")

    self.client = APIClient(api_key)
```

**Retry on timeout:**
```python
def get_api_key_with_retry(self):
    for attempt in range(3):
        try:
            return self.get_secret("api_key", timeout=5.0)
        except SecretError as e:
            if attempt < 2:
                self.log(
                    LogLevel.WARNING,
                    "Secret fetch failed, retrying",
                    attempt=attempt + 1,
                    error=str(e)
                )
                time.sleep(1)
            else:
                raise
```

## Creating Secrets

Secrets are created via CLI or daemon IPC.

**Via CLI (recommended for users):**
```bash
# Interactive prompt (secure)
opperator secret create --name api_key

# From stdin
echo "secret-value" | opperator secret create --name api_key

# From file
cat secret.txt | opperator secret create --name api_key
```

**Via IPC (programmatic):**
```python
# Agents typically don't create secrets themselves,
# but can if needed via IPC client
```

## Common Patterns

**API authentication:**
```python
def initialize(self):
    self.client = None

def ensure_authenticated(self):
    """Authenticate if not already authenticated"""
    if self.client is not None:
        return

    try:
        api_key = self.get_secret("api_key")
        self.client = APIClient(api_key)
        self.log(LogLevel.INFO, "API client authenticated")
    except SecretError as e:
        self.log(LogLevel.ERROR, "Authentication failed", error=str(e))
        raise

def make_request(self, endpoint):
    self.ensure_authenticated()
    return self.client.get(endpoint)
```

**Database connection:**
```python
def connect_database(self):
    """Connect to database using credentials from secrets"""
    try:
        db_user = self.get_secret("db_user")
        db_password = self.get_secret("db_password")

        self.db = database.connect(
            host=self.config.get("db_host", "localhost"),
            user=db_user,
            password=db_password,
            database=self.config.get("db_name")
        )

        self.log(LogLevel.INFO, "Database connected")

    except SecretError as e:
        self.log(
            LogLevel.ERROR,
            "Database credentials not configured",
            error=str(e)
        )
        raise
```

**Multiple secrets:**
```python
def load_credentials(self):
    """Load multiple credentials at once"""
    credentials = {}

    required_secrets = ["api_key", "webhook_secret", "signing_key"]

    for secret_name in required_secrets:
        try:
            credentials[secret_name] = self.get_secret(secret_name)
        except SecretError as e:
            self.log(
                LogLevel.ERROR,
                f"Missing required secret: {secret_name}",
                error=str(e)
            )
            raise ValueError(f"Required secret '{secret_name}' not configured")

    # Use credentials immediately
    self.setup_with_credentials(credentials)

    # credentials dict will be garbage collected
    return True
```

**Optional secrets:**
```python
def initialize(self):
    # Required secret
    try:
        api_key = self.get_secret("api_key")
        self.client = APIClient(api_key)
    except SecretError:
        raise ValueError("api_key is required")

    # Optional secret
    try:
        webhook_secret = self.get_secret("webhook_secret")
        self.webhook_validator = WebhookValidator(webhook_secret)
        self.log(LogLevel.INFO, "Webhook validation enabled")
    except SecretError:
        self.webhook_validator = None
        self.log(LogLevel.INFO, "Webhook validation disabled (no secret)")
```

## Lazy Loading

**Fetch secret only when needed:**
```python
class APIAgent(OpperatorAgent):
    def initialize(self):
        self._client = None

    @property
    def client(self):
        """Lazy-load API client with credentials"""
        if self._client is None:
            api_key = self.get_secret("api_key")
            self._client = APIClient(api_key)
        return self._client

    def _cmd_fetch_data(self, args):
        # API key fetched only when first command runs
        data = self.client.get("/data")
        return data
```

## Complete Example

Agent using secrets securely:

```python
from opperator import OpperatorAgent, LogLevel, SecretError
import requests
from typing import Dict, Any

class GitHubAgent(OpperatorAgent):
    def initialize(self):
        self.set_description("GitHub API agent with secure token storage")

        # Don't fetch token yet - wait until needed
        self._github_client = None

        self.register_command(
            "list_repos",
            self._cmd_list_repos,
            description="List user repositories",
        )

        self.register_command(
            "create_issue",
            self._cmd_create_issue,
            description="Create a GitHub issue",
        )

    def _get_github_token(self) -> str:
        """Fetch GitHub token from secrets"""
        try:
            return self.get_secret("github_token", timeout=5.0)
        except SecretError as e:
            self.log(
                LogLevel.ERROR,
                "GitHub token not configured",
                error=str(e)
            )
            raise ValueError(
                "GitHub token required. Create with: "
                "opperator secret create --name github_token"
            )

    def _cmd_list_repos(self, args: Dict[str, Any]):
        """List user repositories"""
        # Fetch token only when needed
        token = self._get_github_token()

        # Use immediately in request
        response = requests.get(
            "https://api.github.com/user/repos",
            headers={
                "Authorization": f"token {token}",
                "Accept": "application/vnd.github.v3+json"
            }
        )

        # Token is garbage collected here
        response.raise_for_status()
        repos = response.json()

        return {
            "count": len(repos),
            "repos": [r["full_name"] for r in repos]
        }

    def _cmd_create_issue(self, args: Dict[str, Any]):
        """Create a GitHub issue"""
        repo = args["repo"]
        title = args["title"]
        body = args.get("body", "")

        # Fetch token
        token = self._get_github_token()

        # Use in request
        response = requests.post(
            f"https://api.github.com/repos/{repo}/issues",
            headers={
                "Authorization": f"token {token}",
                "Accept": "application/vnd.github.v3+json"
            },
            json={
                "title": title,
                "body": body
            }
        )

        # Token is garbage collected
        response.raise_for_status()
        issue = response.json()

        self.log(
            LogLevel.INFO,
            "Issue created",
            repo=repo,
            number=issue["number"]
        )

        return {
            "number": issue["number"],
            "url": issue["html_url"]
        }
```

## Timeouts

**Default timeout (5 seconds):**
```python
api_key = self.get_secret("api_key")
```

**Custom timeout:**
```python
# Longer timeout for slow systems
api_key = self.get_secret("api_key", timeout=10.0)

# Shorter timeout for quick fail
api_key = self.get_secret("api_key", timeout=2.0)
```

**Handle timeout:**
```python
try:
    api_key = self.get_secret("api_key", timeout=5.0)
except SecretError as e:
    if "timeout" in str(e).lower():
        self.log(LogLevel.ERROR, "Secret fetch timed out")
    else:
        self.log(LogLevel.ERROR, "Secret not found")
    raise
```

## Secret Naming

**Use descriptive names:**
```python
# Good names
self.get_secret("github_token")
self.get_secret("stripe_api_key")
self.get_secret("db_password")
self.get_secret("jwt_signing_key")

# Bad names
self.get_secret("key")
self.get_secret("token")
self.get_secret("secret1")
```

**Agent-specific prefix:**
```python
# For agent-specific secrets
self.get_secret("webhook_agent_api_key")
self.get_secret("monitor_agent_slack_token")

# For shared secrets
self.get_secret("shared_db_password")
```

## Troubleshooting

**Secret not found:**
```
SecretError: secret not found: api_key
```
Create the secret:
```bash
opperator secret create --name api_key
```

**Timeout errors:**
```
SecretError: timeout fetching secret
```
- Check daemon is running
- Increase timeout value
- Check system load

**Permission errors:**
```
SecretError: permission denied
```
- Check keyring permissions
- Restart daemon
- Check OS keyring access

## Best Practices

**Fetch when needed:**
```python
# Good: Fetch in command handler
def _cmd_sync(self, args):
    token = self.get_secret("sync_token")
    self.sync_with_token(token)

# Bad: Fetch in initialize
def initialize(self):
    self.token = self.get_secret("sync_token")  # Stored!
```

**Handle errors gracefully:**
```python
# Good: Graceful degradation
try:
    token = self.get_secret("optional_feature_token")
    self.enable_feature(token)
except SecretError:
    self.log(LogLevel.INFO, "Optional feature disabled")

# Bad: Crash on missing optional secret
token = self.get_secret("optional_feature_token")  # Raises!
```

**Clear error messages:**
```python
# Good: Tell user how to fix
except SecretError as e:
    self.log(
        LogLevel.ERROR,
        "GitHub token required. "
        "Create with: opperator secret create --name github_token",
        error=str(e)
    )

# Bad: Unclear error
except SecretError as e:
    self.log(LogLevel.ERROR, "Error", error=str(e))
```

## Summary

**Fetch secrets:**
```python
secret = self.get_secret("name", timeout=5.0)
```

**Security rules:**
- Never log secrets
- Never store secrets in memory
- Use immediately and discard
- Handle missing secrets gracefully

**Create secrets:**
```bash
opperator secret create --name secret_name
```

**Error handling:**
```python
try:
    secret = self.get_secret("name")
except SecretError as e:
    # Handle missing secret
    self.log(LogLevel.ERROR, "Secret not found", error=str(e))
```

Secrets provide secure credential storage
for agents without hardcoding sensitive data.
