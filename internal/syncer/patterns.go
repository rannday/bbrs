package syncer

import (
	"fmt"
	"path"
	"strings"
)

var DefaultIncludePatterns = []string{"*.js", "*.ts"}

type Patterns struct {
	include []string
}

func NewPatterns(extra []string) (Patterns, error) {
	include := append([]string{}, DefaultIncludePatterns...)
	for _, raw := range extra {
		for _, part := range strings.Split(raw, ",") {
			pattern := strings.TrimSpace(part)
			if pattern == "" {
				continue
			}
			if _, err := path.Match(pattern, "x"); err != nil {
				return Patterns{}, fmt.Errorf("invalid pattern %q: %w", pattern, err)
			}
			include = append(include, pattern)
		}
	}
	return Patterns{include: include}, nil
}

func (patterns Patterns) IncludePatterns() []string {
	return append([]string{}, patterns.include...)
}

func (patterns Patterns) Match(name string) bool {
	normalized := NormalizeSlashes(name)
	if normalized == "" || isDeclarationTypeScript(normalized) {
		return false
	}
	base := path.Base(normalized)
	for _, pattern := range patterns.include {
		if matchGlob(pattern, normalized) || matchGlob(pattern, base) {
			return true
		}
	}
	return false
}

func matchGlob(pattern, value string) bool {
	matched, err := path.Match(pattern, value)
	return err == nil && matched
}

func isDeclarationTypeScript(name string) bool {
	return matchGlob("*.d.ts", path.Base(NormalizeSlashes(name)))
}
