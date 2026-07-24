package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

var (
	ErrBusy         = errors.New("lock is busy")
	ErrInvalidBoxID = errors.New("invalid box ID")
)

var boxIDPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

type UnlockFunc func() error

type Manager struct {
	root string
}

func NewManager(root string) Manager {
	return Manager{root: filepath.Clean(root)}
}

func (manager Manager) Global(ctx context.Context) (UnlockFunc, error) {
	return Acquire(ctx, filepath.Join(manager.root, "state.lock"))
}

func (manager Manager) Box(ctx context.Context, id string) (UnlockFunc, error) {
	if !boxIDPattern.MatchString(id) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidBoxID, id)
	}
	return Acquire(ctx, filepath.Join(manager.root, "boxes", id, "box.lock"))
}

func Acquire(ctx context.Context, path string) (UnlockFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}

	fileLock := flock.New(path)
	locked, err := fileLock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("acquire lock %s: %w", path, err)
	}
	if !locked {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", ErrBusy, path)
	}

	var once sync.Once
	var unlockError error
	return func() error {
		once.Do(func() {
			unlockError = fileLock.Unlock()
		})
		return unlockError
	}, nil
}
