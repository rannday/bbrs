package syncer

// DefaultIgnoredDirNames are directory names skipped during source walks.
// Matched case-insensitively against the base directory name.
var DefaultIgnoredDirNames = []string{
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

// IgnoredDirs holds directory names to skip during source walks.
type IgnoredDirs struct {
	names []string
}

// NewIgnoredDirs returns ignored dirs from defaults plus any extras.
func NewIgnoredDirs(extra []string) IgnoredDirs {
	names := append([]string{}, DefaultIgnoredDirNames...)
	for _, name := range extra {
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return IgnoredDirs{names: names}
}

// IsIgnored reports whether a directory base name should be skipped.
func (ignored IgnoredDirs) IsIgnored(name string) bool {
	for _, candidate := range ignored.names {
		if equalFoldASCII(name, candidate) {
			return true
		}
	}
	return false
}

// Names returns a copy of configured ignored directory names.
func (ignored IgnoredDirs) Names() []string {
	return append([]string{}, ignored.names...)
}

// IsIgnoredDir reports whether name matches the default ignored set.
func IsIgnoredDir(name string) bool {
	return NewIgnoredDirs(nil).IsIgnored(name)
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		l := left[i]
		r := right[i]
		if l >= 'A' && l <= 'Z' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}
