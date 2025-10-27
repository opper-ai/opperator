Manages secrets inside the Opperator daemon. The tool renders a focused input
form so the operator can supply or update sensitive values without leaving the
TUI.

Parameters
- `mode` (`string`, required): One of `create`, `update`, or `delete`.
  - `create` prompts for a new secret value and stores it if non-empty.
  - `update` preloads the existing value when available and saves the
    replacement.
  - `delete` removes the secret; the value field is hidden.
- `name` (`string`, required): Canonical secret identifier to pass to the
  daemon (e.g., `gmail_client_secret`).
- `description` (`string`, optional): Short hint shown above the input field to
  help the user remember what credential is needed.
- `value_label` (`string`, optional, default `Secret value`): Customizes the
  placeholder/label rendered with the focused input.
- `documentation_url` (`string`, optional): When provided, the TUI surfaces a
  "Need help?" link so the user can follow instructions on how to obtain the
  API key.

Behavior
- The tool must render an inline form with a single masked text input that
  autofocuses when `mode` is `create` or `update`.
- Submission sends the value back to the daemon, which persists it using the
  existing secrets API (`./opperator secret ...`).
- On success, the view transitions to a confirmation card summarizing the
  action (`created`, `updated`, or `deleted`) without exposing the secret.
- On failure, the tool displays the error message and keeps the value in memory
  so the user can retry without retyping.
- Invocations in `delete` mode render a confirmation warning and require the
  operator to affirm the action before sending the request to the daemon.
