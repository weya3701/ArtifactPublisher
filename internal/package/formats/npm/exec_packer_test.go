package npm

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExecPackerCommandUsesAbsoluteDirectory(t *testing.T) {
	relativeDirectory := filepath.Join("node_modules", "yocto-queue")
	command, packDirectory, err := (ExecPacker{Executable: "npm"}).command(context.Background(), relativeDirectory)
	if err != nil {
		t.Fatalf("command() error = %v", err)
	}
	if !filepath.IsAbs(packDirectory) {
		t.Fatalf("pack directory = %q; want an absolute path", packDirectory)
	}
	if command.Args[2] != packDirectory {
		t.Fatalf("npm package argument = %q; want %q", command.Args[2], packDirectory)
	}
	if command.Args[len(command.Args)-1] != packDirectory {
		t.Fatalf("npm pack destination = %q; want %q", command.Args[len(command.Args)-1], packDirectory)
	}
}
