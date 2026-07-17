package discovery

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	pypihandler "packagespublisher/internal/package/formats/pypi"
)

func PyPIPackages(root string) ([]string, error) {
	root = filepath.Clean(root)
	selected := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !pypihandler.IsDistribution(entry.Name()) {
			return nil
		}
		identity, err := pypihandler.InspectIdentity(path)
		key := path
		if err == nil {
			key = identity.Name + "\x00" + identity.Version
		}
		current, exists := selected[key]
		if !exists || shorterPath(path, current) {
			selected[key] = path
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover PyPI packages under %q: %w", root, err)
	}
	paths := make([]string, 0, len(selected))
	for _, path := range selected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no PyPI wheel or source distributions found under %q", root)
	}
	return paths, nil
}

func shorterPath(candidate, current string) bool {
	if len(candidate) == len(current) {
		return candidate < current
	}
	return len(candidate) < len(current)
}
