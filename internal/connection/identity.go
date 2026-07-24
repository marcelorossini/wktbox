package connection

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
)

var boxIDPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

func ID(memberIDs []string) (string, error) {
	if len(memberIDs) < 2 {
		return "", errors.New("a connection requires at least two boxes")
	}
	members := append([]string(nil), memberIDs...)
	sort.Strings(members)
	for index, member := range members {
		if !boxIDPattern.MatchString(member) {
			return "", errors.New("connection member has an invalid box ID")
		}
		if index > 0 && member == members[index-1] {
			return "", errors.New("connection members must be distinct")
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(members, "\n")))
	return hex.EncodeToString(sum[:])[:12], nil
}

func Alias(boxID string) string {
	return boxID + ".wktbox"
}
