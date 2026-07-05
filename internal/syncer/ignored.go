package syncer

import (
	"fmt"
	"path"
	"strings"
)

// DefaultIgnorePatterns are path patterns skipped during source walks.
var DefaultIgnorePatterns = []string{
	".bbrs",
	".git",
	"target",
	"node_modules",
	"dist",
	"build",
	".zed",
	".vscode",
	".idea",
	"coverage",
	"tmp",
	"temp",
}

// IgnoredPatterns holds filename and directory patterns to skip during source walks.
type IgnoredPatterns struct {
	patterns []string
}

// NewIgnoredPatterns returns ignored patterns from defaults plus any extras.
func NewIgnoredPatterns(extra []string) (IgnoredPatterns, error) {
	patterns := append([]string{}, DefaultIgnorePatterns...)
	for _, raw := range extra {
		for _, part := range strings.Split(raw, ",") {
			pattern := strings.TrimSpace(part)
			if pattern == "" {
				continue
			}
			if _, err := path.Match(pattern, "x"); err != nil {
				return IgnoredPatterns{}, fmt.Errorf("invalid ignore pattern %q: %w", pattern, err)
			}
			patterns = append(patterns, pattern)
		}
	}
	return IgnoredPatterns{patterns: patterns}, nil
}

// IsIgnored reports whether a source-relative path or base name should be skipped.
func (ignored IgnoredPatterns) IsIgnored(name string) bool {
	normalized := NormalizeSlashes(name)
	if normalized == "" {
		return false
	}
	base := path.Base(normalized)
	for _, pattern := range ignored.patterns {
		if matchIgnorePattern(pattern, normalized) || matchIgnorePattern(pattern, base) {
			return true
		}
	}
	return false
}

// Patterns returns a copy of configured ignore patterns.
func (ignored IgnoredPatterns) Patterns() []string {
	return append([]string{}, ignored.patterns...)
}

func matchIgnorePattern(pattern, value string) bool {
	matched, err := path.Match(strings.ToLower(pattern), strings.ToLower(value))
	return err == nil && matched
}
