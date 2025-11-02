package daemon

import (
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"

	"opperator/config"
)

// ErrAlreadyRunning is returned when another daemon instance already holds the lock.
var ErrAlreadyRunning = errors.New("daemon already running")

type processLock struct {
	file *os.File
	path string
}

func acquireProcessLock() (*processLock, error) {
	pidFile, err := config.GetPIDFile()
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(pidFile, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			log.Printf("LOCK CONFLICT: Another daemon is holding the lock on %s (pid: %d)", pidFile, os.Getpid())
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("lock pid file: %w", err)
	}

	log.Printf("LOCK ACQUIRED: Process %d successfully acquired lock on %s", os.Getpid(), pidFile)

	lock := &processLock{file: file, path: pidFile}

	if err := file.Truncate(0); err != nil {
		lock.Release()
		return nil, fmt.Errorf("truncate pid file: %w", err)
	}

	if _, err := file.Seek(0, 0); err != nil {
		lock.Release()
		return nil, fmt.Errorf("seek pid file: %w", err)
	}

	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		lock.Release()
		return nil, fmt.Errorf("write pid file: %w", err)
	}

	if err := file.Sync(); err != nil {
		lock.Release()
		return nil, fmt.Errorf("sync pid file: %w", err)
	}

	return lock, nil
}

func (l *processLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	var releaseErr error

	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("unlock pid file: %w", err))
	} else {
		log.Printf("LOCK RELEASED: Process %d released lock on %s", os.Getpid(), l.path)
	}

	if err := l.file.Close(); err != nil {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("close pid file: %w", err))
	}

	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		releaseErr = errors.Join(releaseErr, fmt.Errorf("remove pid file: %w", err))
	}

	l.file = nil
	return releaseErr
}
