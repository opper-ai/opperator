package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"opperator/internal/agent"
	"opperator/config"
	"opperator/internal/credentials"
	"opperator/internal/daemon"
	"opperator/internal/ipc"
	"opperator/internal/onboarding"
)

type Status string

const (
	StatusOK   Status = "OK"
	StatusWarn Status = "WARN"
	StatusFail Status = "FAIL"
)

type CheckResult struct {
	Name    string
	Status  Status
	Summary string
	Details []string
	Actions []string
}

type Report struct {
	Checks []CheckResult
}

func (r Report) HasFailures() bool {
	for _, check := range r.Checks {
		if check.Status == StatusFail {
			return true
		}
	}
	return false
}

func (r Report) ExitCode() int {
	if r.HasFailures() {
		return 1
	}
	return 0
}

type metadataInfo struct {
	executablePath string
}

type configInfo struct {
	dir        string
	file       string
	agentConfs []agent.AgentConfig
}

type daemonInfo struct {
	running    bool
	pid        int
	socketPath string
}

func GenerateReport() Report {
	var checks []CheckResult

	metaResult, metaInfo := checkMetadata()
	checks = append(checks, metaResult)

	configResult, cfgInfo := checkConfig()
	checks = append(checks, configResult)

	secretsResult := checkSecrets()
	checks = append(checks, secretsResult)

	daemonResult, dInfo := checkDaemon()
	checks = append(checks, daemonResult)

	agentResult := checkAgentRuntime(dInfo)
	checks = append(checks, agentResult)

	onboardingResult := checkOnboarding()
	checks = append(checks, onboardingResult)

	datastoreResult := checkDataStore()
	checks = append(checks, datastoreResult)

	envResult := checkEnvironment(metaInfo, cfgInfo)
	checks = append(checks, envResult)

	return Report{Checks: checks}
}

func checkMetadata() (CheckResult, *metadataInfo) {
	result := CheckResult{Name: "Runtime Metadata", Status: StatusOK}

	info := &metadataInfo{}

	execPath, err := os.Executable()
	if err != nil {
		result.Status = StatusWarn
		result.Summary = "Could not resolve executable path"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "re-run from installed binary path")
		return result, nil
	}
	info.executablePath = execPath

	buildInfo, ok := debug.ReadBuildInfo()
	goVersion := runtime.Version()
	summaryParts := []string{fmt.Sprintf("go runtime %s", goVersion)}
	if ok && buildInfo != nil {
		if buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
			summaryParts = append(summaryParts, fmt.Sprintf("module %s", buildInfo.Main.Version))
		}
		if buildInfo.Main.Sum != "" {
			summaryParts = append(summaryParts, fmt.Sprintf("sum %s", buildInfo.Main.Sum))
		}
	}

	result.Summary = strings.Join(summaryParts, ", ")
	result.Details = append(result.Details,
		fmt.Sprintf("Executable: %s", execPath),
		fmt.Sprintf("OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH),
	)

	if ok && buildInfo != nil {
		if buildInfo.Main.Path != "" {
			result.Details = append(result.Details, fmt.Sprintf("Module: %s", buildInfo.Main.Path))
		}
		if buildInfo.Main.Version == "(devel)" {
			result.Details = append(result.Details, "Build from local sources")
		}
		if buildInfo.Settings != nil {
			for _, setting := range buildInfo.Settings {
				if setting.Key == "vcs.revision" && setting.Value != "" {
					result.Details = append(result.Details, fmt.Sprintf("VCS Revision: %s", setting.Value))
				}
				if setting.Key == "vcs.time" && setting.Value != "" {
					result.Details = append(result.Details, fmt.Sprintf("VCS Time: %s", setting.Value))
				}
			}
		}
	}

	return result, info
}

func checkConfig() (CheckResult, *configInfo) {
	result := CheckResult{Name: "Configuration", Status: StatusOK}
	info := &configInfo{}

	configDir, err := config.GetConfigDir()
	if err != nil {
		result.Status = StatusFail
		result.Summary = "Unable to resolve config directory"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "verify HOME is set and accessible")
		return result, nil
	}
	info.dir = configDir
	result.Details = append(result.Details, fmt.Sprintf("Config directory: %s", configDir))

	stat, err := os.Stat(configDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = StatusWarn
			result.Summary = "Config directory missing"
			result.Actions = append(result.Actions, "run 'op setup' to create defaults")
		} else {
			result.Status = StatusFail
			result.Summary = "Cannot access config directory"
			result.Details = append(result.Details, err.Error())
			result.Actions = append(result.Actions, "fix permissions on config directory")
		}
		return result, info
	}
	if !stat.IsDir() {
		result.Status = StatusFail
		result.Summary = "Config path is not a directory"
		result.Actions = append(result.Actions, "remove conflicting file and rerun setup")
		return result, info
	}

	if err := checkDirWritable(configDir); err != nil {
		result.Status = StatusWarn
		result.Details = append(result.Details, fmt.Sprintf("Directory not writable: %v", err))
		result.Actions = append(result.Actions, "adjust permissions so opperator can write config")
	}

	configFile, err := config.GetConfigFile()
	if err != nil {
		result.Status = StatusFail
		result.Summary = "Unable to resolve agents config file"
		result.Details = append(result.Details, err.Error())
		return result, info
	}
	info.file = configFile
	result.Details = append(result.Details, fmt.Sprintf("Agents file: %s", configFile))

	if _, err := os.Stat(configFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = StatusWarn
			result.Summary = "agents.yaml not found"
			result.Actions = append(result.Actions, "run 'op setup' or create agents.yaml")
			return result, info
		}
		result.Status = StatusFail
		result.Summary = "Unable to read agents.yaml"
		result.Details = append(result.Details, err.Error())
		return result, info
	}

	cfg, err := agent.LoadConfig(configFile)
	if err != nil {
		result.Status = StatusFail
		result.Summary = "Failed to parse agents.yaml"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "fix YAML syntax in agents.yaml")
		return result, info
	}

	info.agentConfs = cfg.Agents

	agentCount := len(cfg.Agents)
	result.Summary = fmt.Sprintf("Config loaded (%d agents)", agentCount)
	if agentCount == 0 {
		result.Status = StatusWarn
		result.Details = append(result.Details, "No agents configured yet")
		result.Actions = append(result.Actions, "add agents to agents.yaml or run setup wizard")
	}

	return result, info
}

func checkDirWritable(dir string) error {
	file, err := os.CreateTemp(dir, "doctor-")
	if err != nil {
		return err
	}
	name := file.Name()
	file.Close()
	if err := os.Remove(name); err != nil {
		return err
	}
	return nil
}

func checkSecrets() CheckResult {
	result := CheckResult{Name: "OPPER Authentication Status", Status: StatusOK}

	exists, err := credentials.HasAPIKey()
	if err != nil {
		result.Status = StatusFail
		result.Summary = "Unable to access system keyring"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "confirm keyring backend is available")
		return result
	}

	if exists {
		result.Summary = "OPPER_API_KEY is stored"
	} else {
		result.Status = StatusWarn
		result.Summary = "OPPER_API_KEY not configured"
		result.Actions = append(result.Actions, "run 'op setup' to configure authentication")
	}

	return result
}

func checkDaemon() (CheckResult, *daemonInfo) {
	result := CheckResult{Name: "Daemon", Status: StatusOK}
	info := &daemonInfo{}

	socketPath, err := config.GetSocketPath()
	if err != nil {
		result.Status = StatusFail
		result.Summary = "Unable to resolve daemon socket"
		result.Details = append(result.Details, err.Error())
		return result, nil
	}
	info.socketPath = socketPath

	running := daemon.IsRunning()
	info.running = running

	socketExists := false
	if stat, err := os.Stat(socketPath); err == nil && !stat.IsDir() {
		socketExists = true
	}

	pid, err := daemon.ReadPIDFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			pid = 0
		} else {
			result.Status = StatusWarn
			result.Details = append(result.Details, fmt.Sprintf("PID file error: %v", err))
		}
	}
	info.pid = pid

	if running {
		result.Summary = "Daemon is running"
		if pid > 0 {
			if err := verifyPIDAlive(pid); err != nil {
				result.Status = StatusWarn
				result.Details = append(result.Details, fmt.Sprintf("PID %d not responding: %v", pid, err))
				result.Actions = append(result.Actions, "restart daemon with 'op daemon stop && op daemon start'")
			} else {
				result.Details = append(result.Details, fmt.Sprintf("PID: %d", pid))
			}
		} else {
			result.Status = StatusWarn
			result.Details = append(result.Details, "Daemon pid file missing or unreadable")
			result.Actions = append(result.Actions, "restart daemon to regenerate pid file")
		}
	} else {
		result.Status = StatusWarn
		result.Summary = "Daemon is not running"
		result.Actions = append(result.Actions, "run 'op daemon start'")
	}

	if socketExists {
		result.Details = append(result.Details, fmt.Sprintf("Socket: %s", socketPath))
	} else {
		result.Details = append(result.Details, fmt.Sprintf("Socket missing: %s", socketPath))
		if running {
			result.Status = StatusWarn
			result.Actions = append(result.Actions, "clean up stale socket and restart daemon")
		}
	}

	return result, info
}

func verifyPIDAlive(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(syscall.Signal(0))
}

func checkAgentRuntime(info *daemonInfo) CheckResult {
	result := CheckResult{Name: "Agent Runtime", Status: StatusOK}

	if info == nil || !info.running {
		result.Status = StatusWarn
		result.Summary = "Daemon offline; agent status unavailable"
		result.Actions = append(result.Actions, "start daemon to inspect agents")
		return result
	}

	client, err := ipc.NewClient(info.socketPath)
	if err != nil {
		result.Status = StatusWarn
		result.Summary = "Unable to connect to daemon"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "restart daemon or check socket permissions")
		return result
	}
	defer client.Close()

	processes, err := client.ListAgents()
	if err != nil {
		result.Status = StatusWarn
		result.Summary = "Failed to list agents"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "check daemon logs for details")
		return result
	}

	total := len(processes)
	statusCounts := map[string]int{}
	crashed := []string{}
	runningList := []string{}

	for _, proc := range processes {
		key := string(proc.Status)
		statusCounts[key]++
		if proc.Status == agent.StatusCrashed {
			crashed = append(crashed, proc.Name)
		}
		if proc.Status == agent.StatusRunning {
			runningList = append(runningList, proc.Name)
		}
	}

	result.Summary = fmt.Sprintf("Daemon returned %d agent(s)", total)
	if total == 0 {
		result.Status = StatusWarn
		result.Details = append(result.Details, "No agents registered with daemon")
		result.Actions = append(result.Actions, "reload config or add agents")
		return result
	}

	// Compose details
	var detailParts []string
	for _, name := range []string{"running", "stopped", "crashed", "stopping"} {
		if count, ok := statusCounts[name]; ok && count > 0 {
			detailParts = append(detailParts, fmt.Sprintf("%s=%d", name, count))
		}
	}
	if len(detailParts) > 0 {
		result.Details = append(result.Details, fmt.Sprintf("Status counts: %s", strings.Join(detailParts, ", ")))
	}
	if len(runningList) > 0 {
		result.Details = append(result.Details, fmt.Sprintf("Running: %s", strings.Join(runningList, ", ")))
	}
	if len(crashed) > 0 {
		result.Status = StatusWarn
		result.Details = append(result.Details, fmt.Sprintf("Crashed: %s", strings.Join(crashed, ", ")))
		result.Actions = append(result.Actions, "inspect agent logs with 'op agent logs <agent-name>'")
	}

	return result
}

func checkOnboarding() CheckResult {
	result := CheckResult{Name: "Onboarding", Status: StatusOK}

	prefs, err := onboarding.LoadPreferences()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = StatusWarn
			result.Summary = "Onboarding preferences not found"
			result.Actions = append(result.Actions, "run 'op setup' to configure preferences")
			return result
		}
		result.Status = StatusWarn
		result.Summary = "Unable to read onboarding preferences"
		result.Details = append(result.Details, err.Error())
		result.Actions = append(result.Actions, "rerun 'op setup' to regenerate preferences")
		return result
	}

	if prefs.OnboardingComplete {
		result.Summary = "Onboarding complete"
	} else {
		result.Status = StatusWarn
		result.Summary = "Onboarding incomplete"
		result.Actions = append(result.Actions, "run 'op setup' to finish configuration")
	}

	return result
}

func checkDataStore() CheckResult {
	result := CheckResult{Name: "Data Store", Status: StatusOK}

	dbPath, err := config.GetDatabasePath()
	if err != nil {
		result.Status = StatusWarn
		result.Summary = "Unable to resolve database path"
		result.Details = append(result.Details, err.Error())
		return result
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = StatusWarn
			result.Summary = "Database file not initialized"
			result.Actions = append(result.Actions, "run 'op' to create opperator.db")
			return result
		}
		result.Status = StatusWarn
		result.Summary = "Cannot read conversation database"
		result.Details = append(result.Details, err.Error())
		return result
	}

	result.Summary = "Database available"
	result.Details = append(result.Details,
		fmt.Sprintf("Path: %s", dbPath),
		fmt.Sprintf("Size: %s", formatBytes(info.Size())),
		fmt.Sprintf("Last modified: %s", info.ModTime().Format(time.RFC3339)),
	)

	return result
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

func checkEnvironment(meta *metadataInfo, cfg *configInfo) CheckResult {
	result := CheckResult{Name: "Environment", Status: StatusOK}

	requiresPython := false
	if cfg != nil {
		for _, agent := range cfg.agentConfs {
			// Check if the command contains "python" to determine if python is required
			if strings.Contains(strings.ToLower(agent.Command), "python") {
				requiresPython = true
				break
			}
		}
	}

	if requiresPython {
		if _, err := exec.LookPath("python3"); err != nil {
			result.Status = StatusWarn
			result.Summary = "python3 not found in PATH"
			result.Actions = append(result.Actions, "install python3 or adjust PATH for python agents")
		} else {
			result.Details = append(result.Details, "python3 available in PATH")
			result.Summary = "Environment prerequisites satisfied"
		}
	} else {
		result.Summary = "Environment prerequisites satisfied"
	}

	return result
}
