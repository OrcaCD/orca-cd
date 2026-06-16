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
	return f.RemoveErr
}
func (f *fakeFS) Stat(path string) (os.FileInfo, error) {
	return nil, f.StatErr
}
func (f *fakeFS) Open(path string) (*os.File, error) {
	return nil, f.OpenErr
}
func (f *fakeFS) OpenFile(path string, _ int, _ os.FileMode) (*os.File, error) {

	return nil, f.OpenFileErr
}
