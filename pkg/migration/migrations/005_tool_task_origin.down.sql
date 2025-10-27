DROP INDEX IF EXISTS idx_tool_tasks_client;
DROP INDEX IF EXISTS idx_tool_tasks_origin;

ALTER TABLE tool_tasks DROP COLUMN origin;
ALTER TABLE tool_tasks DROP COLUMN client_id;
