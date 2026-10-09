package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func TestLocalReadAcceptsAbsolutePathInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "Example.java")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("class Example {}"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOS_WORKSPACE_ROOT", root)

	out, err := executeLocalFileTool(context.Background(), config.Tool{Name: "workspace.read_file"}, map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if out["path"] != filepath.Join("src", "Example.java") {
		t.Fatalf("unexpected relative path: %v", out["path"])
	}
}

func TestLocalReadRejectsAbsolutePathOutsideWorkspace(t *testing.T) {
	t.Setenv("AOS_WORKSPACE_ROOT", t.TempDir())
	_, err := executeLocalFileTool(context.Background(), config.Tool{Name: "workspace.read_file"}, map[string]string{"path": filepath.Join(t.TempDir(), "outside.txt")})
	if err == nil {
		t.Fatal("expected outside-workspace path to be rejected")
	}
}

func TestInspectProjectDetectsMavenMetadata(t *testing.T) {
	root := t.TempDir()
	pom := `<?xml version="1.0"?><project><parent><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-parent</artifactId><version>3.2.12</version></parent><artifactId>demo</artifactId><properties><java.version>17</java.version></properties><dependencies><dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>`
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte(pom), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := inspectProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if out["project_type"] != "maven" || out["java_version"] != "17" || out["test_runner"] != "mvn test -q" {
		t.Fatalf("unexpected project metadata: %#v", out)
	}
	parent := out["parent"].(map[string]string)
	if parent["version"] != "3.2.12" {
		t.Fatalf("unexpected parent: %#v", parent)
	}
}

func TestReplaceRejectsNonIdempotentEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Example.java")
	if err := os.WriteFile(path, []byte("void test() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOS_WORKSPACE_ROOT", root)
	_, err := executeLocalFileTool(context.Background(), config.Tool{Name: "workspace.replace_in_file"}, map[string]string{
		"path": "Example.java",
		"old":  "void test()",
		"new":  "public void test()",
	})
	if err == nil {
		t.Fatal("expected non-idempotent replacement to be rejected")
	}
}

func TestFindProjectRootFromNestedMavenTestPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project/>"), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src", "test", "java", "com", "example", "demo", "task")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	projectDir, projectType := findProjectRoot(nested, root)
	if projectDir != root || projectType != "maven" {
		t.Fatalf("unexpected project root: dir=%q type=%q", projectDir, projectType)
	}
}
