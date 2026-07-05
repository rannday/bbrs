package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// File holds optional settings loaded from <source>/.bbrs/config.toml.
// CLI flags override file values when both are set.
type File struct {
	Listen      string   `toml:"listen"`
	Port        *int     `toml:"port"`
	Destination string   `toml:"destination"`
	Target      string   `toml:"target"`
	Include     []string `toml:"include"`
	Ignore      []string `toml:"ignore"`
	LogDir      string   `toml:"log_dir"`
	Verbose     *bool    `toml:"verbose"`
}

// Load reads config from <source>/.bbrs/config.toml when present.
func Load(source string) (File, error) {
	path := filepath.Join(source, ".bbrs", "config.toml")
	file, err := loadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, err
	}
	return file, nil
}

func loadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var file File
	if _, err := toml.Decode(string(data), &file); err != nil {
		return File{}, fmt.Errorf("decode %q: %w", path, err)
	}
	return file, nil
}
