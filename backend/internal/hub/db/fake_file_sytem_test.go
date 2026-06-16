package db

import "os"

type fakeFS struct {
	files       map[string][]byte
	MkdirAllErr error
	RemoveErr   error
	StatErr     error
	OpenErr     error
	OpenFileErr error
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: make(map[string][]byte)}
}

func (f *fakeFS) MkdirAll(_ string, _ os.FileMode) error { return f.MkdirAllErr }
func (f *fakeFS) Remove(path string) error {
	delete(f.files, path)
	return f.RemoveErr
}
func (f *fakeFS) Stat(path string) (os.FileInfo, error) {
	if f.StatErr != nil {
		return nil, f.StatErr
	}
	if _, ok := f.files[path]; !ok {
		return nil, os.ErrNotExist
	}
	return nil, nil // return a real FileInfo if you need it
}
func (f *fakeFS) Open(path string) (*os.File, error) {
	// For a real in-memory file, use os.CreateTemp or bytes.Buffer tricks
	// Simple version: just gate on the error flag
	if f.OpenErr != nil {
		return nil, f.OpenErr
	}
	// write to a real temp file backed by the fake's map content
	tmp, _ := os.CreateTemp("", "fakeFS")
	tmp.Write(f.files[path])
	tmp.Seek(0, 0)
	return tmp, nil
}
func (f *fakeFS) OpenFile(path string, _ int, _ os.FileMode) (*os.File, error) {
	if f.OpenFileErr != nil {
		return nil, f.OpenFileErr
	}
	tmp, _ := os.CreateTemp("", "fakeFS")
	f.files[path] = nil // track that it was created
	return tmp, nil
}
