package deployfiles

import (
	"slices"
	"strings"
	"testing"
)

func TestValidatePaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
		want  string
	}{
		{"nested", []string{"config/site.conf", "config/private/.env"}, ""},
		{"parent traversal", []string{"../site.conf"}, "invalid"},
		{"absolute", []string{"/config"}, "invalid"},
		{"backslash", []string{`config\site.conf`}, "invalid"},
		{"long", []string{strings.Repeat("x", MaxPathBytes+1)}, "invalid"},
		{"managed", []string{"Compose.Override.Yaml/site"}, "reserved"},
		{"duplicate", []string{"config/a", "config/a"}, "duplicate"},
		{"parent collision", []string{"config/a/b", "config"}, "directory"},
		{"too many", slices.Repeat([]string{"site"}, MaxFiles+1), "at most"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePaths(tc.paths)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
