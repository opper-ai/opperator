ALTER TABLE tool_tasks ADD COLUMN origin TEXT;
ALTER TABLE tool_tasks ADD COLUMN client_id TEXT;

CREATE INDEX IF NOT EXISTS idx_tool_tasks_origin ON tool_tasks(origin);
CREATE INDEX IF NOT EXISTS idx_tool_tasks_client ON tool_tasks(client_id);
