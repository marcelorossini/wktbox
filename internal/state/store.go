package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrCorrupt      = errors.New("state file is corrupt")
	ErrInvalidBoxID = errors.New("invalid box ID")
)

var boxIDPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

type Store struct {
	root string
}

func NewStore(root string) Store {
	return Store{root: filepath.Clean(root)}
}

func (store Store) Root() string {
	return store.root
}

func (store Store) BoxDir(id string) (string, error) {
	if !boxIDPattern.MatchString(id) {
		return "", fmt.Errorf("%w: %q", ErrInvalidBoxID, id)
	}
	return filepath.Join(store.root, "boxes", id), nil
}

func (store Store) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if err := store.removeStaleTemporaryFiles(); err != nil {
		return State{}, err
	}

	data, err := os.ReadFile(filepath.Join(store.root, "state.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Empty(), nil
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	var current State
	if err := json.Unmarshal(data, &current); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if current.Version != 1 {
		return State{}, fmt.Errorf("%w: unsupported version %d", ErrCorrupt, current.Version)
	}
	if current.Boxes == nil {
		current.Boxes = make(map[string]BoxRecord)
	}
	return current, nil
}

func (store Store) Save(ctx context.Context, current State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if current.Version == 0 {
		current.Version = 1
	}
	if current.Version != 1 {
		return fmt.Errorf("save state: unsupported version %d", current.Version)
	}
	if current.Boxes == nil {
		current.Boxes = make(map[string]BoxRecord)
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(store.root, 0o700); err != nil {
		return fmt.Errorf("protect state directory: %w", err)
	}

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(store.root, ".state.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	destination := filepath.Join(store.root, "state.json")
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	if err := syncDirectory(store.root); err != nil {
		return fmt.Errorf("sync state directory: %w", err)
	}
	return nil
}

func (store Store) removeStaleTemporaryFiles() error {
	matches, err := filepath.Glob(filepath.Join(store.root, ".state.json.*.tmp"))
	if err != nil {
		return fmt.Errorf("find temporary state files: %w", err)
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove temporary state file: %w", err)
		}
	}
	return nil
}
