package lock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"wktbox/internal/lock"
)

func TestGlobalLockExcludesConcurrentHolder(t *testing.T) {
	manager := lock.NewManager(t.TempDir())
	unlockFirst, err := manager.Global(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFirst()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = manager.Global(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestUnlockIsIdempotentAndAllowsNextHolder(t *testing.T) {
	manager := lock.NewManager(t.TempDir())
	unlock, err := manager.Box(context.Background(), "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}

	unlockAgain, err := manager.Box(context.Background(), "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	if err := unlockAgain(); err != nil {
		t.Fatal(err)
	}
}

func TestBoxLockRejectsUnsafeID(t *testing.T) {
	_, err := lock.NewManager(t.TempDir()).Box(context.Background(), "../other")
	if !errors.Is(err, lock.ErrInvalidBoxID) {
		t.Fatalf("error = %v", err)
	}
}
