package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	Open(path string) (*os.File, error)
	OpenFile(path string, flag int, perm os.FileMode) (*os.File, error)
	Copy(dst io.Writer, src io.Reader) (written int64, err error)
}

type realFS struct{}

func (realFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (realFS) Remove(path string) error                     { return os.Remove(path) }
func (realFS) Open(path string) (*os.File, error) {
	path = filepath.Clean(path)
	return os.Open(path)
}
func (realFS) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	path = filepath.Clean(path)
	return os.OpenFile(path, flag, perm)
}
func (realFS) Copy(dst io.Writer, src io.Reader) (written int64, err error) {
	return io.Copy(dst, src)
}

var fs FileSystem = realFS{}

func Restore(backupPath string) error {
	if DB == nil {
		return fmt.Errorf("database not connected")
	}

	if err := fs.MkdirAll("data", 0750); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	backupCurrentPath := sqliteFilePath + "-restore-" + fmt.Sprintf("%d", time.Now().Unix()) + ".bak"
	if err := Export(backupCurrentPath); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if err := Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	_ = fs.Remove(sqliteFilePath + "-shm")
	_ = fs.Remove(sqliteFilePath + "-wal")

	if err := copyFile(backupPath, sqliteFilePath); err != nil {
		if rerr := copyFile(backupCurrentPath, sqliteFilePath); rerr != nil {
			return fmt.Errorf("restore failed: %w; rollback also failed: %v", err, rerr)
		}
		return fmt.Errorf("failed to restore database (rolled back): %w", err)
	}

	return nil
}

func copyFile(src, dst string) (err error) {
	source, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()

	destination, err := fs.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := destination.Close(); err == nil {
			err = cerr
		}
	}()

	if _, err := fs.Copy(destination, source); err != nil {
		return err
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	return nil
}
