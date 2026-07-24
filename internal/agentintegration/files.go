package agentintegration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	managedStartMarker = "<!-- wktbox-agent:start -->"
	managedEndMarker   = "<!-- wktbox-agent:end -->"
)

func digestTree(filesystem fs.FS, root string) (string, error) {
	paths := make([]string, 0)
	if err := fs.WalkDir(filesystem, root, func(
		name string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			paths = append(paths, name)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(paths)

	digest := sha256.New()
	for _, name := range paths {
		data, err := fs.ReadFile(filesystem, name)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(filepath.FromSlash(root), filepath.FromSlash(name))
		if err != nil {
			return "", err
		}
		normalized := path.Clean(filepath.ToSlash(relative))
		if normalized == "." || strings.HasPrefix(normalized, "../") {
			return "", fmt.Errorf("invalid skill asset path %q", name)
		}
		_, _ = digest.Write([]byte(normalized))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(data)
		_, _ = digest.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func renderManagedInstructions(existing string) (string, error) {
	start := strings.Index(existing, managedStartMarker)
	end := strings.Index(existing, managedEndMarker)
	if (start < 0) != (end < 0) {
		return "", fmt.Errorf("agent instructions contain an incomplete Wktbox block")
	}
	if start >= 0 {
		if end < start {
			return "", fmt.Errorf("agent instruction markers are out of order")
		}
		end += len(managedEndMarker)
		return existing[:start] + ManagedInstructionsBlock + existing[end:], nil
	}
	if existing == "" {
		return ManagedInstructionsBlock + "\n", nil
	}
	separator := "\n\n"
	if strings.HasSuffix(existing, "\n\n") {
		separator = ""
	} else if strings.HasSuffix(existing, "\n") {
		separator = "\n"
	}
	return existing + separator + ManagedInstructionsBlock + "\n", nil
}

func removeManagedInstructions(existing string) (string, error) {
	start := strings.Index(existing, managedStartMarker)
	end := strings.Index(existing, managedEndMarker)
	if start < 0 && end < 0 {
		return existing, nil
	}
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("agent instructions contain an incomplete Wktbox block")
	}
	end += len(managedEndMarker)
	before := existing[:start]
	after := existing[end:]
	if strings.HasSuffix(before, "\n\n") && strings.HasPrefix(after, "\n") {
		before = strings.TrimSuffix(before, "\n")
	} else if before == "" && strings.HasPrefix(after, "\n") {
		after = strings.TrimPrefix(after, "\n")
	}
	return before + after, nil
}

func syncWriteFile(path string, data []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func prepareAtomicFile(
	path string,
	data []byte,
	newMode fs.FileMode,
) (string, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	mode := newMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return "", err
	}
	file, err := os.CreateTemp(parent, ".wktbox-instructions-*")
	if err != nil {
		return "", err
	}
	temporary := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(temporary)
	}
	if err := file.Chmod(mode); err != nil {
		cleanup()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		cleanup()
		return "", err
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return temporary, nil
}

func reserveSiblingPath(parent string, pattern string) (string, error) {
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}

func copySkillTree(
	source fs.FS,
	sourceRoot string,
	destination string,
	writeFile func(string, []byte, fs.FileMode) error,
) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		return err
	}
	err := fs.WalkDir(source, sourceRoot, func(
		name string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		relative := strings.TrimPrefix(name, sourceRoot)
		relative = strings.TrimPrefix(relative, "/")
		if relative == "" {
			return nil
		}
		if relative == name || !fs.ValidPath(relative) {
			return fmt.Errorf("invalid skill asset path %q", name)
		}
		target := filepath.Join(
			destination,
			filepath.FromSlash(relative),
		)
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			return os.Chmod(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported skill asset %s", name)
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		if err := writeFile(target, data, 0o644); err != nil {
			return err
		}
		return os.Chmod(target, 0o644)
	})
	if err != nil {
		return err
	}
	return filepath.WalkDir(destination, func(
		name string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return syncDirectory(name)
		}
		return nil
	})
}

func readFileOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func copyFile(source string, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(
		destination,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		mode,
	)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
