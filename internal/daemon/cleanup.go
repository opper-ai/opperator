package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// findOrphanProcesses finds processes that are children of the daemon or match opperator agent paths
func findOrphanProcesses(daemonPID int) []int {
	var orphans []int

	// Get the name of our binary
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: could not get executable path: %v", err)
		return orphans
	}
	binaryName := filepath.Base(exePath)

	// Use ps with full command line to find opperator-related processes
	// -ww ensures we get the full command line
	cmd := exec.Command("ps", "-eo", "pid,ppid,command", "-ww")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Warning: failed to run ps command: %v", err)
		return orphans
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		// Parse PID and PPID
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		// Skip our own process and the daemon itself
		if pid == os.Getpid() || pid == daemonPID {
			continue
		}

		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		// Get the full command line (everything after PID and PPID)
		commandLine := strings.Join(fields[2:], " ")

		// Check if this is an orphan we should kill
		isOrphan := false

		// Check if it's a direct child of the daemon
		if daemonPID > 0 && ppid == daemonPID {
			isOrphan = true
			log.Printf("Found orphan process (daemon child): PID=%d PPID=%d CMD=%s", pid, ppid, commandLine)
		}

		// Check if it's an opperator binary
		if strings.Contains(commandLine, binaryName) && pid != daemonPID {
			isOrphan = true
			log.Printf("Found orphan process (binary match): PID=%d PPID=%d CMD=%s", pid, ppid, commandLine)
		}

		// Check if it's running from the opperator agents directory
		if strings.Contains(commandLine, "/.config/opperator/agents/") ||
			strings.Contains(commandLine, ".config/opperator/agents/") {
			isOrphan = true
			log.Printf("Found orphan process (agent path): PID=%d PPID=%d CMD=%s", pid, ppid, commandLine)
		}

		if isOrphan && !contains(orphans, pid) {
			orphans = append(orphans, pid)
		}
	}

	log.Printf("Found %d total orphan processes", len(orphans))
	return orphans
}

// killProcessGroup sends a signal to an entire process group
func killProcessGroup(pgid int, signal syscall.Signal) error {
	if pgid <= 0 {
		return fmt.Errorf("invalid process group id: %d", pgid)
	}

	// Send signal to negative PID to target the process group
	log.Printf("Sending signal %v to process group %d", signal, pgid)
	err := syscall.Kill(-pgid, signal)
	if err != nil {
		// ESRCH means no such process, which is fine (already dead)
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("failed to signal process group %d: %w", pgid, err)
	}

	return nil
}

// killProcess sends a signal to a single process
func killProcess(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process id: %d", pid)
	}

	log.Printf("Sending signal %v to process %d", signal, pid)
	err := syscall.Kill(pid, signal)
	if err != nil {
		// ESRCH means no such process, which is fine (already dead)
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("failed to signal process %d: %w", pid, err)
	}

	return nil
}

// waitForProcessExit waits for a process to exit, returns true if it exited within timeout
func waitForProcessExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if process still exists
		process, err := os.FindProcess(pid)
		if err != nil {
			// Process doesn't exist
			return true
		}

		// Send signal 0 to check if process is alive
		err = process.Signal(syscall.Signal(0))
		if err != nil {
			// Process is dead (ESRCH) or we can't signal it
			return true
		}

		// Process still exists, wait a bit
		time.Sleep(100 * time.Millisecond)
	}

	return false
}

// isProcessRunning checks if a process is still running
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// verifyNoOrphans checks if any opperator processes are still running (except current process)
func verifyNoOrphans() ([]int, error) {
	// Get the name of our binary
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("could not get executable path: %w", err)
	}
	binaryName := filepath.Base(exePath)

	var remainingProcesses []int

	// Use ps with full command line to find opperator-related processes
	cmd := exec.Command("ps", "-eo", "pid,command", "-ww")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ps command: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		// Skip our own process
		if pid == os.Getpid() {
			continue
		}

		// Get the full command line
		commandLine := strings.Join(fields[1:], " ")

		// Check if it's an opperator-related process
		isOpperator := false

		// Check if it's an opperator binary
		if strings.Contains(commandLine, binaryName) {
			isOpperator = true
		}

		// Check if it's running from the opperator agents directory
		if strings.Contains(commandLine, "/.config/opperator/agents/") ||
			strings.Contains(commandLine, ".config/opperator/agents/") {
			isOpperator = true
		}

		if isOpperator {
			remainingProcesses = append(remainingProcesses, pid)
			log.Printf("Found remaining opperator process: PID=%d CMD=%s", pid, commandLine)
		}
	}

	return remainingProcesses, nil
}

// shutdownViaIPC attempts to gracefully shutdown the daemon via IPC
func shutdownViaIPC(timeout time.Duration) error {
	// This will be implemented to send a shutdown request via the IPC socket
	// For now, we'll return an error to fall back to signal-based shutdown
	log.Printf("Attempting graceful shutdown via IPC (timeout: %v)", timeout)

	// Try to send shutdown request
	// Note: We could implement this by sending a RequestShutdown IPC message
	// but for simplicity, we'll rely on signal-based shutdown

	return fmt.Errorf("IPC shutdown not implemented, falling back to signals")
}

// contains checks if a slice contains a value
func contains(slice []int, value int) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// StopDaemonComprehensive performs a comprehensive multi-layer daemon shutdown
func StopDaemonComprehensive(daemonPID int) error {
	log.Printf("=== Starting comprehensive daemon shutdown (PID: %d) ===", daemonPID)

	// Layer 1: Try IPC graceful shutdown (5 second timeout)
	log.Printf("Layer 1: Attempting IPC graceful shutdown...")
	err := shutdownViaIPC(5 * time.Second)
	if err == nil {
		log.Printf("Layer 1: IPC shutdown successful")
		if waitForProcessExit(daemonPID, 5*time.Second) {
			log.Printf("Daemon exited gracefully via IPC")
			return nil
		}
	} else {
		log.Printf("Layer 1: IPC shutdown failed: %v", err)
	}

	// Check if daemon is still running
	if !isProcessRunning(daemonPID) {
		log.Printf("Daemon already stopped after Layer 1")
		return nil
	}

	// Layer 2: Send SIGTERM to process group (5 second timeout)
	log.Printf("Layer 2: Sending SIGTERM to daemon process group...")
	err = killProcessGroup(daemonPID, syscall.SIGTERM)
	if err != nil {
		log.Printf("Layer 2: Failed to send SIGTERM to process group: %v", err)
		// Try sending to just the daemon process
		killProcess(daemonPID, syscall.SIGTERM)
	}

	daemonStopped := false
	if waitForProcessExit(daemonPID, 5*time.Second) {
		log.Printf("Daemon exited gracefully via SIGTERM")
		daemonStopped = true
		// Give a moment for child processes to clean up
		time.Sleep(500 * time.Millisecond)
	}

	// Layer 3: Send SIGKILL if daemon is still running
	if !daemonStopped && isProcessRunning(daemonPID) {
		log.Printf("Layer 3: Daemon did not respond to SIGTERM, sending SIGKILL...")
		err = killProcessGroup(daemonPID, syscall.SIGKILL)
		if err != nil {
			log.Printf("Layer 3: Failed to send SIGKILL to process group: %v", err)
			// Try sending to just the daemon process
			killProcess(daemonPID, syscall.SIGKILL)
		}

		if waitForProcessExit(daemonPID, 2*time.Second) {
			log.Printf("Daemon killed via SIGKILL")
			// Give a moment for kernel to clean up
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Layer 4: Hunt and kill orphan processes
	log.Printf("Layer 4: Searching for orphan processes...")
	orphans := findOrphanProcesses(daemonPID)

	if len(orphans) > 0 {
		log.Printf("Found %d orphan process(es), killing them...", len(orphans))
		for _, orphanPID := range orphans {
			log.Printf("Killing orphan process: PID=%d", orphanPID)
			// Try SIGTERM first
			killProcess(orphanPID, syscall.SIGTERM)
		}

		// Wait a bit for graceful termination
		time.Sleep(1 * time.Second)

		// Force kill any that didn't die
		for _, orphanPID := range orphans {
			if isProcessRunning(orphanPID) {
				log.Printf("Force killing stubborn orphan process: PID=%d", orphanPID)
				killProcess(orphanPID, syscall.SIGKILL)
			}
		}

		// Wait for them to die
		time.Sleep(500 * time.Millisecond)
	} else {
		log.Printf("No orphan processes found")
	}

	// Layer 5: Verify no processes remain
	log.Printf("Layer 5: Verifying all processes are stopped...")
	remaining, err := verifyNoOrphans()
	if err != nil {
		log.Printf("Warning: Failed to verify no orphans: %v", err)
	} else if len(remaining) > 0 {
		log.Printf("WARNING: %d opperator process(es) still running after cleanup:", len(remaining))
		for _, pid := range remaining {
			log.Printf("  - PID: %d", pid)
		}
		return fmt.Errorf("%d process(es) could not be stopped", len(remaining))
	} else {
		log.Printf("Verification complete: All processes stopped successfully")
	}

	log.Printf("=== Daemon shutdown complete ===")
	return nil
}
