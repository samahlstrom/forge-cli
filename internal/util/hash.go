package util

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// HashManifest tracks file hashes for upgrade diffing.
type HashManifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

// HashContent returns a sha256 hash prefixed with "sha256:".
func HashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("sha256:%x", h)
}

// HashFile reads a file and returns its hash.
func HashFile(path string) (string, error) {
	content, err := ReadText(path)
	if err != nil {
		return "", err
	}
	return HashContent(content), nil
}

const hashesFile = ".forge/.hashes.json"

// ReadHashes reads the hash manifest from the project root.
func ReadHashes(projectRoot string) (HashManifest, error) {
	path := filepath.Join(projectRoot, hashesFile)
	if !Exists(path) {
		return HashManifest{Version: "0.0.0", Files: map[string]string{}}, nil
	}
	data, err := ReadText(path)
	if err != nil {
		return HashManifest{Version: "0.0.0", Files: map[string]string{}}, err
	}
	var m HashManifest
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return HashManifest{Version: "0.0.0", Files: map[string]string{}}, err
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, nil
}

// WriteHashes writes the hash manifest to the project root.
func WriteHashes(projectRoot string, m HashManifest) error {
	return WriteJSON(filepath.Join(projectRoot, hashesFile), m)
}
