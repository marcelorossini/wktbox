package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
)

type Platform string

const (
	Unix    Platform = "unix"
	Windows Platform = "windows"
)

type BoxIdentity struct {
	ID          string
	ProjectName string
	DisplayName string
}

func ForWorktree(commonDir string, worktreePath string, platform Platform) BoxIdentity {
	canonicalCommon := canonical(commonDir, platform)
	canonicalWorktree := canonical(worktreePath, platform)
	digest := sha256.Sum256([]byte(canonicalCommon + "\n" + canonicalWorktree))
	id := hex.EncodeToString(digest[:])[:12]

	displayName := filepath.Base(canonicalWorktree)
	if platform == Windows {
		displayName = path.Base(canonicalWorktree)
	}

	return BoxIdentity{
		ID:          id,
		ProjectName: "wktbox-" + id,
		DisplayName: displayName,
	}
}

func canonical(value string, platform Platform) string {
	if platform == Windows {
		value = strings.ReplaceAll(value, `\`, "/")
		value = path.Clean(value)
		return strings.ToLower(value)
	}

	if absolute, err := filepath.Abs(value); err == nil {
		value = absolute
	}
	value = filepath.Clean(value)
	if evaluated, err := filepath.EvalSymlinks(value); err == nil {
		value = evaluated
	}
	return value
}
