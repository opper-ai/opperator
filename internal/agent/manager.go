package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"opperator/internal/protocol"
	"opperator/pkg/migration"

	_ "modernc.org/sqlite"
)

type StateChangeCallback func(agentName string, changeType string, data interface{})

type Manager struct {
	agents            map[string]*Agent
	config            *Config
	configPath        string
	mu                sync.RWMutex
	stopWatching      chan struct{}
	lastModTime       time.Time
	persistence       *AgentPersistence
	onStateChange     StateChangeCallback
}

func New(configPath string) (*Manager, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		config = &Config{Agents: []AgentConfig{}}
	}

	var modTime time.Time
	if stat, err := os.Stat(configPath); err == nil {
		modTime = stat.ModTime()
	}

	configDir := filepath.Dir(configPath)

	// Open database connection
	dbPath := filepath.Join(configDir, "opperator.db")
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Run migrations
	migrationRunner := migration.NewRunner(db)
	if err := migrationRunner.Run(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	persistence := NewAgentPersistence(configDir, db)

	m := &Manager{
		agents:       make(map[string]*Agent),
		config:       config,
		configPath:   configPath,
		stopWatching: make(chan struct{}),
		lastModTime:  modTime,
		persistence:  persistence,
	}

	for _, agentConfig := range config.Agents {
		if agentConfig.MaxRestarts == 0 && agentConfig.AutoRestart {
			agentConfig.MaxRestarts = 3
		}
		agent := NewAgent(agentConfig, persistence)

		agent.stateChangeNotifier = m.notifyStateChange

		// Restore persistent data
		persistentData := persistence.GetAgentData(agentConfig.Name)
		agent.RestartCount = persistentData.RestartCount

		m.agents[agentConfig.Name] = agent
	}

	// Start config file watcher goroutine
	go m.watchConfigFile()

	return m, nil
}

func (m *Manager) SetStateChangeCallback(callback StateChangeCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStateChange = callback
}

func (m *Manager) notifyStateChange(agentName string, changeType string, data interface{}) {
	m.mu.RLock()
	callback := m.onStateChange
	m.mu.RUnlock()

	if callback != nil {
		callback(agentName, changeType, data)
	}
}

func (m *Manager) GetAgent(name string) (*Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, exists := m.agents[name]
	if !exists {
		return nil, fmt.Errorf("agent %s not found", name)
	}

	return agent, nil
}

func (m *Manager) GetAllAgents() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]*Agent, 0, len(m.agents))

	// Maintain order from config
	for _, agentConfig := range m.config.Agents {
		if a, exists := m.agents[agentConfig.Name]; exists {
			agents = append(agents, a)
		}
	}

	return agents
}

func (m *Manager) StartAgent(name string) error {
	agent, err := m.GetAgent(name)
	if err != nil {
		return err
	}

	return agent.Start()
}

func (m *Manager) StopAgent(name string) error {
	agent, err := m.GetAgent(name)
	if err != nil {
		return err
	}

	return agent.Stop()
}

func (m *Manager) RestartAgent(name string) error {
	agent, err := m.GetAgent(name)
	if err != nil {
		return err
	}

	return agent.Restart()
}

// InvokeCommand sends a command to the specified managed agent and waits for a response.
func (m *Manager) InvokeCommand(name, command string, args map[string]interface{}, workingDir string, timeout time.Duration) (*protocol.ResponseMessage, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	agent, err := m.GetAgent(name)
	if err != nil {
		return nil, err
	}

	// Check and notify invocation directory changes
	if err := agent.CheckAndNotifyInvocationDirChange(workingDir); err != nil {
		// Log the error but don't fail the command
		log.Printf("Failed to notify invocation directory change for agent %s: %v", name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return agent.SendCommand(ctx, command, args, strings.TrimSpace(workingDir))
}

// InvokeCommandAsync sends a command to the agent and forwards progress events via the provided callback.
func (m *Manager) InvokeCommandAsync(name, command string, args map[string]interface{}, workingDir string, timeout time.Duration, progress func(protocol.CommandProgressMessage)) (*protocol.ResponseMessage, error) {
	if timeout <= 0 {
		timeout = 0
	}

	agent, err := m.GetAgent(name)
	if err != nil {
		return nil, err
	}

	// Check and notify invocation directory changes
	if err := agent.CheckAndNotifyInvocationDirChange(workingDir); err != nil {
		// Log the error but don't fail the command
		log.Printf("Failed to notify invocation directory change for agent %s: %v", name, err)
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	return agent.SendCommandWithProgress(ctx, command, args, strings.TrimSpace(workingDir), progress)
}

// ListCommands requests the set of registered command names from the agent.
func (m *Manager) ListCommands(name string, timeout time.Duration) ([]protocol.CommandDescriptor, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	agent, err := m.GetAgent(name)
	if err != nil {
		return nil, err
	}

	cmds := agent.RegisteredCommands()
	if len(cmds) > 0 {
		copyCmds := make([]protocol.CommandDescriptor, len(cmds))
		copy(copyCmds, cmds)
		return copyCmds, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := agent.SendCommand(ctx, "__list_commands", nil, "")
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, fmt.Errorf("agent %s returned no response", name)
	}

	if !resp.Success {
		if resp.Error == "" {
			resp.Error = "command failed"
		}
		return nil, fmt.Errorf("%s", resp.Error)
	}

	if data, err := json.Marshal(resp.Result); err == nil {
		var descriptors []protocol.CommandDescriptor
		if err := json.Unmarshal(data, &descriptors); err == nil && len(descriptors) > 0 {
			return protocol.NormalizeCommandDescriptors(descriptors), nil
		}
	}

	switch resp.Result.(type) {
	case []interface{}, []string:
		return nil, fmt.Errorf("string array command format no longer supported - agent must return CommandDescriptor array")
	case nil:
		return []protocol.CommandDescriptor{}, nil
	default:
		return nil, fmt.Errorf("unexpected command list format: %T", resp.Result)
	}
}

func (m *Manager) StopAll() error {
	m.mu.RLock()
	agents := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	m.mu.RUnlock()

	var lastErr error
	for _, a := range agents {
		if a.GetStatus() == StatusRunning {
			if err := a.Stop(); err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
}

// StopAllPreservingState stops all running agents but preserves their running state for auto-restart
func (m *Manager) StopAllPreservingState() error {
	m.mu.RLock()
	agents := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	m.mu.RUnlock()

	var lastErr error
	for _, a := range agents {
		if a.GetStatus() == StatusRunning {
			if err := a.StopPreservingState(); err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
}

func (m *Manager) Cleanup() {
	// Stop config file watcher
	close(m.stopWatching)

	m.StopAll()
}

func (m *Manager) GetPreviouslyRunningAgents() []string {
	if m.persistence != nil {
		return m.persistence.GetPreviouslyRunningAgents()
	}
	return []string{}
}

// SnapshotRunningAgents captures current running state for daemon restart
func (m *Manager) SnapshotRunningAgents() {
	if m.persistence == nil {
		return
	}

	m.mu.RLock()
	var runningAgents []string
	for name, agent := range m.agents {
		if agent.GetStatus() == StatusRunning {
			runningAgents = append(runningAgents, name)
		}
	}
	m.mu.RUnlock()

	m.persistence.SnapshotRunningAgents(runningAgents)
}

func (m *Manager) AddAgent(config AgentConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent := NewAgent(config, m.persistence)
	agent.stateChangeNotifier = m.notifyStateChange
	// Restore persistent data
	if m.persistence != nil {
		persistentData := m.persistence.GetAgentData(config.Name)
		agent.RestartCount = persistentData.RestartCount
	}
	m.agents[config.Name] = agent
}

func (m *Manager) RemoveAgent(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, exists := m.agents[name]
	if !exists {
		return fmt.Errorf("agent %s not found", name)
	}

	if agent.GetStatus() == StatusRunning {
		agent.Stop()
	}

	delete(m.agents, name)
	return nil
}

// watchConfigFile monitors the configuration file for changes
func (m *Manager) watchConfigFile() {
	if m.configPath == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Error creating config watcher: %v", err)
		return
	}
	defer watcher.Close()

	configDir := filepath.Dir(m.configPath)
	if err := watcher.Add(configDir); err != nil {
		log.Printf("Error watching config directory: %v", err)
		return
	}

	for {
		select {
		case <-m.stopWatching:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Clean(event.Name) != filepath.Clean(m.configPath) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			stat, err := os.Stat(m.configPath)
			if err != nil {
				continue
			}

			if stat.ModTime().After(m.lastModTime) {
				log.Printf("Config file changed, reloading...")
				m.lastModTime = stat.ModTime()

				time.Sleep(100 * time.Millisecond)

				if err := m.ReloadConfig(); err != nil {
					log.Printf("Error reloading config: %v", err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Config watcher error: %v", err)
		}
	}
}

// ReloadConfig reloads the configuration and updates agents
func (m *Manager) ReloadConfig() error {
	newConfig, err := LoadConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Collect metadata changes to notify after releasing lock
	type metadataChange struct {
		agentName string
		update    MetadataUpdate
	}
	var metadataChanges []metadataChange

	m.mu.Lock()

	oldConfig := m.config
	m.config = newConfig

	oldAgents := make(map[string]AgentConfig)
	newAgents := make(map[string]AgentConfig)

	for _, agent := range oldConfig.Agents {
		oldAgents[agent.Name] = agent
	}

	for _, agent := range newConfig.Agents {
		if agent.MaxRestarts == 0 && agent.AutoRestart {
			agent.MaxRestarts = 3
		}
		newAgents[agent.Name] = agent
	}

	for name := range oldAgents {
		if _, exists := newAgents[name]; !exists {
			log.Printf("Removing agent: %s", name)

			if process, exists := m.agents[name]; exists {
				if process.GetStatus() == StatusRunning {
					process.Stop()
				}
				delete(m.agents, name)
			}
		}
	}

	for name, newAgent := range newAgents {
		if oldAgent, existed := oldAgents[name]; existed {
			// Agent existed before - check if config changed
			if !agentConfigEqual(oldAgent, newAgent) {
				log.Printf("Updating agent config: %s", name)

				// Check if only metadata fields changed (description, color, system_prompt)
				metadataChanged := agentMetadataChanged(oldAgent, newAgent)
				configChanged := !agentConfigEqualIgnoringMetadata(oldAgent, newAgent)

				// Stop old process if running and non-metadata config changed
				if agent, exists := m.agents[name]; exists {
					wasRunning := agent.GetStatus() == StatusRunning

					// Only restart if non-metadata config changed
					if configChanged && wasRunning {
						agent.Stop()
					}

					// If non-metadata config changed, create new agent
					if configChanged {
						agent := NewAgent(newAgent, m.persistence)
						agent.stateChangeNotifier = m.notifyStateChange
						// Restore persistent data
						if m.persistence != nil {
							persistentData := m.persistence.GetAgentData(newAgent.Name)
							agent.RestartCount = persistentData.RestartCount
						}
						m.agents[name] = agent

						// Restart if it was running before
						if wasRunning {
							m.agents[name].Start()
						}
					} else if metadataChanged {
						// Only metadata changed - update the agent's config and runtime fields without restart
						agent.Config = newAgent

						// Update runtime metadata fields with proper locking
						agent.mu.Lock()
						agent.description = strings.TrimSpace(newAgent.Description)
						agent.systemPrompt = strings.TrimSpace(newAgent.SystemPrompt)
						agent.color = strings.TrimSpace(newAgent.Color)
						agent.mu.Unlock()

						// Collect metadata change to notify after lock is released
						metadataChanges = append(metadataChanges, metadataChange{
							agentName: name,
							update: MetadataUpdate{
								Description:  newAgent.Description,
								SystemPrompt: newAgent.SystemPrompt,
								Color:        newAgent.Color,
							},
						})
					}
				}
			}
		} else {
			log.Printf("Adding new agent: %s", name)
			agent := NewAgent(newAgent, m.persistence)
			agent.stateChangeNotifier = m.notifyStateChange
			// Restore persistent data
			if m.persistence != nil {
				persistentData := m.persistence.GetAgentData(newAgent.Name)
				agent.RestartCount = persistentData.RestartCount
			}
			m.agents[name] = agent
		}
	}

	m.mu.Unlock()

	// Notify metadata changes after releasing the lock to avoid deadlock
	for _, change := range metadataChanges {
		m.notifyStateChange(change.agentName, "metadata", change.update)
	}

	log.Printf("Configuration reloaded successfully")
	return nil
}

func agentConfigEqual(a, b AgentConfig) bool {
	if a.Name != b.Name || a.Command != b.Command {
		return false
	}

	if a.Description != b.Description {
		return false
	}

	if strings.TrimSpace(a.Color) != strings.TrimSpace(b.Color) {
		return false
	}

	if a.SystemPrompt != b.SystemPrompt {
		return false
	}

	if a.ProcessRoot != b.ProcessRoot || a.AutoRestart != b.AutoRestart {
		return false
	}

	if a.MaxRestarts != b.MaxRestarts {
		return false
	}

	// Compare args
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i, arg := range a.Args {
		if arg != b.Args[i] {
			return false
		}
	}

	// Compare env
	if len(a.Env) != len(b.Env) {
		return false
	}
	for key, val := range a.Env {
		if b.Env[key] != val {
			return false
		}
	}

	// Compare StartWithDaemon
	if (a.StartWithDaemon == nil) != (b.StartWithDaemon == nil) {
		return false
	}
	if a.StartWithDaemon != nil && b.StartWithDaemon != nil {
		if *a.StartWithDaemon != *b.StartWithDaemon {
			return false
		}
	}

	return true
}

// agentMetadataChanged checks if only metadata fields (description, color, system_prompt) changed
func agentMetadataChanged(a, b AgentConfig) bool {
	return a.Description != b.Description ||
		strings.TrimSpace(a.Color) != strings.TrimSpace(b.Color) ||
		a.SystemPrompt != b.SystemPrompt
}

// agentConfigEqualIgnoringMetadata checks if configs are equal ignoring metadata fields
func agentConfigEqualIgnoringMetadata(a, b AgentConfig) bool {
	if a.Name != b.Name || a.Command != b.Command {
		return false
	}

	if a.ProcessRoot != b.ProcessRoot || a.AutoRestart != b.AutoRestart {
		return false
	}

	if a.MaxRestarts != b.MaxRestarts {
		return false
	}

	// Compare args
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i, arg := range a.Args {
		if arg != b.Args[i] {
			return false
		}
	}

	// Compare env
	if len(a.Env) != len(b.Env) {
		return false
	}
	for key, val := range a.Env {
		if b.Env[key] != val {
			return false
		}
	}

	// Compare StartWithDaemon
	if (a.StartWithDaemon == nil) != (b.StartWithDaemon == nil) {
		return false
	}
	if a.StartWithDaemon != nil && b.StartWithDaemon != nil {
		if *a.StartWithDaemon != *b.StartWithDaemon {
			return false
		}
	}

	return true
}

// ReloadConfigManual allows manual triggering of config reload
func (m *Manager) ReloadConfigManual() error {
	log.Printf("Manual config reload requested")
	return m.ReloadConfig()
}

func (m *Manager) GetAgentRuntimeStats(agentName string) (restartCount int, totalRuntime int64, crashCount int) {
	if m.persistence != nil {
		data := m.persistence.GetAgentData(agentName)
		return data.RestartCount, m.persistence.GetTotalRuntime(agentName), data.CrashCount
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if agent, exists := m.agents[agentName]; exists {
		return agent.RestartCount, 0, 0
	}

	return 0, 0, 0
}

// DeleteAgentPersistentData removes an agent's persistent data from agent_data.json
func (m *Manager) DeleteAgentPersistentData(agentName string) error {
	if m.persistence == nil {
		return nil
	}
	return m.persistence.DeleteAgentData(agentName)
}

// GetDB returns the database connection from persistence
func (m *Manager) GetDB() *sql.DB {
	if m.persistence == nil {
		return nil
	}
	return m.persistence.GetDB()
}

// UnregisterAgent removes an agent from the manager without reloading config
func (m *Manager) UnregisterAgent(agentName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, agentName)
	log.Printf("Agent %s unregistered from manager", agentName)
}
