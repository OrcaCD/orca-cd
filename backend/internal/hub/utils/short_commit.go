package utils

// ShortCommit abbreviates a commit hash like Git does by default.
func ShortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
