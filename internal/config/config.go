package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// File holds optional settings loaded from <source>/.bbrs/config.toml or config.json.
// CLI flags override file values when both are set.
type File struct {
	Listen            string   `json:"listen" toml:"listen"`
	Port              *int     `json:"port" toml:"port"`
	Destination       string   `json:"destination" toml:"destination"`
	Host              string   `json:"host" toml:"host"`
	Patterns          []string `json:"patterns" toml:"patterns"`
	LogDir            string   `json:"log_dir" toml:"log_dir"`
	AllowRemoteListen *bool    `json:"allow_remote_listen" toml:"allow_remote_listen"`
	DryRun            *bool    `json:"dry_run" toml:"dry_run"`
	Verbose           *bool    `json:"verbose" toml:"verbose"`
	Once              *bool    `json:"once" toml:"once"`
	Yes               *bool    `json:"yes" toml:"yes"`
	IgnoredDirs       []string `json:"ignored_dirs" toml:"ignored_dirs"`
}

// Load reads config from <source>/.bbrs/config.toml or config.json when present.
func Load(source string) (File, error) {
	dir := filepath.Join(source, ".bbrs")
	for _, name := range []string{"config.toml", "config.json"} {
		path := filepath.Join(dir, name)
		file, err := loadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return File{}, err
		}
		return file, nil
	}
	return File{}, nil
}

func loadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var file File
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		if _, err := toml.Decode(string(data), &file); err != nil {
			return File{}, fmt.Errorf("decode %q: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, &file); err != nil {
			return File{}, fmt.Errorf("decode %q: %w", path, err)
		}
	default:
		return File{}, fmt.Errorf("unsupported config extension %q", path)
	}
	return file, nil
}
