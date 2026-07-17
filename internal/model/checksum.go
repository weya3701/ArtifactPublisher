package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
)

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// BundleSHA256 provides a stable identity independent of file enumeration order.
func BundleSHA256(files []PackageFile) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("package contains no files")
	}
	copyFiles := append([]PackageFile(nil), files...)
	sort.Slice(copyFiles, func(i, j int) bool { return copyFiles[i].Name < copyFiles[j].Name })
	hash := sha256.New()
	for _, file := range copyFiles {
		if file.Name == "" || len(file.SHA256) != sha256.Size*2 {
			return "", fmt.Errorf("invalid checksum metadata for %q", file.Name)
		}
		hash.Write([]byte(file.Name))
		hash.Write([]byte{0})
		hash.Write([]byte(file.SHA256))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
