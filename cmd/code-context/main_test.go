package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitDiffCmd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cli-git-diff-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "a.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGitCLI(t, tmpDir, "init")
	runGitCLI(t, tmpDir, "add", "a.go")
	runGitCLI(t, tmpDir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	if err := os.WriteFile(path, []byte("package main\n\nfunc A() string { return \"a\" }\n"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}

	prevRoot, prevDB := root, dbPath
	root = tmpDir
	dbPath = filepath.Join(tmpDir, "index.db")
	defer func() {
		root, dbPath = prevRoot, prevDB
	}()

	cmd := newGitDiffCmd()
	cmd.SetArgs([]string{"--state", "unstaged", "--context", "1"})

	out, err := captureStdout(func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("execute git-diff cmd: %v", err)
	}
	if !strings.Contains(out, "File: a.go") {
		t.Fatalf("expected file output, got:\n%s", out)
	}
	if !strings.Contains(out, "@@ -") {
		t.Fatalf("expected hunk header output, got:\n%s", out)
	}
}

func runGitCLI(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func captureStdout(fn func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return buf.String(), runErr
}
