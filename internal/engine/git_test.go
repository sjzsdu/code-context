package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseGitState(t *testing.T) {
	tests := []struct {
		in      string
		want    GitState
		wantErr bool
	}{
		{in: "", want: GitStateUnstaged},
		{in: "unstaged", want: GitStateUnstaged},
		{in: "staged", want: GitStateStaged},
		{in: "all", want: GitStateAll},
		{in: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		got, err := ParseGitState(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ParseGitState(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseGitState(%q): unexpected error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ParseGitState(%q): got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGitChangedFilesAndGitContext(t *testing.T) {
	ctx := context.Background()
	eng, cleanup := setupIndexedGitRepo(t)
	defer cleanup()

	unstaged, err := eng.GitChangedFiles(ctx, GitStateUnstaged)
	if err != nil {
		t.Fatalf("GitChangedFiles(unstaged) failed: %v", err)
	}
	if !reflect.DeepEqual(unstaged, []string{"a.go"}) {
		t.Fatalf("unstaged files: got %v, want [a.go]", unstaged)
	}

	staged, err := eng.GitChangedFiles(ctx, GitStateStaged)
	if err != nil {
		t.Fatalf("GitChangedFiles(staged) failed: %v", err)
	}
	if !reflect.DeepEqual(staged, []string{"b.go"}) {
		t.Fatalf("staged files: got %v, want [b.go]", staged)
	}

	all, err := eng.GitChangedFiles(ctx, GitStateAll)
	if err != nil {
		t.Fatalf("GitChangedFiles(all) failed: %v", err)
	}
	if !reflect.DeepEqual(all, []string{"a.go", "b.go"}) {
		t.Fatalf("all files: got %v, want [a.go b.go]", all)
	}

	snapshot, err := eng.SnapshotGit(ctx, GitStateUnstaged, 5)
	if err != nil {
		t.Fatalf("SnapshotGit failed: %v", err)
	}
	if snapshot.Query != "git:unstaged" {
		t.Fatalf("snapshot query: got %q", snapshot.Query)
	}
	if len(snapshot.Files) != 1 || snapshot.Files[0].Path != "a.go" {
		t.Fatalf("snapshot files: got %+v, want [a.go]", snapshot.Files)
	}

	impacts, err := eng.DiffImpactGit(ctx, GitStateStaged, 2)
	if err != nil {
		t.Fatalf("DiffImpactGit failed: %v", err)
	}
	if len(impacts) != 1 || impacts[0].File != "b.go" {
		t.Fatalf("diff impact files: got %+v, want [b.go]", impacts)
	}

	gitImpact, err := eng.ImpactGit(ctx, GitStateAll, 2)
	if err != nil {
		t.Fatalf("ImpactGit failed: %v", err)
	}
	if len(gitImpact.ChangedFiles) != 2 || len(gitImpact.FileImpacts) == 0 || gitImpact.Summary == "" {
		t.Fatalf("unexpected git impact: %+v", gitImpact)
	}
	if gitImpact.Risk.Level == "" || len(gitImpact.RecommendedTests) == 0 || len(gitImpact.TestCommands) == 0 {
		t.Fatalf("expected risk and test recommendations, got %+v", gitImpact)
	}

	diffs, err := eng.GitDiff(ctx, GitStateUnstaged, 1)
	if err != nil {
		t.Fatalf("GitDiff(unstaged) failed: %v", err)
	}
	if len(diffs) != 1 || diffs[0].Path != "a.go" {
		t.Fatalf("git diff files: got %+v, want [a.go]", diffs)
	}
	if len(diffs[0].Hunks) == 0 {
		t.Fatalf("expected at least one hunk, got none")
	}
	if len(diffs[0].Snippets) == 0 {
		t.Fatalf("expected at least one snippet, got none")
	}

	allDiffs, err := eng.GitDiff(ctx, GitStateAll, 1)
	if err != nil {
		t.Fatalf("GitDiff(all) failed: %v", err)
	}
	if len(allDiffs) != 2 {
		t.Fatalf("expected 2 diff files for all state, got %d (%+v)", len(allDiffs), allDiffs)
	}
}

func TestParseGitDiffHunkHeader(t *testing.T) {
	h, err := parseDiffHunkHeader("@@ -10,2 +20,3 @@ func A()")
	if err != nil {
		t.Fatalf("parseDiffHunkHeader failed: %v", err)
	}
	if h.OldStart != 10 || h.OldLines != 2 || h.NewStart != 20 || h.NewLines != 3 {
		t.Fatalf("unexpected parsed hunk: %+v", h)
	}

	if _, err := parseDiffHunkHeader("@@ invalid @@"); err == nil {
		t.Fatalf("expected error for invalid header")
	}
}

func TestParseGitDiffAndSnippetExtraction(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/a.go b/a.go",
		"index 123..456 100644",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1,3 +1,4 @@",
		" package main",
		"-func A() {",
		"+func A() string {",
		"+\treturn \"a\"",
		" }",
	}, "\n")

	files, err := parseGitDiff(raw, 1)
	if err != nil {
		t.Fatalf("parseGitDiff failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "a.go" {
		t.Fatalf("expected path a.go, got %q", files[0].Path)
	}
	if len(files[0].Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(files[0].Hunks))
	}
	if len(files[0].Snippets) == 0 {
		t.Fatalf("expected snippet extraction, got none")
	}
}

func TestRecommendedTestCommands(t *testing.T) {
	tests := []string{
		"internal/engine/git_test.go",
		"internal/parser/parser_test.go",
		"tests/test_api.py",
		"web/app.spec.ts",
		"src/UserServiceTest.java",
		"crates/core_test.rs",
		"internal/engine/git_test.go",
	}
	commands := recommendedTestCommands(tests)
	want := []string{
		"go test ./internal/engine",
		"go test ./internal/parser",
		"pytest tests/test_api.py",
		"npm test -- web/app.spec.ts",
		"mvn test -Dtest=UserServiceTest",
		"cargo test",
	}
	if len(commands) != len(want) {
		t.Fatalf("expected %d commands, got %d: %+v", len(want), len(commands), commands)
	}
	for i, command := range commands {
		if command.Command != want[i] {
			t.Fatalf("command[%d] got %q, want %q", i, command.Command, want[i])
		}
	}
	if len(commands[0].Files) != 1 || commands[0].Files[0] != "internal/engine/git_test.go" {
		t.Fatalf("expected duplicate go test file to be deduped, got %+v", commands[0].Files)
	}
}

func setupIndexedGitRepo(t *testing.T) (*Engine, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "engine-git-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	writeFile(t, filepath.Join(tmpDir, "a.go"), "package main\n\nimport \"fmt\"\n\nfunc A() { fmt.Println(\"a\") }\n")
	writeFile(t, filepath.Join(tmpDir, "b.go"), "package main\n\nimport \"fmt\"\n\nfunc B() { fmt.Println(\"b\") }\n")
	writeFile(t, filepath.Join(tmpDir, "a_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	writeFile(t, filepath.Join(tmpDir, "b_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")

	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "add", "a.go", "b.go")
	runGit(t, tmpDir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")

	dbPath := filepath.Join(tmpDir, "index.db")
	eng, err := New(tmpDir, dbPath)
	if err != nil {
		t.Fatalf("engine.New failed: %v", err)
	}
	if _, err := eng.Index(context.Background(), false); err != nil {
		t.Fatalf("index failed: %v", err)
	}

	writeFile(t, filepath.Join(tmpDir, "a.go"), "package main\n\nimport \"fmt\"\n\nfunc A() { fmt.Println(\"a2\") }\n")
	writeFile(t, filepath.Join(tmpDir, "b.go"), "package main\n\nimport \"fmt\"\n\nfunc B() { fmt.Println(\"b2\") }\n")
	runGit(t, tmpDir, "add", "b.go")

	cleanup := func() {
		_ = eng.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return eng, cleanup
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
