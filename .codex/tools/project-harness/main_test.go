package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheckDocumentation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.md"), []byte("現在の仕様を記述する。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDocumentation(root, []string{"ok.md"}); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad.md"), []byte("旧版との比較\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDocumentation(root, []string{"bad.md"}); err == nil {
		t.Fatal("prohibited term was not detected")
	}
}

func TestHasTrailingWhitespace(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"clean LF", []byte("one\ntwo\n"), false},
		{"clean CRLF", []byte("one\r\ntwo\r\n"), false},
		{"space LF", []byte("one \ntwo\n"), true},
		{"tab CRLF", []byte("one\t\r\ntwo\r\n"), true},
		{"space at EOF", []byte("one "), true},
		{"binary excluded", []byte{'a', 0, ' '}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasTrailingWhitespace(test.content); got != test.want {
				t.Fatalf("hasTrailingWhitespace() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSplitNUL(t *testing.T) {
	want := []string{"ordinary.txt", "line\nbreak.md"}
	if got := splitNUL([]byte("ordinary.txt\x00line\nbreak.md\x00")); !reflect.DeepEqual(got, want) {
		t.Fatalf("splitNUL() = %#v, want %#v", got, want)
	}
}

func TestWithTempBuildOutputAlwaysCleansUp(t *testing.T) {
	wantErr := errors.New("build failed")
	tests := []struct {
		name     string
		buildErr error
	}{
		{"success", nil},
		{"failure", wantErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var tempDir string
			err := withTempBuildOutput(func(outputPath string) error {
				tempDir = filepath.Dir(outputPath)
				if err := os.WriteFile(outputPath, []byte("binary"), 0o600); err != nil {
					return err
				}
				return test.buildErr
			})
			if !errors.Is(err, test.buildErr) {
				t.Fatalf("withTempBuildOutput() error = %v, want %v", err, test.buildErr)
			}
			if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
				t.Fatalf("temporary build directory remains: %s", tempDir)
			}
		})
	}
}

func TestCheckGitWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		want  bool
	}{
		{"clean", func(*testing.T, string) {}, false},
		{"unstaged", func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, "tracked.txt"), "bad \n")
		}, true},
		{"staged", func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, "tracked.txt"), "bad \n")
			runTestCommand(t, root, "git", "add", "tracked.txt")
		}, true},
		{"untracked", func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, "untracked.txt"), "bad \n")
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newTestRepository(t)
			test.setup(t, root)
			err := checkGitWhitespace(root)
			if (err != nil) != test.want {
				t.Fatalf("checkGitWhitespace() error = %v, want error %v", err, test.want)
			}
		})
	}
}

func newTestRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runTestCommand(t, root, "git", "init", "--quiet")
	writeTestFile(t, filepath.Join(root, "tracked.txt"), "clean\n")
	runTestCommand(t, root, "git", "add", "tracked.txt")
	return root
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runTestCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
}
