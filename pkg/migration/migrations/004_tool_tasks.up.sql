-- Persist daemon async tool tasks in SQLite database.
CREATE TABLE IF NOT EXISTS tool_tasks (
    id TEXT PRIMARY KEY,
    tool_name TEXT NOT NULL,
    args TEXT,
    working_dir TEXT,
    session_id TEXT,
    call_id TEXT,
    mode TEXT NOT NULL,
    agent_name TEXT,
    command_name TEXT,
    command_args TEXT,
    status TEXT NOT NULL,
    result TEXT,
    metadata TEXT,
    error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER,
    FOREIGN KEY (session_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tool_task_progress (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    text TEXT,
    metadata TEXT,
    status TEXT,
    FOREIGN KEY (task_id) REFERENCES tool_tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tool_tasks_status ON tool_tasks(status);
CREATE INDEX IF NOT EXISTS idx_tool_tasks_session ON tool_tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tool_tasks_call ON tool_tasks(call_id);
CREATE INDEX IF NOT EXISTS idx_tool_task_progress_task ON tool_task_progress(task_id, timestamp);
