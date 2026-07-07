package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	confx "github.com/rannday/go-conf"
)

// File holds optional settings loaded from <source>/.bbrs/config.toml.
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
	var file File
	if err := confx.LoadInto(path, &file); err != nil {
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			return File{}, nil
		}
		return File{}, fmt.Errorf("load %q: %w", path, err)
	}
	return file, nil
}
