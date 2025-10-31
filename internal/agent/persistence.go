package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type AgentPersistentData struct {
	Name         string    `json:"name"`
	RestartCount int       `json:"restart_count"`
	TotalRuntime int64     `json:"total_runtime_seconds"`
	LastStarted  time.Time `json:"last_started"`
	LastStopped  time.Time `json:"last_stopped"`
	CrashCount   int       `json:"crash_count"`
	WasRunning   bool      `json:"was_running"` // Whether agent was running when daemon last stopped
}

// AgentPersistence manages persistent storage for agent data
type AgentPersistence struct {
	dataFile string
	logDir   string
	db       *sql.DB
	data     map[string]*AgentPersistentData
	mu       sync.RWMutex
}

func NewAgentPersistence(configDir string, db *sql.DB) *AgentPersistence {
	dataFile := filepath.Join(configDir, "agent_data.json")
	logDir := filepath.Join(configDir, "logs")

	p := &AgentPersistence{
		dataFile: dataFile,
		logDir:   logDir,
		db:       db,
		data:     make(map[string]*AgentPersistentData),
	}

	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("Warning: failed to create logs directory: %v", err)
	}

	p.load()
	return p
}

func (p *AgentPersistence) GetAgentData(agentName string) *AgentPersistentData {
	p.mu.Lock()
	defer p.mu.Unlock()

	if data, exists := p.data[agentName]; exists {
		return data
	}

	data := &AgentPersistentData{
		Name:        agentName,
		LastStarted: time.Time{},
		LastStopped: time.Time{},
	}
	p.data[agentName] = data
	return data
}

// RecordStart records when an agent starts
func (p *AgentPersistence) RecordStart(agentName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data := p.getOrCreateData(agentName)
	data.LastStarted = time.Now()
	p.saveAsync()
}

// RecordStop records when an agent stops (graceful)
func (p *AgentPersistence) RecordStop(agentName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data := p.getOrCreateData(agentName)
	if !data.LastStarted.IsZero() {
		runtime := time.Since(data.LastStarted)
		data.TotalRuntime += int64(runtime.Seconds())
	}
	data.LastStopped = time.Now()
	data.WasRunning = false
	p.saveAsync()
}

// RecordCrash records when an agent crashes and increments restart count
func (p *AgentPersistence) RecordCrash(agentName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data := p.getOrCreateData(agentName)
	if !data.LastStarted.IsZero() {
		runtime := time.Since(data.LastStarted)
		data.TotalRuntime += int64(runtime.Seconds())
	}
	data.CrashCount++
	data.RestartCount++
	data.LastStopped = time.Now()
	data.WasRunning = false
	p.saveAsync()
}

func (p *AgentPersistence) AddLog(agentName string, logLine string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Write to database
	if p.db != nil {
		_, err := p.db.Exec(
			`INSERT INTO agent_logs (agent_name, log_line, created_at) VALUES (?, ?, ?)`,
			agentName, logLine, time.Now().Unix(),
		)
		if err != nil {
			log.Printf("Warning: failed to write log to database: %v", err)
		} else {
			// Trim old logs asynchronously
			go p.trimDatabaseLogs(agentName)
		}
	}

	// Still write to disk as backup
	p.writeLogToDisk(agentName, logLine)
}

func (p *AgentPersistence) GetLogs(agentName string, maxLines int) []string {
	// Try to get logs from database first
	if p.db != nil {
		rows, err := p.db.Query(`
			SELECT log_line
			FROM agent_logs
			WHERE agent_name = ?
			ORDER BY id DESC
			LIMIT ?
		`, agentName, maxLines)

		if err == nil {
			defer rows.Close()
			var logs []string
			for rows.Next() {
				var logLine string
				if err := rows.Scan(&logLine); err == nil {
					logs = append(logs, logLine)
				}
			}

			// Reverse to get chronological order
			for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
				logs[i], logs[j] = logs[j], logs[i]
			}

			if len(logs) > 0 {
				return logs
			}
		} else {
			log.Printf("Warning: failed to query logs from database: %v", err)
		}
	}

	// Fallback to disk logs if database query failed or returned no results
	return p.readLogsFromDisk(agentName, maxLines)
}

func (p *AgentPersistence) GetRestartCount(agentName string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if data, exists := p.data[agentName]; exists {
		return data.RestartCount
	}
	return 0
}

func (p *AgentPersistence) GetTotalRuntime(agentName string) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	data := p.getOrCreateData(agentName)
	totalRuntime := data.TotalRuntime

	if !data.LastStarted.IsZero() && data.LastStarted.After(data.LastStopped) {
		totalRuntime += int64(time.Since(data.LastStarted).Seconds())
	}

	return totalRuntime
}

// RecordRunning marks an agent as currently running
func (p *AgentPersistence) RecordRunning(agentName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data := p.getOrCreateData(agentName)
	data.WasRunning = true
	p.saveAsync()
}

func (p *AgentPersistence) GetPreviouslyRunningAgents() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var runningAgents []string
	for agentName, data := range p.data {
		if data.WasRunning {
			runningAgents = append(runningAgents, agentName)
		}
	}
	return runningAgents
}

// SnapshotRunningAgents captures which agents are currently running
// Call this before stopping agents during daemon shutdown
func (p *AgentPersistence) SnapshotRunningAgents(runningAgentNames []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// First, clear all was_running flags
	for _, data := range p.data {
		data.WasRunning = false
	}

	for _, agentName := range runningAgentNames {
		data := p.getOrCreateData(agentName)
		data.WasRunning = true
	}

	p.saveAsync()
}


func (p *AgentPersistence) getOrCreateData(agentName string) *AgentPersistentData {
	if data, exists := p.data[agentName]; exists {
		return data
	}

	data := &AgentPersistentData{
		Name:        agentName,
		LastStarted: time.Time{},
		LastStopped: time.Time{},
	}
	p.data[agentName] = data
	return data
}

// trimDatabaseLogs keeps only the last 10,000 logs per agent in the database
func (p *AgentPersistence) trimDatabaseLogs(agentName string) {
	if p.db == nil {
		return
	}

	_, err := p.db.Exec(`
		DELETE FROM agent_logs
		WHERE agent_name = ?
		AND id NOT IN (
			SELECT id FROM agent_logs
			WHERE agent_name = ?
			ORDER BY id DESC
			LIMIT 10000
		)
	`, agentName, agentName)

	if err != nil {
		log.Printf("Warning: failed to trim database logs for %s: %v", agentName, err)
	}
}

func (p *AgentPersistence) writeLogToDisk(agentName string, logLine string) {
	logFile := filepath.Join(p.logDir, fmt.Sprintf("%s.log", agentName))

	timestamped := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), logLine)

	// Read existing lines to check if we need to truncate
	existingLines := p.readAllLinesFromDisk(logFile)

	maxLines := 10000
	if len(existingLines) >= maxLines {
		// Keep only the last (maxLines-1) lines to make room for the new one
		keepLines := maxLines - 1
		existingLines = existingLines[len(existingLines)-keepLines:]

		// Rewrite the entire file with truncated content
		if err := p.rewriteLogFile(logFile, existingLines); err != nil {
			log.Printf("Warning: failed to truncate log file %s: %v", logFile, err)
		}
	}

	// Append the new line
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Warning: failed to open log file %s: %v", logFile, err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(timestamped); err != nil {
		log.Printf("Warning: failed to write to log file %s: %v", logFile, err)
	}
}

func (p *AgentPersistence) readAllLinesFromDisk(logFile string) []string {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return []string{} // File doesn't exist or can't be read
	}

	// Split by newlines and filter empty lines
	allLines := strings.Split(string(data), "\n")
	lines := []string{}
	for _, line := range allLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

func (p *AgentPersistence) rewriteLogFile(logFile string, lines []string) error {
	tempFile := logFile + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write all lines
	for _, line := range lines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			os.Remove(tempFile) // Cleanup on error
			return err
		}
	}

	// Atomically replace original file
	return os.Rename(tempFile, logFile)
}

func (p *AgentPersistence) readLogsFromDisk(agentName string, maxLines int) []string {
	logFile := filepath.Join(p.logDir, fmt.Sprintf("%s.log", agentName))

	data, err := os.ReadFile(logFile)
	if err != nil {
		return []string{} // File doesn't exist or can't be read
	}

	// Split by newlines and filter empty lines
	allLines := strings.Split(string(data), "\n")
	lines := []string{}
	for _, line := range allLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}

	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return lines
}

func (p *AgentPersistence) load() {
	data, err := os.ReadFile(p.dataFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: failed to read agent data file: %v", err)
		}
		return
	}

	var loadedData map[string]*AgentPersistentData
	if err := json.Unmarshal(data, &loadedData); err != nil {
		log.Printf("Warning: failed to parse agent data file: %v", err)
		return
	}

	p.mu.Lock()
	p.data = loadedData
	p.mu.Unlock()

	log.Printf("Loaded persistent data for %d agents", len(loadedData))
}

func (p *AgentPersistence) save() error {
	p.mu.RLock()
	dataCopy := make(map[string]*AgentPersistentData)
	for k, v := range p.data {
		dataCopy[k] = v
	}
	p.mu.RUnlock()

	data, err := json.MarshalIndent(dataCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent data: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(p.dataFile), 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := os.WriteFile(p.dataFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent data file: %w", err)
	}

	return nil
}

func (p *AgentPersistence) saveAsync() {
	go func() {
		if err := p.save(); err != nil {
			log.Printf("Warning: failed to save agent data: %v", err)
		}
	}()
}

// DeleteAgentData removes an agent's persistent data
func (p *AgentPersistence) DeleteAgentData(agentName string) error {
	p.mu.Lock()
	delete(p.data, agentName)

	// Make a copy of data while holding the lock
	dataCopy := make(map[string]*AgentPersistentData)
	for k, v := range p.data {
		dataCopy[k] = v
	}
	p.mu.Unlock()

	// Now save without holding the lock
	return p.saveData(dataCopy)
}

// saveData saves the given data to disk without acquiring locks
func (p *AgentPersistence) saveData(data map[string]*AgentPersistentData) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent data: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(p.dataFile), 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := os.WriteFile(p.dataFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write agent data file: %w", err)
	}

	return nil
}

// GetDB returns the database connection
func (p *AgentPersistence) GetDB() *sql.DB {
	return p.db
}

