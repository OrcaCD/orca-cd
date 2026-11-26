package models

import (
	"encoding/json"
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
)

// DeploymentFiles is stored as encrypted JSON. Byte values survive JSON
// encoding even for repository files that aren't UTF-8 text.
type DeploymentFiles = map[string][]byte

func EncodeDeploymentFiles(files DeploymentFiles) (crypto.EncryptedString, error) {
	if len(files) == 0 {
		return "", nil
	}
	data, err := json.Marshal(files)
	if err != nil {
		return "", fmt.Errorf("encode deployment files: %w", err)
	}
	return crypto.EncryptedString(data), nil
}

func DecodeDeploymentFiles(value crypto.EncryptedString) (DeploymentFiles, error) {
	if value == "" {
		return nil, nil
	}
	var files DeploymentFiles
	if err := json.Unmarshal([]byte(value), &files); err != nil {
		return nil, fmt.Errorf("decode deployment files: %w", err)
	}
	return files, nil
}
