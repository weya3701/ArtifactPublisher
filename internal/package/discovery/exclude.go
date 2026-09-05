package discovery

import (
	"fmt"
	"path/filepath"
	"strings"
)

func prepareExcludedDirectories(directories []string) (map[string]struct{}, error) {
	excluded := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		if strings.TrimSpace(directory) == "" {
			return nil, fmt.Errorf("excluded directory cannot be empty")
		}
		clean := filepath.Clean(filepath.FromSlash(directory))
		if clean == "." {
			return nil, fmt.Errorf("excluded directory %q cannot be the package root", directory)
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("excluded directory %q must be relative to the package root", directory)
		}
		excluded[clean] = struct{}{}
	}
	return excluded, nil
}

func isExcludedDirectory(root, path string, excluded map[string]struct{}) bool {
	if len(excluded) == 0 || path == root {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	_, ok := excluded[filepath.Clean(relative)]
	return ok
}
