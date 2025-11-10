CREATE TABLE IF NOT EXISTS custom_sections (
    agent_name TEXT NOT NULL,
    section_id TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    collapsed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (agent_name, section_id)
);

CREATE INDEX IF NOT EXISTS idx_custom_sections_agent ON custom_sections(agent_name);
