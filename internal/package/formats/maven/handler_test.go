package maven_test

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"packagespublisher/internal/package/formats/maven"
)

func TestBuildPackageDescriptor(t *testing.T) {
	directory := t.TempDir()
	write(t, filepath.Join(directory, "demo-1.2.3.pom"), `<project><modelVersion>4.0.0</modelVersion><groupId>com.example</groupId><artifactId>demo</artifactId><version>1.2.3</version><packaging>jar</packaging></project>`)
	write(t, filepath.Join(directory, "demo-1.2.3.jar"), "jar-content")
	write(t, filepath.Join(directory, "demo-1.2.3-sources.jar"), "source-content")

	descriptor, err := (maven.Handler{}).BuildPackageDescriptor(context.Background(), directory)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if descriptor.Namespace != "com.example" || descriptor.Name != "demo" || descriptor.Version != "1.2.3" {
		t.Fatalf("unexpected coordinate: %+v", descriptor)
	}
	if descriptor.Packaging != "jar" || len(descriptor.Files) != 3 || len(descriptor.SHA256) != 64 {
		t.Fatalf("unexpected bundle: %+v", descriptor)
	}
}

func TestBuildPackageDescriptorGeneratesPOMAndSHA256ForJAROnlyInput(t *testing.T) {
	directory := t.TempDir()
	jarPath := filepath.Join(directory, "demo-1.2.3.jar")
	createJAR(t, jarPath, map[string]string{
		"META-INF/maven/com.example/demo/pom.properties": "groupId=com.example\nartifactId=demo\nversion=1.2.3\n",
		"com/example/Demo.class":                         "bytecode",
	})

	descriptor, err := (maven.Handler{}).BuildPackageDescriptor(context.Background(), jarPath)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if descriptor.Namespace != "com.example" || descriptor.Name != "demo" || descriptor.Version != "1.2.3" {
		t.Fatalf("unexpected generated coordinate: %+v", descriptor)
	}
	pomPath := filepath.Join(directory, "demo-1.2.3.pom")
	for _, generated := range []string{pomPath, jarPath + ".sha256", pomPath + ".sha256"} {
		if _, err := os.Stat(generated); err != nil {
			t.Errorf("generated file %q: %v", generated, err)
		}
	}
}

func TestBuildPackageDescriptorUsesConfiguredGAVWhenJARHasNoMavenMetadata(t *testing.T) {
	directory := t.TempDir()
	jarPath := filepath.Join(directory, "legacy-4.5.6.jar")
	createJAR(t, jarPath, map[string]string{"legacy/Library.class": "bytecode"})
	handler := maven.Handler{Fallback: maven.Coordinates{GroupID: "org.legacy", ArtifactID: "legacy", Version: "4.5.6"}}
	descriptor, err := handler.BuildPackageDescriptor(context.Background(), jarPath)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if descriptor.Namespace != "org.legacy" {
		t.Fatalf("fallback GAV not used: %+v", descriptor)
	}
}

func TestBuildPackageDescriptorRejectsMissingMainArtifact(t *testing.T) {
	directory := t.TempDir()
	write(t, filepath.Join(directory, "demo-1.0.0.pom"), `<project><groupId>com.example</groupId><artifactId>demo</artifactId><version>1.0.0</version></project>`)
	if _, err := (maven.Handler{}).BuildPackageDescriptor(context.Background(), directory); err == nil {
		t.Fatal("expected missing JAR error")
	}
}

func TestBuildPackageDescriptorUsesParentGAV(t *testing.T) {
	directory := t.TempDir()
	write(t, filepath.Join(directory, "child-2.0.0.pom"), `<project><parent><groupId>com.example</groupId><version>2.0.0</version></parent><artifactId>child</artifactId></project>`)
	write(t, filepath.Join(directory, "child-2.0.0.jar"), "jar")
	descriptor, err := (maven.Handler{}).BuildPackageDescriptor(context.Background(), directory)
	if err != nil {
		t.Fatalf("BuildPackageDescriptor() error = %v", err)
	}
	if descriptor.Namespace != "com.example" || descriptor.Version != "2.0.0" {
		t.Fatalf("parent GAV not applied: %+v", descriptor)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func createJAR(t *testing.T, path string, files map[string]string) {
	t.Helper()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(output)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
