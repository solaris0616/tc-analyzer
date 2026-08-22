package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var prohibitedTerms = []string{
	"Python",
	"旧版",
	"旧形式",
	"旧スキーマ",
	"既存DB",
	"互換",
	"移行",
}

var documentationTargets = []string{
	"design",
	"README.md",
	"AGENTS.md",
	filepath.Join(".codex", "agents"),
	filepath.Join(".codex", "instructions"),
	filepath.Join(".codex", "skills"),
}

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: project-harness <fmt|docs-check|verify>")
	}

	root, err := os.Getwd()
	if err != nil {
		fatalf("determine project root: %v", err)
	}

	switch os.Args[1] {
	case "fmt":
		err = format(root)
	case "docs-check":
		err = checkDocumentation(root, documentationTargets)
		if err == nil {
			fmt.Println("Documentation terminology check passed.")
		}
	case "verify":
		err = verify(root)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func format(root string) error {
	if err := run(root, "go", "fmt", "./..."); err != nil {
		return err
	}
	toolFiles, err := goFilesUnder(filepath.Join(root, ".codex", "tools"))
	if err != nil {
		return err
	}
	if len(toolFiles) == 0 {
		return nil
	}
	return run(root, "gofmt", append([]string{"-w"}, toolFiles...)...)
}

func verify(root string) error {
	goFiles, err := projectGoFiles(root)
	if err != nil {
		return err
	}
	output, err := capture(root, "gofmt", append([]string{"-l"}, goFiles...)...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(output)) != "" {
		return fmt.Errorf("Go files are not formatted; run mise run fmt:\n%s", output)
	}

	commands := [][]string{
		{"go", "test", "-count=1", "./..."},
		{"go", "test", "-count=1", "./.codex/tools/..."},
		{"go", "vet", "./..."},
		{"go", "vet", "./.codex/tools/..."},
	}
	for _, command := range commands {
		if err := run(root, command[0], command[1:]...); err != nil {
			return err
		}
	}

	if err := withTempBuildOutput(func(outputPath string) error {
		return run(root, "go", "build", "-o", outputPath, "./cmd/tc-analyzer")
	}); err != nil {
		return err
	}
	if err := checkGitWhitespace(root); err != nil {
		return err
	}
	if err := checkDocumentation(root, documentationTargets); err != nil {
		return err
	}
	fmt.Println("Documentation terminology check passed.")
	if err := run(root, "go", "run", "./.codex/tools/definition-check"); err != nil {
		return err
	}
	fmt.Println("All verification checks passed.")
	return nil
}

func run(dir, name string, args ...string) error {
	fmt.Printf("+ %s %s\n", name, strings.Join(args, " "))
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func capture(dir, name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s failed: %s", name, exitErr.Stderr)
		}
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return output, nil
}

func projectGoFiles(root string) ([]string, error) {
	output, err := capture(root, "go", "list", "-json", "./...")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	files := make(map[string]struct{})
	for decoder.More() {
		var pkg struct{ Dir string }
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		packageFiles, err := goFilesUnder(pkg.Dir)
		if err != nil {
			return nil, err
		}
		for _, path := range packageFiles {
			files[path] = struct{}{}
		}
	}
	toolFiles, err := goFilesUnder(filepath.Join(root, ".codex", "tools"))
	if err != nil {
		return nil, err
	}
	for _, path := range toolFiles {
		files[path] = struct{}{}
	}
	result := make([]string, 0, len(files))
	for path := range files {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func goFilesUnder(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func checkDocumentation(root string, targets []string) error {
	for _, target := range targets {
		path := filepath.Join(root, target)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := checkDocument(path, root); err != nil {
				return err
			}
			continue
		}
		if err := filepath.WalkDir(path, func(documentPath string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			return checkDocument(documentPath, root)
		}); err != nil {
			return err
		}
	}
	return nil
}

func checkDocument(path, root string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, term := range prohibitedTerms {
		if line := lineContaining(string(content), term); line > 0 {
			relative, _ := filepath.Rel(root, path)
			return fmt.Errorf("%s:%d contains prohibited historical/comparison term %q", relative, line, term)
		}
	}
	return nil
}

func lineContaining(content, term string) int {
	index := strings.Index(content, term)
	if index < 0 {
		return 0
	}
	return strings.Count(content[:index], "\n") + 1
}

func checkUntrackedWhitespace(root string) error {
	output, err := capture(root, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	for _, name := range splitNUL(output) {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if hasTrailingWhitespace(content) {
			return fmt.Errorf("untracked file has trailing whitespace: %s", name)
		}
	}
	return nil
}

func checkGitWhitespace(root string) error {
	if err := run(root, "git", "diff", "--check"); err != nil {
		return err
	}
	if err := run(root, "git", "diff", "--cached", "--check"); err != nil {
		return err
	}
	return checkUntrackedWhitespace(root)
}

func splitNUL(content []byte) []string {
	parts := bytes.Split(content, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}

func hasTrailingWhitespace(content []byte) bool {
	if bytes.IndexByte(content, 0) >= 0 {
		return false
	}
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
			return true
		}
	}
	return false
}

func withTempBuildOutput(build func(string) error) error {
	tempDir, err := os.MkdirTemp("", "tc-analyzer-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	name := "tc-analyzer"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return build(filepath.Join(tempDir, name))
}
