package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"opperator/config"
)

// IsRunning reports whether a daemon is listening on the configured socket.
func IsRunning() bool {
	socketPath, err := config.GetSocketPath()
	if err != nil {
		return false
	}

	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// WritePIDFile writes the current process PID to the configured PID file.
func WritePIDFile() error {
	pidFile, err := config.GetPIDFile()
	if err != nil {
		return err
	}
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

// ReadPIDFile reads the PID from the configured PID file.
func ReadPIDFile() (int, error) {
	pidFile, err := config.GetPIDFile()
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0, fmt.Errorf("pid file %s is empty", pidFile)
	}

	pid, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid pid in %s: %w", pidFile, err)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid %d in %s", pid, pidFile)
	}

	return pid, nil
}

func CleanupStaleFiles() error {
	socketPath, err := config.GetSocketPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(socketPath); err == nil {
		conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
		if err != nil {
			if removeErr := os.Remove(socketPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("remove stale socket: %w", removeErr)
			}
		} else {
			conn.Close()
			return fmt.Errorf("daemon is actually running")
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat socket: %w", err)
	}

	pidFile, err := config.GetPIDFile()
	if err != nil {
		return err
	}

	pid, err := ReadPIDFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if removeErr := os.Remove(pidFile); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove invalid pid file: %w", removeErr)
		}
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		if removeErr := os.Remove(pidFile); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove stale pid file: %w", removeErr)
		}
		return nil
	}

	if err := process.Signal(syscall.Signal(0)); err != nil {
		if removeErr := os.Remove(pidFile); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove stale pid file: %w", removeErr)
		}
	}

	return nil
}
