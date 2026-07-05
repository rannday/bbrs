package syncer

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const cacheVersion = 1

// FileStamp captures local file size and modification time for upload caching.
type FileStamp struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"mod_time"`
}

// State tracks uploaded file stamps to skip unchanged uploads across restarts.
type State struct {
	UploadCache map[string]FileStamp `json:"upload_cache"`
}

type cacheFile struct {
	Version int                  `json:"version"`
	Cache   map[string]FileStamp `json:"upload_cache"`
}

// CachePath returns the default persistent cache path under a source directory.
func CachePath(source string) string {
	return filepath.Join(source, ".bbrs", "cache.json")
}

// NewState returns an empty in-memory upload cache.
func NewState() *State {
	return &State{
		UploadCache: make(map[string]FileStamp),
	}
}

// LoadState reads upload cache from path. Missing file yields empty state.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(), nil
		}
		return nil, fmt.Errorf("read cache %q: %w", path, err)
	}

	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode cache %q: %w", path, err)
	}

	state := NewState()
	switch file.Version {
	case 0, 1:
		for remote, stamp := range file.Cache {
			state.UploadCache[remote] = stamp
		}
	default:
		return nil, fmt.Errorf("unsupported cache version %d in %q", file.Version, path)
	}
	return state, nil
}

// Save writes upload cache to path, creating parent directories as needed.
func (state *State) Save(path string) error {
	if state == nil {
		return nil
	}
	if state.UploadCache == nil {
		state.UploadCache = make(map[string]FileStamp)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %q: %w", dir, err)
	}

	payload, err := json.MarshalIndent(cacheFile{
		Version: cacheVersion,
		Cache:   state.UploadCache,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write cache %q: %w", path, err)
	}
	return nil
}

// RememberUpload stores a successful upload stamp for remote path.
func (state *State) RememberUpload(remote string, stamp FileStamp) {
	if state == nil {
		return
	}
	if state.UploadCache == nil {
		state.UploadCache = make(map[string]FileStamp)
	}
	state.UploadCache[remote] = stamp
}

// ForgetRemote removes remote path from the upload cache.
func (state *State) ForgetRemote(remote string) {
	if state == nil || state.UploadCache == nil {
		return
	}
	delete(state.UploadCache, remote)
}

// ShouldUpload reports whether local stamp differs from cached upload.
func (state *State) ShouldUpload(remote string, stamp FileStamp) bool {
	if state == nil || state.UploadCache == nil {
		return true
	}
	previous, ok := state.UploadCache[remote]
	return !ok || previous != stamp
}

// FileStampFromInfo builds a stamp from local file metadata.
func FileStampFromInfo(info fs.FileInfo) FileStamp {
	return FileStamp{
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
	}
}
