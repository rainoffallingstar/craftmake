package colab

import (
	"fmt"
	"path/filepath"
	"strings"
)

type PathMapper struct {
	HostRoot   string
	RemoteRoot string
}

func (m PathMapper) Map(path string) (string, error) {
	host, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve host path: %w", err)
	}
	root, err := filepath.Abs(m.HostRoot)
	if err != nil {
		return "", fmt.Errorf("resolve host root: %w", err)
	}
	if m.RemoteRoot == "" {
		return "", fmt.Errorf("remote root is required")
	}
	relative, err := filepath.Rel(root, host)
	if err != nil {
		return "", fmt.Errorf("map path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside host root %q", path, m.HostRoot)
	}
	return filepath.Join(m.RemoteRoot, relative), nil
}
