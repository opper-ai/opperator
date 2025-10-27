-- Track per-session input history for recall/navigation
CREATE TABLE IF NOT EXISTS input_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    text TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES conversations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_input_history_session_id ON input_history(session_id);
CREATE INDEX IF NOT EXISTS idx_input_history_session_created ON input_history(session_id, created_at DESC);

