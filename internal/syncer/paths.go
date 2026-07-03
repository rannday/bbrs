package syncer

import (
	"fmt"
	"path"
	"strings"
)

func NormalizeSlashes(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func NormalizeRemotePath(value string) (string, error) {
	replaced := NormalizeSlashes(value)
	if strings.HasPrefix(replaced, "/") {
		return "", fmt.Errorf("remote paths must be relative")
	}
	if hasWindowsDrivePrefix(replaced) {
		return "", fmt.Errorf("remote paths must be relative")
	}

	parts := make([]string, 0)
	for _, part := range strings.Split(replaced, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", fmt.Errorf("remote paths must not contain '..'")
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/"), nil
}

func NormalizeRemoteFilePath(value string) (string, error) {
	normalized, err := NormalizeRemotePath(value)
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", fmt.Errorf("remote file path must not be empty")
	}
	return normalized, nil
}

func JoinDestinationFile(destination, relative string) (string, error) {
	dest, err := NormalizeRemotePath(destination)
	if err != nil {
		return "", fmt.Errorf("invalid destination %q: %w", destination, err)
	}
	rel, err := NormalizeRemoteFilePath(relative)
	if err != nil {
		return "", fmt.Errorf("invalid relative file path %q: %w", relative, err)
	}

	joined := rel
	if dest != "" {
		joined = dest + "/" + rel
	}
	if _, ok := RelativeToDestination(dest, joined); !ok {
		return "", fmt.Errorf("remote path %q is outside destination %q", joined, dest)
	}
	return joined, nil
}

func RelativeToDestination(destination, remoteFile string) (string, bool) {
	dest, err := NormalizeRemotePath(destination)
	if err != nil {
		return "", false
	}
	remote, err := NormalizeRemoteFilePath(remoteFile)
	if err != nil {
		return "", false
	}
	if dest == "" {
		return remote, true
	}
	prefix := dest + "/"
	if strings.HasPrefix(remote, prefix) {
		return strings.TrimPrefix(remote, prefix), true
	}
	return "", false
}

func DisplayDestination(destination string) string {
	normalized, err := NormalizeRemotePath(destination)
	if err != nil || normalized == "" {
		return "/"
	}
	return "/" + normalized
}

func hasWindowsDrivePrefix(value string) bool {
	if len(value) < 2 {
		return false
	}
	if value[1] != ':' {
		return false
	}
	letter := value[0]
	if !((letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')) {
		return false
	}
	return len(value) == 2 || value[2] == '/'
}

func cleanRelativeSlashPath(value string) (string, error) {
	normalized := NormalizeSlashes(value)
	if strings.HasPrefix(normalized, "/") || hasWindowsDrivePrefix(normalized) {
		return "", fmt.Errorf("relative paths must be relative")
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." {
		return "", nil
	}
	return NormalizeRemoteFilePath(cleaned)
}
