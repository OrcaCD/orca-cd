package deployfiles

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode"
)

const (
	Capability   = "additional-deployment-files"
	MaxFiles     = 64
	MaxBytes     = 8 << 20
	MaxPathBytes = 512
	ComposeName  = "compose.yaml"
	OverrideName = "compose.override.yaml"
)

// ValidPath accepts canonical slash-separated paths on both the Hub and Agent.
func ValidPath(name string) bool {
	return len(name) <= MaxPathBytes && name != "." && fs.ValidPath(name) &&
		!strings.ContainsAny(name, `\:`) && strings.IndexFunc(name, unicode.IsControl) < 0
}

func ValidatePaths(paths []string) error {
	if len(paths) > MaxFiles {
		return fmt.Errorf("select at most %d additional files", MaxFiles)
	}
	seen := make(map[string]bool, len(paths))
	for _, name := range paths {
		if err := ValidatePath(name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("duplicate deployment file path %q", name)
		}
		seen[name] = true
	}
	for _, name := range paths {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return fmt.Errorf("file %q is a directory of file %q", parent, name)
			}
		}
	}
	return nil
}

func ValidatePath(name string) error {
	if !ValidPath(name) {
		return fmt.Errorf("invalid deployment file path %q", name)
	}
	first, _, _ := strings.Cut(name, "/")
	switch strings.ToLower(first) {
	case ComposeName, OverrideName, ".env":
		return fmt.Errorf("deployment file path %q is reserved", name)
	}
	return nil
}
