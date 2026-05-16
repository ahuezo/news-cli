package fetchlock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireTimesOutWhenHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "news.db.fetch.lock")

	first, err := Acquire(path, 0)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer first.Release()

	_, err = Acquire(path, 25*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Acquire() error = %v, want ErrTimeout", err)
	}
}

func TestAcquireAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "news.db.fetch.lock")

	first, err := Acquire(path, 0)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	second, err := Acquire(path, 0)
	if err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
	defer second.Release()
}
