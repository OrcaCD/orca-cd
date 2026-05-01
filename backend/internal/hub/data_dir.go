package hub

import (
	"fmt"
	"os"
)

func initDataDir() error {
	if err := os.MkdirAll("data", 0750); err != nil {
		return err
	}
	return checkDataDirWritable()
}

func checkDataDirWritable() error {
	return checkWritable("data")
}

func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err == nil {
		err = f.Close()
		if err != nil {
			return err
		}
		err = os.Remove(f.Name())
		if err != nil {
			return err
		}
		return nil
	}
	if !os.IsPermission(err) {
		return err
	}
	uid := os.Getuid()
	return fmt.Errorf(
		"directory is not writable, run: sudo chown -R %d:%d ./data",
		uid, os.Getgid(),
	)
}
