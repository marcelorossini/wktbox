package redact

import (
	"sort"
	"strings"
)

type Redactor struct {
	values []string
}

func New(values ...string) Redactor {
	unique := make(map[string]struct{}, len(values))
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := unique[value]; exists {
			continue
		}
		unique[value] = struct{}{}
		filtered = append(filtered, value)
	}
	sort.Slice(filtered, func(left int, right int) bool {
		return len(filtered[left]) > len(filtered[right])
	})
	return Redactor{values: filtered}
}

func (redactor Redactor) String(input string) string {
	for _, value := range redactor.values {
		input = strings.ReplaceAll(input, value, "***")
	}
	return input
}
