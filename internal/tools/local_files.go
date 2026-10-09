package tools

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func executeLocalFileTool(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	root := os.Getenv("AOS_WORKSPACE_ROOT")
	if root == "" {
		root, _ = os.Getwd()
	}
	root, _ = filepath.Abs(root)
	rel := params["path"]
	if rel == "" {
		if t.Name == "workspace.run_tests" || t.Name == "workspace.inspect_project" {
			rel = "."
		} else {
			rel = "README.md"
		}
	}
	var path string
	if filepath.IsAbs(rel) {
		path = filepath.Clean(rel)
		if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
			return nil, fmt.Errorf("absolute path is outside workspace root")
		}
		rel, _ = filepath.Rel(root, path)
	} else {
		rel = filepath.Clean(rel)
		path = filepath.Join(root, rel)
	}
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("local file path escapes workspace root")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch t.Name {
	case "workspace.inspect_project":
		return inspectProject(path)
	case "workspace.tree":
		var entries []string
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if p == root {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if strings.HasPrefix(rel, ".git"+string(filepath.Separator)) || rel == ".git" {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			entries = append(entries, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": root, "entries": entries}, nil
	case "workspace.read_file":
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": rel, "content": string(b)}, nil
	case "workspace.search":
		query := params["query"]
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		var matches []string
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				name := info.Name()
				if name == ".git" || name == "node_modules" || name == "dist" || name == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if len(matches) >= 100 || info.Size() > 1<<20 {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for i, line := range strings.Split(string(b), "\n") {
				if strings.Contains(line, query) {
					r, _ := filepath.Rel(root, p)
					matches = append(matches, fmt.Sprintf("%s:%d:%s", r, i+1, strings.TrimSpace(line)))
					if len(matches) >= 100 {
						break
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"query": query, "matches": matches}, nil
	case "workspace.append_file":
		text := params["text"]
		if text == "" {
			return nil, fmt.Errorf("text is required")
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err = f.WriteString(text); err != nil {
			return nil, err
		}
		return map[string]any{"path": rel, "appended": text, "status": "updated"}, nil
	case "workspace.write_file":
		content := cleanGeneratedSource(params["content"])
		if content == "" {
			return nil, fmt.Errorf("content is required")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return nil, err
		}
		return map[string]any{"path": rel, "bytes": len(content), "status": "written"}, nil
	case "workspace.replace_in_file":
		oldText, newText := params["old"], params["new"]
		if oldText == "" {
			return nil, fmt.Errorf("old is required")
		}
		if strings.Contains(newText, oldText) {
			return nil, fmt.Errorf("unsafe non-idempotent replacement: new text contains old text; use a larger unique context or rewrite the complete file")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		content := string(b)
		count := strings.Count(content, oldText)
		if count == 0 {
			return nil, fmt.Errorf("old text not found in %s", rel)
		}
		if count > 1 {
			return nil, fmt.Errorf("old text is not unique in %s (%d matches)", rel, count)
		}
		updated := strings.Replace(content, oldText, newText, 1)
		if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
			return nil, err
		}
		return map[string]any{"path": rel, "status": "updated", "replacements": 1}, nil
	case "workspace.git_diff":
		if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
			return map[string]any{"path": rel, "diff": "", "status": "unavailable", "reason": "workspace is not a git repository"}, nil
		}
		tracked := exec.CommandContext(ctx, "git", "ls-files", "--error-unmatch", "--", rel)
		tracked.Dir = root
		if err := tracked.Run(); err != nil {
			cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--", "/dev/null", path)
			cmd.Dir = root
			out, diffErr := cmd.CombinedOutput()
			// git diff --no-index returns 1 when differences are present.
			if diffErr != nil && cmd.ProcessState.ExitCode() != 1 {
				return nil, fmt.Errorf("git diff untracked: %w: %s", diffErr, strings.TrimSpace(string(out)))
			}
			return map[string]any{"path": rel, "diff": string(out), "status": "untracked"}, nil
		}
		cmd := exec.CommandContext(ctx, "git", "diff", "--", rel)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return map[string]any{"path": rel, "diff": string(out)}, nil
	case "workspace.run_tests":
		info, err := os.Stat(path)
		if err != nil {
			return map[string]any{"status": "failed", "error": fmt.Sprintf("test target %s: %v", rel, err)}, nil
		}
		language := strings.ToLower(params["language"])
		var cmd *exec.Cmd
		runner := ""
		searchFrom := path
		if !info.IsDir() {
			searchFrom = filepath.Dir(path)
		}
		projectDir, projectType := findProjectRoot(searchFrom, root)
		if projectDir != "" {
			switch projectType {
			case "maven":
				cmd = exec.CommandContext(ctx, "mvn", "test", "-q")
				runner = "maven"
			case "gradle-wrapper":
				cmd = exec.CommandContext(ctx, filepath.Join(projectDir, "gradlew"), "test")
				runner = "gradle-wrapper"
			case "gradle":
				cmd = exec.CommandContext(ctx, "gradle", "test")
				runner = "gradle"
			case "go":
				cmd = exec.CommandContext(ctx, "go", "test", "./...")
				runner = "go"
			case "python":
				cmd = exec.CommandContext(ctx, "python3", "-m", "pytest")
				runner = "pytest"
			}
			cmd.Dir = projectDir
		} else if !info.IsDir() {
			switch language {
			case "python", "py":
				cmd = exec.CommandContext(ctx, "python3", "-m", "py_compile", path)
				runner = "py_compile"
			case "java":
				cmd = exec.CommandContext(ctx, "javac", path)
				runner = "javac"
			default:
				cmd = exec.CommandContext(ctx, "go", "test", "./...")
				runner = "go"
			}
			cmd.Dir = root
		} else {
			return map[string]any{"status": "failed", "error": "no supported project manifest found", "path": rel}, nil
		}
		out, err := cmd.CombinedOutput()
		result := map[string]any{"output": string(out), "status": "passed", "runner": runner, "path": rel}
		if err != nil {
			result["status"] = "failed"
			result["error"] = err.Error()
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unknown local file tool %s", t.Name)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func findProjectRoot(start, workspaceRoot string) (string, string) {
	dir := filepath.Clean(start)
	root := filepath.Clean(workspaceRoot)
	for {
		switch {
		case fileExists(filepath.Join(dir, "pom.xml")):
			return dir, "maven"
		case fileExists(filepath.Join(dir, "gradlew")):
			return dir, "gradle-wrapper"
		case fileExists(filepath.Join(dir, "build.gradle")) || fileExists(filepath.Join(dir, "build.gradle.kts")):
			return dir, "gradle"
		case fileExists(filepath.Join(dir, "go.mod")):
			return dir, "go"
		case fileExists(filepath.Join(dir, "pyproject.toml")) || fileExists(filepath.Join(dir, "pytest.ini")):
			return dir, "python"
		}
		if dir == root {
			return "", ""
		}
		parent := filepath.Dir(dir)
		if parent == dir || (parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator))) {
			return "", ""
		}
		dir = parent
	}
}

func inspectProject(root string) (map[string]any, error) {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("project path must be a directory")
	}
	if manifest := filepath.Join(root, "pom.xml"); fileExists(manifest) {
		b, err := os.ReadFile(manifest)
		if err != nil {
			return nil, err
		}
		var pom struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
			Parent     struct {
				GroupID    string `xml:"groupId"`
				ArtifactID string `xml:"artifactId"`
				Version    string `xml:"version"`
			} `xml:"parent"`
			Properties struct {
				JavaVersion string `xml:"java.version"`
			} `xml:"properties"`
			Dependencies []struct {
				GroupID    string `xml:"groupId"`
				ArtifactID string `xml:"artifactId"`
				Version    string `xml:"version"`
				Scope      string `xml:"scope"`
			} `xml:"dependencies>dependency"`
		}
		if err := xml.Unmarshal(b, &pom); err != nil {
			return nil, fmt.Errorf("parse pom.xml: %w", err)
		}
		deps := make([]map[string]string, 0, len(pom.Dependencies))
		for _, d := range pom.Dependencies {
			deps = append(deps, map[string]string{"groupId": d.GroupID, "artifactId": d.ArtifactID, "version": d.Version, "scope": d.Scope})
		}
		return map[string]any{
			"project_type": "maven", "manifest": "pom.xml", "test_runner": "mvn test -q",
			"groupId": pom.GroupID, "artifactId": pom.ArtifactID, "version": pom.Version,
			"java_version": pom.Properties.JavaVersion,
			"parent":       map[string]string{"groupId": pom.Parent.GroupID, "artifactId": pom.Parent.ArtifactID, "version": pom.Parent.Version},
			"dependencies": deps,
		}, nil
	}
	for _, name := range []string{"build.gradle.kts", "build.gradle"} {
		if manifest := filepath.Join(root, name); fileExists(manifest) {
			b, err := os.ReadFile(manifest)
			if err != nil {
				return nil, err
			}
			runner := "gradle test"
			if fileExists(filepath.Join(root, "gradlew")) {
				runner = "./gradlew test"
			}
			return map[string]any{"project_type": "gradle", "manifest": name, "test_runner": runner, "manifest_content": string(b)}, nil
		}
	}
	if manifest := filepath.Join(root, "go.mod"); fileExists(manifest) {
		b, err := os.ReadFile(manifest)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project_type": "go", "manifest": "go.mod", "test_runner": "go test ./...", "manifest_content": string(b)}, nil
	}
	for _, name := range []string{"pyproject.toml", "pytest.ini"} {
		if manifest := filepath.Join(root, name); fileExists(manifest) {
			b, err := os.ReadFile(manifest)
			if err != nil {
				return nil, err
			}
			return map[string]any{"project_type": "python", "manifest": name, "test_runner": "python3 -m pytest", "manifest_content": string(b)}, nil
		}
	}
	return map[string]any{"project_type": "unknown", "test_runner": "", "manifest": ""}, nil
}

func cleanGeneratedSource(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if nl := strings.IndexByte(content, '\n'); nl >= 0 {
			content = content[nl+1:]
		}
		if end := strings.Index(content, "\n```"); end >= 0 {
			content = content[:end]
		}
	} else if end := strings.Index(content, "\n```"); end >= 0 {
		content = content[:end]
	}
	return strings.TrimSpace(content)
}
