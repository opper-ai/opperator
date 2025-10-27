Manage plan items for the currently focused agent. Plans are stored in the database and include a specification (task overview) and individual plan items.

Actions:
- `get` - Retrieve the complete plan including specification and all items
- `set_specification` - Set or update the plan specification/overview (provide `specification` parameter)
- `add` - Create a new plan item (provide `text` parameter with the item description)
- `toggle` - Toggle a plan item's completion status (provide `id` parameter)
- `remove` - Delete a specific plan item (provide `id` parameter)
- `clear` - Remove all plan items
- `list` - List all current plan items

The tool returns the current list of plan items after the action completes. The specification is kept internal and not displayed in the UI, except when using the `get` action.
