package discovery

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type npmIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func NPMPackages(root string) ([]string, error) {
	root = filepath.Clean(root)
	var tarballs []string
	var installedDirectories []string
	packageDirectories := make(map[string]bool)
	tarballDirectories := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".tgz") {
			tarballs = append(tarballs, path)
			tarballDirectories[filepath.Dir(path)] = true
		}
		if !entry.IsDir() && entry.Name() == "package.json" {
			directory := filepath.Dir(path)
			if isInstalledNPMPackage(directory) {
				installedDirectories = append(installedDirectories, directory)
			} else {
				packageDirectories[directory] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover npm packages under %q: %w", root, err)
	}

	var directories []string
	if len(installedDirectories) > 0 {
		directories = uniqueInstalledNPMPackages(installedDirectories)
	} else {
		directories = outermostPackageDirectories(packageDirectories)
	}
	paths := append([]string(nil), tarballs...)
	for _, directory := range directories {
		if !tarballDirectories[directory] {
			paths = append(paths, directory)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no npm package tarballs or package.json files found under %q", root)
	}
	return paths, nil
}

// isInstalledNPMPackage recognizes both node_modules/name and
// node_modules/@scope/name package roots. Other package.json files within a
// dependency (for example test fixtures) are not publishable package roots.
func isInstalledNPMPackage(directory string) bool {
	parent := filepath.Dir(directory)
	if filepath.Base(parent) == "node_modules" {
		return true
	}
	return strings.HasPrefix(filepath.Base(parent), "@") && filepath.Base(filepath.Dir(parent)) == "node_modules"
}

func uniqueInstalledNPMPackages(directories []string) []string {
	sort.Slice(directories, func(i, j int) bool {
		if len(directories[i]) == len(directories[j]) {
			return directories[i] < directories[j]
		}
		return len(directories[i]) < len(directories[j])
	})
	seen := make(map[string]bool)
	unique := make([]string, 0, len(directories))
	for _, directory := range directories {
		key := directory
		contents, err := os.ReadFile(filepath.Join(directory, "package.json"))
		if err == nil {
			var identity npmIdentity
			if json.Unmarshal(contents, &identity) == nil && identity.Name != "" && identity.Version != "" {
				key = identity.Name + "\x00" + identity.Version
			}
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, directory)
		}
	}
	return unique
}

func outermostPackageDirectories(packageDirectories map[string]bool) []string {
	directories := make([]string, 0, len(packageDirectories))
	for directory := range packageDirectories {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(i, j int) bool {
		if len(directories[i]) == len(directories[j]) {
			return directories[i] < directories[j]
		}
		return len(directories[i]) < len(directories[j])
	})
	var selected []string
	for _, directory := range directories {
		nested := false
		for _, outer := range selected {
			relative, err := filepath.Rel(outer, directory)
			if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			selected = append(selected, directory)
		}
	}
	return selected
}
