package fetchlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrTimeout = errors.New("fetch lock timeout")

// Lock is an advisory process lock used to serialize fetch jobs per database.
type Lock struct {
	file *os.File
	path string
}

func PathForDB(dbPath string) string {
	return filepath.Clean(dbPath) + ".fetch.lock"
}

func AcquireForDB(dbPath string, timeout time.Duration) (*Lock, error) {
	return Acquire(PathForDB(dbPath), timeout)
}

func Acquire(path string, timeout time.Duration) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open fetch lock: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lock{file: file, path: path}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, fmt.Errorf("acquire fetch lock: %w", err)
		}
		if timeout <= 0 || time.Now().After(deadline) {
			file.Close()
			return nil, fmt.Errorf("%w: %s", ErrTimeout, path)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("release fetch lock: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close fetch lock: %w", closeErr)
	}
	return nil
}
