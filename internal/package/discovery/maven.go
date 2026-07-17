package discovery

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// MavenPackages returns directories containing one POM, or one main JAR when
// no POM exists. A directory is one independently publishable package version.
func MavenPackages(root string) ([]string, error) {
	type directoryFiles struct {
		poms     int
		mainJars int
	}
	directories := make(map[string]*directoryFiles)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".pom" && extension != ".jar" {
			return nil
		}
		directory := filepath.Dir(path)
		if directories[directory] == nil {
			directories[directory] = &directoryFiles{}
		}
		if extension == ".pom" {
			directories[directory].poms++
		} else {
			name := strings.ToLower(entry.Name())
			if !strings.HasSuffix(name, "-sources.jar") && !strings.HasSuffix(name, "-javadoc.jar") && !strings.HasSuffix(name, "-tests.jar") {
				directories[directory].mainJars++
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover Maven packages under %q: %w", root, err)
	}
	paths := make([]string, 0, len(directories))
	for directory, files := range directories {
		if files.poms > 1 {
			return nil, fmt.Errorf("Maven package directory %q contains %d POM files", directory, files.poms)
		}
		if files.poms == 0 && files.mainJars != 1 {
			return nil, fmt.Errorf("JAR-only Maven directory %q contains %d main JAR files; expected one", directory, files.mainJars)
		}
		paths = append(paths, directory)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no Maven packages found under %q", root)
	}
	return paths, nil
}
