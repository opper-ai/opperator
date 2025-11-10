package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"tui/components/sidebar"
)

// setupTestDB creates a test database with the custom_sections schema
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create the schema
	_, err = db.Exec(`
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
	`)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(dbPath)
	}

	return db, cleanup
}

func TestSectionStore_InMemory(t *testing.T) {
	store := NewInMemorySectionStore()

	section := sidebar.CustomSection{
		ID:        "test1",
		Title:     "Test Section",
		Content:   "Test content",
		Collapsed: false,
	}

	// Test save
	err := store.SaveSection("agent1", "test1", section)
	if err != nil {
		t.Fatalf("Failed to save section: %v", err)
	}

	// Test get
	sections := store.GetSections("agent1")
	if len(sections) != 1 {
		t.Fatalf("Expected 1 section, got %d", len(sections))
	}

	if sections[0].ID != "test1" {
		t.Errorf("Expected ID test1, got %s", sections[0].ID)
	}

	// Test delete
	err = store.DeleteSection("agent1", "test1")
	if err != nil {
		t.Fatalf("Failed to delete section: %v", err)
	}

	sections = store.GetSections("agent1")
	if len(sections) != 0 {
		t.Fatalf("Expected 0 sections after delete, got %d", len(sections))
	}
}

func TestSectionStore_Persistence(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create store
	store, err := NewSectionStore(db, SectionStoreConfig{
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	section := sidebar.CustomSection{
		ID:        "test1",
		Title:     "Test Section",
		Content:   "Test content",
		Collapsed: false,
	}

	// Save section
	err = store.SaveSection("agent1", "test1", section)
	if err != nil {
		t.Fatalf("Failed to save section: %v", err)
	}

	// Verify in cache
	sections := store.GetSections("agent1")
	if len(sections) != 1 {
		t.Fatalf("Expected 1 section in cache, got %d", len(sections))
	}

	// Wait for flush
	time.Sleep(150 * time.Millisecond)

	// Close store
	store.Close()

	// Reopen store with same DB connection (should load from DB)
	store2, err := NewSectionStore(db, SectionStoreConfig{
		FlushInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()

	// Verify sections loaded from DB
	sections = store2.GetSections("agent1")
	if len(sections) != 1 {
		t.Fatalf("Expected 1 section loaded from DB, got %d", len(sections))
	}

	if sections[0].Title != "Test Section" {
		t.Errorf("Expected title 'Test Section', got %s", sections[0].Title)
	}
}

func TestSectionStore_RapidUpdates(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewSectionStore(db, SectionStoreConfig{
		FlushInterval: 1 * time.Second, // Long interval to test batching
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Simulate rapid updates (100 updates in quick succession)
	start := time.Now()
	for i := 0; i < 100; i++ {
		section := sidebar.CustomSection{
			ID:        "metrics",
			Title:     "Metrics",
			Content:   "Update " + string(rune(i)),
			Collapsed: false,
		}
		store.SaveSection("agent1", "metrics", section)
	}
	elapsed := time.Since(start)

	// Should complete very quickly (all writes are to cache)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Rapid updates took too long: %v (expected < 100ms)", elapsed)
	}

	// Verify latest version is in cache
	sections := store.GetSections("agent1")
	if len(sections) != 1 {
		t.Fatalf("Expected 1 section, got %d", len(sections))
	}

	// Check stats
	stats := store.Stats()
	if stats.TotalSections != 1 {
		t.Errorf("Expected 1 total section, got %d", stats.TotalSections)
	}
	if stats.DirtySections != 1 {
		t.Errorf("Expected 1 dirty section, got %d", stats.DirtySections)
	}
}

func TestSectionStore_MultipleAgents(t *testing.T) {
	store := NewInMemorySectionStore()

	// Add sections for multiple agents
	for i := 1; i <= 3; i++ {
		agentName := "agent" + string(rune('0'+i))
		for j := 1; j <= 2; j++ {
			sectionID := "section" + string(rune('0'+j))
			section := sidebar.CustomSection{
				ID:        sectionID,
				Title:     "Section " + string(rune('0'+j)),
				Content:   "Content for " + agentName,
				Collapsed: false,
			}
			store.SaveSection(agentName, sectionID, section)
		}
	}

	// Verify each agent has 2 sections
	for i := 1; i <= 3; i++ {
		agentName := "agent" + string(rune('0'+i))
		sections := store.GetSections(agentName)
		if len(sections) != 2 {
			t.Errorf("Agent %s: expected 2 sections, got %d", agentName, len(sections))
		}
	}

	// Clear one agent
	store.ClearAgent("agent2")
	sections := store.GetSections("agent2")
	if len(sections) != 0 {
		t.Errorf("Expected 0 sections for agent2 after clear, got %d", len(sections))
	}

	// Verify other agents unaffected
	sections = store.GetSections("agent1")
	if len(sections) != 2 {
		t.Errorf("Expected agent1 to still have 2 sections, got %d", len(sections))
	}

	stats := store.Stats()
	if stats.TotalAgents != 2 {
		t.Errorf("Expected 2 agents after clear, got %d", stats.TotalAgents)
	}
	if stats.TotalSections != 4 {
		t.Errorf("Expected 4 total sections, got %d", stats.TotalSections)
	}
}

func TestSectionStore_CleanupOnClose(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store, err := NewSectionStore(db, SectionStoreConfig{
		FlushInterval: 10 * time.Second, // Long interval
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	section := sidebar.CustomSection{
		ID:        "test1",
		Title:     "Test",
		Content:   "Content",
		Collapsed: false,
	}

	// Save without waiting for flush
	store.SaveSection("agent1", "test1", section)

	// Close immediately (should flush dirty sections)
	store.Close()

	// Reopen and verify data was persisted
	store2, err := NewSectionStore(db, SectionStoreConfig{
		FlushInterval: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()

	sections := store2.GetSections("agent1")
	if len(sections) != 1 {
		t.Fatalf("Expected 1 section after close flush, got %d", len(sections))
	}
}
