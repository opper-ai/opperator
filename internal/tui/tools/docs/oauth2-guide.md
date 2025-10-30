# OAuth 2.0 Integration Guide

This guide shows how to implement OAuth 2.0 authentication in Opperator agents using Gmail as a concrete example. The pattern demonstrated here applies to any OAuth 2.0 provider (Google Drive, GitHub, Microsoft, etc.).

***

## Overview

**Goal:** Enable agents to securely authenticate with external services using OAuth 2.0, with user credentials managed through Opperator's secure keyring and generated tokens persisted locally.

**Storage Architecture:**

```
User-Provided Credentials          Generated Tokens
(Client ID, Client Secret)         (Access Token, Refresh Token)
        ↓                                   ↓
Opperator Keyring                   Local JSON File
(Set via TUI/CLI)                   (Agent root directory)
```

**Key Opperator Integration Points:**

1. **Async Commands** - Authorization flows run without blocking the UI
2. **Secret Management** - Client credentials stored securely in keyring
3. **Sidebar Sections** - Real-time authentication status display
4. **Progress Reporting** - Updates during browser authorization flow
5. **Background Tasks** - Automatic token refresh for long-running operations

**OAuth Flow Summary:**

```
1. User provides Client ID/Secret → Stored in Opperator keyring
2. Agent runs async authorize command
3. Opens browser to OAuth provider
4. Local HTTP server captures authorization code
5. Exchange code for access/refresh tokens
6. Save tokens to local JSON file
7. Use tokens for API calls (refresh automatically when expired)
```

## Gmail OAuth Example (Complete Implementation)

This example demonstrates the full OAuth flow for Gmail API access, using the `invoice-monitor` agent pattern.

### Prerequisites

**1. Get Google OAuth Credentials:**

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create or select a project
3. Enable Gmail API (APIs & Services > Library)
4. Create OAuth 2.0 Client ID (APIs & Services > Credentials)
5. Application type: "Desktop app"
6. Add redirect URI: `http://localhost`

**2. Install Dependencies:**

Follow the standard Opperator dependency installation workflow:

```bash
# 1. Stop the agent (if already running)
stop_agent(agent_name="invoice-monitor")

# 2. Add dependencies to pyproject.toml
# Edit ~/.config/opperator/agents/invoice-monitor/pyproject.toml
edit(
    path="~/.config/opperator/agents/invoice-monitor/pyproject.toml",
    old_string='dependencies = []',
    new_string='''dependencies = [
    "google-auth-oauthlib>=1.2.0",
    "google-auth-httplib2>=0.2.0",
    "google-api-python-client>=2.108.0",
]'''
)

# 3. Install using uv (CRITICAL: targets agent's isolated environment)
bash("""
uv pip install ~/.config/opperator/agents/invoice-monitor/ \\
  --python ~/.config/opperator/agents/invoice-monitor/.venv/bin/python
""")

# 4. Restart agent
restart_agent(agent_name="invoice-monitor")
```

**Why this command pattern?**
- Uses `pyproject.toml` as the modern dependency manifest
- `uv pip install` provides 10-100x faster dependency resolution
- `--python` flag targets the agent's isolated VENV
- Ensures dependencies are available when agent runs

**3. Set Credentials in Opperator:**

```bash
opperator secret set invoice_monitor_gmail_client_id "<your_client_id>"
opperator secret set invoice_monitor_gmail_client_secret "<your_client_secret>"
```

### Agent Implementation

```python
from opperator.agent import Agent
from opperator.secrets import get_secret, SecretError
from google_auth_oauthlib.flow import InstalledAppFlow
from google.auth.transport.requests import Request
from google.oauth2.credentials import Credentials
from googleapiclient.discovery import build
import os
import tempfile
import json
from typing import Optional

# Gmail API scopes - request only what you need
SCOPES = ['https://www.googleapis.com/auth/gmail.readonly']


class InvoiceMonitorAgent(Agent):
    def __init__(self):
        super().__init__()

        # OAuth credentials (from keyring)
        self.client_id: Optional[str] = None
        self.client_secret: Optional[str] = None

        # Token storage (local file)
        self.token_path: Optional[str] = None

        # Gmail service
        self.gmail_service = None
        self.authenticated_email: Optional[str] = None

    def initialize(self) -> None:
        """Initialize agent and set up OAuth."""
        try:
            # Retrieve user-provided credentials from keyring
            self.client_id = get_secret("invoice_monitor_gmail_client_id")
            self.client_secret = get_secret("invoice_monitor_gmail_client_secret")

            # Token file in agent root directory
            agent_root = os.path.dirname(os.path.abspath(__file__))
            self.token_path = os.path.join(agent_root, "token.json")

            # Register async authorization command
            self.register_command(
                name="authorize_gmail",
                handler=self._cmd_authorize_gmail,
                description="Authorize Gmail access via browser OAuth flow",
                async_enabled=True,  # Don't block UI during browser flow
                progress_label="Authorizing Gmail"
            )

            # Register sidebar section for auth status
            self.register_section(
                section_id="auth_status",
                title="Gmail Authentication",
                content=self._build_auth_status_content(),
                collapsed=False
            )

            # Try to initialize Gmail service with existing token
            self._setup_gmail_service()

        except SecretError as e:
            self.log_error(f"OAuth credentials not configured: {e}")
            self.log_info("Set secrets using Opperator CLI:")
            self.log_info("  opperator secret set invoice_monitor_gmail_client_id <value>")
            self.log_info("  opperator secret set invoice_monitor_gmail_client_secret <value>")

    def _cmd_authorize_gmail(self, args: dict) -> dict:
        """
        Interactive OAuth authorization command.

        Opens browser for user to grant Gmail access, captures authorization
        code via local callback server, and stores tokens locally.
        """
        if not self.client_id or not self.client_secret:
            return {
                "status": "error",
                "message": "OAuth credentials not set. Configure secrets first."
            }

        self.report_progress("Preparing authorization flow...")

        # Create temporary client_secret.json for InstalledAppFlow
        # This file contains user-provided credentials and is immediately deleted
        client_config = {
            "installed": {
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "auth_uri": "https://accounts.google.com/o/oauth2/auth",
                "token_uri": "https://oauth2.googleapis.com/token",
                "redirect_uris": ["http://localhost", "urn:ietf:wg:oauth:2.0:oob"]
            }
        }

        temp_creds_path = None
        try:
            # Write temporary credentials file
            with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False) as f:
                json.dump(client_config, f)
                temp_creds_path = f.name

            self.report_progress("Starting local callback server...")

            # Create OAuth flow from temporary credentials
            flow = InstalledAppFlow.from_client_secrets_file(
                temp_creds_path,
                SCOPES
            )

            self.report_progress("Opening browser for authorization...")

            # Run OAuth flow with local server
            # - port=0: Use any available port
            # - open_browser=True: Automatically open authorization URL
            # - Local server captures OAuth callback and extracts authorization code
            creds = flow.run_local_server(
                port=0,
                open_browser=True,
                success_message="Authorization successful! You can close this window.",
            )

            # Save generated tokens to persistent local file
            with open(self.token_path, 'w') as token_file:
                token_file.write(creds.to_json())

            self.report_progress("Tokens saved, initializing service...")

            # Initialize Gmail service with new credentials
            self._setup_gmail_service()

            return {
                "status": "success",
                "message": f"Successfully authorized as {self.authenticated_email}",
                "email": self.authenticated_email
            }

        except Exception as e:
            self.log_error(f"Authorization failed: {e}")
            return {
                "status": "error",
                "message": f"Authorization failed: {str(e)}"
            }

        finally:
            # CRITICAL: Clean up temporary credentials file
            if temp_creds_path and os.path.exists(temp_creds_path):
                try:
                    os.unlink(temp_creds_path)
                except Exception as e:
                    self.log_warning(f"Failed to delete temporary credentials: {e}")

    def _setup_gmail_service(self) -> None:
        """
        Initialize Gmail API service using stored token.

        Handles automatic token refresh if expired.
        """
        if not os.path.exists(self.token_path):
            self.log_info("No token file found. Run 'authorize_gmail' to authenticate.")
            self._update_auth_status_section()
            return

        try:
            # Load credentials from token file
            creds = Credentials.from_authorized_user_file(self.token_path, SCOPES)

            # Check if token needs refresh
            if not creds.valid:
                if creds.expired and creds.refresh_token:
                    self.log_info("Token expired, refreshing...")

                    # Refresh using stored refresh token
                    creds.refresh(Request())

                    # Save refreshed token back to file
                    with open(self.token_path, 'w') as token_file:
                        token_file.write(creds.to_json())

                    self.log_info("Token refreshed successfully")
                else:
                    self.log_warning("Token invalid and cannot be refreshed. Re-authorize required.")
                    self.gmail_service = None
                    self.authenticated_email = None
                    self._update_auth_status_section()
                    return

            # Build Gmail service
            self.gmail_service = build('gmail', 'v1', credentials=creds)

            # Fetch authenticated user's email for display
            profile = self.gmail_service.users().getProfile(userId='me').execute()
            self.authenticated_email = profile.get('emailAddress', 'Unknown')

            self.log_info(f"Gmail service initialized for {self.authenticated_email}")
            self._update_auth_status_section()

        except Exception as e:
            self.log_error(f"Failed to initialize Gmail service: {e}")
            self.gmail_service = None
            self.authenticated_email = None
            self._update_auth_status_section()

    def _build_auth_status_content(self) -> str:
        """Build sidebar content showing current authentication status."""
        lines = []

        # Overall status indicator
        if self.gmail_service and self.authenticated_email:
            lines.append("<c fg='green'>✓ Authenticated</c>")
        else:
            lines.append("<c fg='yellow'>⚠ Not Authenticated</c>")

        # Account info
        if self.authenticated_email:
            lines.append(f"\n<b>Account:</b> {self.authenticated_email}")

        # Secrets status
        if self.client_id and self.client_secret:
            lines.append(f"\n<b>Credentials:</b> <c fg='green'>Configured</c>")
        else:
            lines.append(f"\n<b>Credentials:</b> <c fg='red'>Missing</c>")

        # Token file status
        if self.token_path:
            if os.path.exists(self.token_path):
                lines.append(f"\n<b>Token File:</b> <c fg='green'>OK</c>")
            else:
                lines.append(f"\n<b>Token File:</b> <c fg='red'>Missing</c>")

        # Guidance
        if not self.client_id or not self.client_secret:
            lines.append("\n<c fg='yellow'>→ Set credentials via Opperator CLI</c>")
        elif not self.gmail_service:
            lines.append("\n<c fg='yellow'>→ Run 'authorize_gmail' to authenticate</c>")
        else:
            lines.append("\n<c fg='green'>→ Monitoring active</c>")

        return '\n'.join(lines)

    def _update_auth_status_section(self) -> None:
        """Update sidebar section with current auth status."""
        self.update_section("auth_status", self._build_auth_status_content())
```

### Background Token Refresh

For agents with background tasks that use the Gmail API:

```python
def _ensure_valid_credentials(self) -> bool:
    """
    Ensure credentials are valid before API calls.
    Automatically refreshes if expired.

    Returns:
        True if credentials are valid, False otherwise
    """
    if not self.gmail_service:
        return False

    try:
        # Load current credentials
        creds = Credentials.from_authorized_user_file(self.token_path, SCOPES)

        # Refresh if expired
        if not creds.valid and creds.expired and creds.refresh_token:
            self.log_info("Refreshing expired token...")
            creds.refresh(Request())

            # Save refreshed token
            with open(self.token_path, 'w') as token_file:
                token_file.write(creds.to_json())

            # Rebuild service with new credentials
            self.gmail_service = build('gmail', 'v1', credentials=creds)

        return creds.valid

    except Exception as e:
        self.log_error(f"Failed to refresh credentials: {e}")
        return False


def _background_task(self) -> None:
    """Example background task that uses Gmail API."""
    while not self._stop_event.is_set():
        # Ensure valid credentials before API call
        if not self._ensure_valid_credentials():
            self.log_warning("Credentials invalid, skipping API call")
            self._stop_event.wait(60)
            continue

        try:
            # Make Gmail API calls
            results = self.gmail_service.users().messages().list(
                userId='me',
                q='is:unread'
            ).execute()

            # Process results...

        except Exception as e:
            self.log_error(f"API call failed: {e}")

        self._stop_event.wait(300)  # Check every 5 minutes
```

## Local Callback Server Implementation

For OAuth providers without a dedicated library like Google's `InstalledAppFlow`, implement a custom callback server:

```python
import http.server
import threading
import webbrowser
import urllib.parse
from typing import Optional


class OAuthCallbackHandler(http.server.BaseHTTPRequestHandler):
    """HTTP server handler that captures OAuth authorization codes."""

    # Class-level storage for captured values
    authorization_code: Optional[str] = None
    error: Optional[str] = None

    def do_GET(self):
        """Handle OAuth provider redirect."""
        query = urllib.parse.urlparse(self.path).query
        params = urllib.parse.parse_qs(query)

        if 'code' in params:
            # Capture authorization code
            OAuthCallbackHandler.authorization_code = params['code'][0]

            # Send success page
            self.send_response(200)
            self.send_header('Content-type', 'text/html')
            self.end_headers()
            self.wfile.write(b"""
                <html><body style="font-family: sans-serif; text-align: center; padding: 50px;">
                    <h1 style="color: green;">Authorization Successful</h1>
                    <p>You can close this window and return to Opperator.</p>
                </body></html>
            """)

        elif 'error' in params:
            # Capture error
            OAuthCallbackHandler.error = params['error'][0]

            # Send error page
            self.send_response(200)
            self.send_header('Content-type', 'text/html')
            self.end_headers()
            error_msg = params['error'][0]
            self.wfile.write(f"""
                <html><body style="font-family: sans-serif; text-align: center; padding: 50px;">
                    <h1 style="color: red;">Authorization Failed</h1>
                    <p>Error: {error_msg}</p>
                </body></html>
            """.encode())

    def log_message(self, format, *args):
        """Suppress server logs to avoid cluttering agent output."""
        pass


def run_oauth_callback_server(port: int = 0) -> tuple[http.server.HTTPServer, int]:
    """
    Start a temporary HTTP server for OAuth callbacks.

    Args:
        port: Port to bind (0 = random available port)

    Returns:
        (server, actual_port) tuple
    """
    server = http.server.HTTPServer(('localhost', port), OAuthCallbackHandler)
    actual_port = server.server_address[1]

    # Run server in background thread
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()

    return server, actual_port


# Usage in authorization command:
def _cmd_authorize_custom_oauth(self, args: dict) -> dict:
    """Example authorization using custom callback server."""

    # Start callback server
    server, port = run_oauth_callback_server()
    redirect_uri = f"http://localhost:{port}"

    try:
        # Build authorization URL
        auth_url = f"{AUTH_ENDPOINT}?client_id={self.client_id}&redirect_uri={redirect_uri}&response_type=code&scope={SCOPES}"

        # Open browser
        webbrowser.open(auth_url)
        self.report_progress(f"Waiting for authorization on port {port}...")

        # Wait for callback (with timeout)
        import time
        timeout = 300  # 5 minutes
        start_time = time.time()

        while time.time() - start_time < timeout:
            if OAuthCallbackHandler.authorization_code:
                break
            if OAuthCallbackHandler.error:
                return {"status": "error", "message": OAuthCallbackHandler.error}
            time.sleep(0.5)

        if not OAuthCallbackHandler.authorization_code:
            return {"status": "error", "message": "Authorization timeout"}

        # Exchange code for tokens
        # ... (provider-specific token exchange)

        return {"status": "success"}

    finally:
        server.shutdown()
        # Reset handler state
        OAuthCallbackHandler.authorization_code = None
        OAuthCallbackHandler.error = None
```

## Troubleshooting

### Issue: "Redirect URI mismatch"

**Cause:** OAuth provider expects exact redirect URI registered in app settings.

**Solution:**
1. Check redirect URI in provider's developer console
2. Common values: `http://localhost`, `http://localhost:8080`
3. If using `port=0` (random port), register `http://localhost` without port
4. Update provider settings to match your code

### Issue: "Port already in use"

**Cause:** Another process is using the callback server port.

**Solution:**

Use `port=0` for automatic port selection:
```python
# Google's InstalledAppFlow handles this automatically
flow.run_local_server(port=0)

# For custom server:
server, actual_port = run_oauth_callback_server(port=0)
```

Or try a range of ports:
```python
for port in range(8080, 8090):
    try:
        server = http.server.HTTPServer(('localhost', port), handler)
        break
    except OSError:
        continue
```

### Issue: "Token refresh fails"

**Cause:** Refresh token expired, revoked, or invalid.

**Solution:**
1. Delete token file: `rm ~/.config/opperator/agents/your-agent/token.json`
2. Run authorization command again
3. Check if user revoked access in provider's account settings

```python
def _handle_refresh_failure(self) -> None:
    """Handle invalid refresh token."""
    self.log_warning("Refresh token invalid - re-authorization required")

    if os.path.exists(self.token_path):
        os.unlink(self.token_path)

    self.update_section("auth_status",
        "<c fg='red'>✗ Token Expired</c>\n\n"
        "Run 'authorize_gmail' to re-authenticate."
    )
```

### Issue: "Browser doesn't open automatically"

**Cause:** No default browser configured or display not available.

**Solution:**

```python
try:
    webbrowser.open(auth_url)
except Exception as e:
    self.log_info("Could not open browser automatically")
    self.log_info(f"Please open this URL manually:\n{auth_url}")
```

### Issue: "SecretError: Secret not found"

**Cause:** OAuth credentials not set in Opperator keyring.

**Solution:**

```bash
# Set secrets via CLI
opperator secret set your_agent_client_id "<value>"
opperator secret set your_agent_client_secret "<value>"

# Verify secrets are set
opperator secret list
```

## Quick Start Checklist

**1. Get OAuth credentials from provider:**
- Google: [Cloud Console](https://console.cloud.google.com/)
- Create OAuth 2.0 Client ID (Desktop app type)
- Add redirect URI: `http://localhost`

**2. Install dependencies (using agent's VENV):**
```bash
# Stop agent
stop_agent(agent_name="your-agent")

# Edit ~/.config/opperator/agents/your-agent/pyproject.toml
# Add to dependencies array:
#   "google-auth-oauthlib>=1.2.0",
#   "google-auth-httplib2>=0.2.0",
#   "google-api-python-client>=2.108.0",

# Install using uv with --python flag
uv pip install ~/.config/opperator/agents/your-agent/ \
  --python ~/.config/opperator/agents/your-agent/.venv/bin/python

# Restart agent
restart_agent(agent_name="your-agent")
```

**3. Set credentials in Opperator:**
```bash
opperator secret set your_agent_client_id "<client_id>"
opperator secret set your_agent_client_secret "<client_secret>"
```

**4. Implement agent initialization:**
```python
# In initialize() method:
# 1. Load credentials from keyring
self.client_id = get_secret("your_agent_client_id")
self.client_secret = get_secret("your_agent_client_secret")

# 2. Set token file path
agent_root = os.path.dirname(os.path.abspath(__file__))
self.token_path = os.path.join(agent_root, "token.json")

# 3. Register async authorization command
self.register_command("authorize", self._cmd_authorize, async_enabled=True)
```

**5. Implement authorization command:**
- Create temporary `client_secret.json` from keyring credentials
- Run `InstalledAppFlow.run_local_server(port=0)`
- Save tokens to `self.token_path`
- Clean up temporary credentials file

**6. Implement service initialization:**
- Load tokens from file
- Check if refresh needed
- Build API service client
- Update sidebar status

**7. Test the flow:**
```bash
# Start agent
opperator agent start your-agent

# Run authorization command
# (Opens browser, user grants access, tokens saved locally)

# Agent now has access to OAuth-protected APIs
```

---

## Summary

OAuth 2.0 integration in Opperator follows a simple pattern:

1. **User-provided credentials** (Client ID/Secret) → Opperator keyring
2. **Generated tokens** (Access/Refresh) → Local JSON file
3. **Async authorization command** prevents UI blocking during browser flow
4. **Automatic token refresh** for long-running operations
5. **Sidebar status** provides real-time authentication state

This pattern works for any OAuth 2.0 provider - just adapt the endpoints, scopes, and API client library.
