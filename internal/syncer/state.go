package syncer

import "io/fs"

type FileStamp struct {
	Size    int64
	ModTime int64
}

type State struct {
	UploadCache map[string]FileStamp
}

func NewState() *State {
	return &State{
		UploadCache: make(map[string]FileStamp),
	}
}

func (state *State) RememberUpload(remote string, stamp FileStamp) {
	if state == nil {
		return
	}
	if state.UploadCache == nil {
		state.UploadCache = make(map[string]FileStamp)
	}
	state.UploadCache[remote] = stamp
}

func (state *State) ForgetRemote(remote string) {
	if state == nil || state.UploadCache == nil {
		return
	}
	delete(state.UploadCache, remote)
}

func (state *State) ShouldUpload(remote string, stamp FileStamp) bool {
	if state == nil || state.UploadCache == nil {
		return true
	}
	previous, ok := state.UploadCache[remote]
	return !ok || previous != stamp
}

func FileStampFromInfo(info fs.FileInfo) FileStamp {
	return FileStamp{
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
	}
}