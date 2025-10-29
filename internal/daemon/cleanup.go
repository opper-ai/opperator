package daemon

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)


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

// verifyNoOrphans checks if any opperator agent processes are still running
// Only checks for agents in the configured agents directory, not by binary name matching
func verifyNoOrphans() ([]int, error) {
	var remainingProcesses []int

	// Use ps with full command line to find opperator agent processes
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

		// ONLY check for processes running from the opperator agents directory
		// This is a very specific pattern that indicates an opperator agent
		// We avoid generic binary name matching to prevent false positives
		if strings.Contains(commandLine, "/.config/opperator/agents/") ||
			strings.Contains(commandLine, ".config/opperator/agents/") {
			remainingProcesses = append(remainingProcesses, pid)
			log.Printf("Found remaining opperator agent process: PID=%d CMD=%s", pid, commandLine)
		}
	}

	return remainingProcesses, nil
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

// Shutdown performs a clean multi-layer daemon shutdown
// Uses process group termination to ensure daemon and all children are stopped
func Shutdown(daemonPID int, _ interface{}) error {
	log.Printf("=== Starting daemon shutdown (PID: %d) ===", daemonPID)

	// Check if daemon is already stopped
	if !isProcessRunning(daemonPID) {
		log.Printf("Daemon is not running")
		return nil
	}

	// Send SIGTERM to process group (5 second timeout)
	log.Printf("Sending SIGTERM to daemon process group...")
	err := killProcessGroup(daemonPID, syscall.SIGTERM)
	if err != nil {
		log.Printf("Failed to send SIGTERM to process group: %v", err)
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

	// Send SIGKILL if daemon is still running
	if !daemonStopped && isProcessRunning(daemonPID) {
		log.Printf("Daemon did not respond to SIGTERM, sending SIGKILL...")
		err = killProcessGroup(daemonPID, syscall.SIGKILL)
		if err != nil {
			log.Printf("Failed to send SIGKILL to process group: %v", err)
			// Try sending to just the daemon process
			killProcess(daemonPID, syscall.SIGKILL)
		}

		if waitForProcessExit(daemonPID, 2*time.Second) {
			log.Printf("Daemon killed via SIGKILL")
			// Give a moment for kernel to clean up
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Verify all daemon child processes are stopped
	// Process group termination handles this automatically since agents are created with Setpgid: true
	log.Printf("Verifying daemon shutdown...")
	remaining, err := verifyNoOrphans()
	if err != nil {
		log.Printf("Warning: Failed to verify daemon shutdown: %v", err)
	} else if len(remaining) > 0 {
		log.Printf("WARNING: %d opperator agent process(es) still running:", len(remaining))
		for _, pid := range remaining {
			log.Printf("  - PID: %d", pid)
		}
		return fmt.Errorf("%d process(es) could not be stopped", len(remaining))
	} else {
		log.Printf("Daemon shutdown complete")
	}
	return nil
}
