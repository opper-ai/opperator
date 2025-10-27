Lists the names of secrets stored in the Opperator keyring so the agent can
reuse existing credentials instead of prompting the operator unnecessarily.

Behavior
- The tool emits a simple bulleted list of secret identifiers. Reserved secrets
  such as `OPPER_API_KEY` include a friendly description (`Your default Opper
  API key`).
- When no secrets have been registered, the tool returns a short message instead
  of an empty list.
- The response metadata contains a JSON payload listing the discovered secret
  names and any accompanying labels so follow-up tools can reuse the data.
