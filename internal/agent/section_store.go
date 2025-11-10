package agent

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"tui/components/sidebar"
)

// SectionStore provides persistent storage for agent custom sections with
// write-through caching and batched persistence to minimize disk I/O.
type SectionStore struct {
	// In-memory cache for fast reads/writes
	cache map[string]map[string]*sidebar.CustomSection // agentName -> sectionID -> section
	mu    sync.RWMutex

	// Dirty tracking for batched persistence
	dirty   map[string]map[string]bool // agentName -> sectionID -> dirty flag
	dirtyMu sync.Mutex

	// Database for persistence
	db     *sql.DB
	dbPath string

	// Background flusher
	flushInterval time.Duration
	stopFlush     chan struct{}
	flushDone     sync.WaitGroup
	closed        bool
	closeMu       sync.Mutex
}

// SectionStoreConfig configures the section store behavior
type SectionStoreConfig struct {
	DBPath        string
	FlushInterval time.Duration // How often to flush dirty sections to disk
}

// NewSectionStore creates a new section store with persistence using an existing DB connection
func NewSectionStore(db *sql.DB, config SectionStoreConfig) (*SectionStore, error) {
	if config.FlushInterval == 0 {
		config.FlushInterval = 5 * time.Second
	}

	store := &SectionStore{
		cache:         make(map[string]map[string]*sidebar.CustomSection),
		dirty:         make(map[string]map[string]bool),
		db:            db,
		flushInterval: config.FlushInterval,
		stopFlush:     make(chan struct{}),
	}

	// Load existing sections from DB into cache
	if err := store.loadCache(); err != nil {
		return nil, fmt.Errorf("failed to load cache: %w", err)
	}

	// Start background flusher
	store.startFlusher()

	return store, nil
}

// NewInMemorySectionStore creates a section store without persistence (for testing)
func NewInMemorySectionStore() *SectionStore {
	return &SectionStore{
		cache:         make(map[string]map[string]*sidebar.CustomSection),
		dirty:         make(map[string]map[string]bool),
		flushInterval: 0, // Disabled
		stopFlush:     make(chan struct{}),
	}
}

// loadCache loads all sections from database into memory cache
func (s *SectionStore) loadCache() error {
	rows, err := s.db.Query(`
		SELECT agent_name, section_id, title, content, collapsed
		FROM custom_sections
		ORDER BY agent_name, created_at
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	for rows.Next() {
		var agentName, sectionID, title, content string
		var collapsed bool

		if err := rows.Scan(&agentName, &sectionID, &title, &content, &collapsed); err != nil {
			return err
		}

		if s.cache[agentName] == nil {
			s.cache[agentName] = make(map[string]*sidebar.CustomSection)
		}

		s.cache[agentName][sectionID] = &sidebar.CustomSection{
			ID:        sectionID,
			Title:     title,
			Content:   content,
			Collapsed: collapsed,
		}
	}

	return rows.Err()
}

// SaveSection saves a section to the cache and marks it dirty for persistence
func (s *SectionStore) SaveSection(agentName, sectionID string, section sidebar.CustomSection) error {
	// Update cache immediately (fast path)
	s.mu.Lock()
	if s.cache[agentName] == nil {
		s.cache[agentName] = make(map[string]*sidebar.CustomSection)
	}
	s.cache[agentName][sectionID] = &section
	s.mu.Unlock()

	// Mark as dirty for background persistence
	s.markDirty(agentName, sectionID)

	return nil
}

// DeleteSection removes a section from cache and database
func (s *SectionStore) DeleteSection(agentName, sectionID string) error {
	// Remove from cache immediately
	s.mu.Lock()
	if s.cache[agentName] != nil {
		delete(s.cache[agentName], sectionID)
		if len(s.cache[agentName]) == 0 {
			delete(s.cache, agentName)
		}
	}
	s.mu.Unlock()

	// Remove from dirty tracking
	s.dirtyMu.Lock()
	if s.dirty[agentName] != nil {
		delete(s.dirty[agentName], sectionID)
		if len(s.dirty[agentName]) == 0 {
			delete(s.dirty, agentName)
		}
	}
	s.dirtyMu.Unlock()

	// Delete from database immediately (small overhead, ensures consistency)
	if s.db != nil {
		_, err := s.db.Exec(`DELETE FROM custom_sections WHERE agent_name = ? AND section_id = ?`, agentName, sectionID)
		return err
	}

	return nil
}

// GetSections returns all sections for an agent from cache
func (s *SectionStore) GetSections(agentName string) []sidebar.CustomSection {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sections := make([]sidebar.CustomSection, 0, len(s.cache[agentName]))
	for _, sec := range s.cache[agentName] {
		sections = append(sections, *sec)
	}
	return sections
}

// ClearAgent removes all sections for an agent
func (s *SectionStore) ClearAgent(agentName string) error {
	// Remove from cache
	s.mu.Lock()
	delete(s.cache, agentName)
	s.mu.Unlock()

	// Remove from dirty tracking
	s.dirtyMu.Lock()
	delete(s.dirty, agentName)
	s.dirtyMu.Unlock()

	// Delete from database
	if s.db != nil {
		_, err := s.db.Exec(`DELETE FROM custom_sections WHERE agent_name = ?`, agentName)
		return err
	}

	return nil
}

// markDirty marks a section as needing persistence
func (s *SectionStore) markDirty(agentName, sectionID string) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()

	if s.dirty[agentName] == nil {
		s.dirty[agentName] = make(map[string]bool)
	}
	s.dirty[agentName][sectionID] = true
}

// startFlusher starts the background goroutine that periodically flushes dirty sections
func (s *SectionStore) startFlusher() {
	if s.db == nil || s.flushInterval == 0 {
		return // No persistence
	}

	s.flushDone.Add(1)
	go func() {
		defer s.flushDone.Done()

		ticker := time.NewTicker(s.flushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.flushDirty()
			case <-s.stopFlush:
				// Final flush on shutdown
				s.flushDirty()
				return
			}
		}
	}()
}

// flushDirty writes all dirty sections to database in a single transaction
func (s *SectionStore) flushDirty() error {
	// Snapshot dirty items (minimize lock time)
	s.dirtyMu.Lock()
	toFlush := s.dirty
	s.dirty = make(map[string]map[string]bool)
	s.dirtyMu.Unlock()

	if len(toFlush) == 0 {
		return nil
	}

	// Single transaction for all writes
	tx, err := s.db.Begin()
	if err != nil {
		// Restore dirty items to retry later
		s.dirtyMu.Lock()
		for agent, sections := range toFlush {
			if s.dirty[agent] == nil {
				s.dirty[agent] = make(map[string]bool)
			}
			for section := range sections {
				s.dirty[agent][section] = true
			}
		}
		s.dirtyMu.Unlock()
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO custom_sections
		(agent_name, section_id, title, content, collapsed, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	// Write all dirty sections
	s.mu.RLock()
	for agentName, sections := range toFlush {
		for sectionID := range sections {
			if sec := s.cache[agentName][sectionID]; sec != nil {
				if _, err := stmt.Exec(agentName, sectionID, sec.Title, sec.Content, sec.Collapsed); err != nil {
					s.mu.RUnlock()
					tx.Rollback()
					return err
				}
			}
		}
	}
	s.mu.RUnlock()

	return tx.Commit()
}

// Close flushes any remaining dirty sections
// Note: Does not close the database connection as it's owned by the caller
func (s *SectionStore) Close() error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	s.closeMu.Unlock()

	// Stop background flusher and wait for it
	close(s.stopFlush)
	s.flushDone.Wait()

	return nil
}

// Stats returns statistics about the section store
func (s *SectionStore) Stats() SectionStoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := SectionStoreStats{
		TotalAgents:   len(s.cache),
		TotalSections: 0,
	}

	for _, sections := range s.cache {
		stats.TotalSections += len(sections)
	}

	s.dirtyMu.Lock()
	for _, sections := range s.dirty {
		stats.DirtySections += len(sections)
	}
	s.dirtyMu.Unlock()

	return stats
}

// SectionStoreStats contains statistics about the section store
type SectionStoreStats struct {
	TotalAgents   int
	TotalSections int
	DirtySections int
}
