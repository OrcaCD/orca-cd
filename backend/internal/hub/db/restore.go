package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func Restore(backupPath string) error {
	if DB == nil {
		return fmt.Errorf("database not connected")
	}

	if err := os.MkdirAll("data", 0750); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	backupCurrentPath := sqliteFilePath + "-restore-" + fmt.Sprintf("%d", time.Now().Unix()) + ".bak"
	if err := Export(backupCurrentPath); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if err := Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	_ = os.Remove(sqliteFilePath + "-shm")
	_ = os.Remove(sqliteFilePath + "-wal")

	if err := copyFile(backupPath, sqliteFilePath); err != nil {
        if rerr := copyFile(backupCurrentPath, sqliteFilePath); rerr != nil {
            return fmt.Errorf("restore failed: %w; rollback also failed: %v", err, rerr)
        }
        return fmt.Errorf("failed to restore database (rolled back): %w", err)
    }

	return nil
}

func copyFile(src, dst string) (err error) {
	src = filepath.Clean(src)
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()

	dst = filepath.Clean(dst)
	destination, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := destination.Close(); err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	return nil
}
