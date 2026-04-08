package engine

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
)

type GitState string

type DiffHunk struct {
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
	Content  string `json:"content"`
}

type GitDiffFile struct {
	Path     string     `json:"path"`
	Hunks    []DiffHunk `json:"hunks"`
	Snippets []string   `json:"snippets"`
}

const (
	GitStateUnstaged GitState = "unstaged"
	GitStateStaged   GitState = "staged"
	GitStateAll      GitState = "all"
)

func ParseGitState(v string) (GitState, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", string(GitStateUnstaged):
		return GitStateUnstaged, nil
	case string(GitStateStaged):
		return GitStateStaged, nil
	case string(GitStateAll):
		return GitStateAll, nil
	default:
		return "", fmt.Errorf("invalid git state %q (must be unstaged, staged, or all)", v)
	}
}

func (e *Engine) GitChangedFiles(ctx context.Context, state GitState) ([]string, error) {
	if err := e.ensureGitRepo(ctx); err != nil {
		return nil, err
	}

	var files []string
	switch state {
	case GitStateUnstaged:
		changed, err := e.gitDiffNames(ctx, false)
		if err != nil {
			return nil, err
		}
		files = changed
	case GitStateStaged:
		changed, err := e.gitDiffNames(ctx, true)
		if err != nil {
			return nil, err
		}
		files = changed
	case GitStateAll:
		unstaged, err := e.gitDiffNames(ctx, false)
		if err != nil {
			return nil, err
		}
		staged, err := e.gitDiffNames(ctx, true)
		if err != nil {
			return nil, err
		}
		files = append(files, unstaged...)
		files = append(files, staged...)
	default:
		return nil, fmt.Errorf("unsupported git state: %s", state)
	}

	return dedupStrings(files), nil
}

func (e *Engine) SnapshotGit(ctx context.Context, state GitState, maxFiles int) (*Snapshot, error) {
	if maxFiles <= 0 {
		maxFiles = 5
	}

	changed, err := e.GitChangedFiles(ctx, state)
	if err != nil {
		return nil, err
	}

	if len(changed) == 0 {
		return &Snapshot{
			Query:   fmt.Sprintf("git:%s", state),
			Summary: fmt.Sprintf("No %s changed files in git working tree", state),
		}, nil
	}

	var files []FileSummary
	var symbols []api.Symbol
	for _, path := range changed {
		if len(files) >= maxFiles {
			break
		}

		fs, err := e.Explain(ctx, path)
		if err != nil {
			continue
		}

		files = append(files, *fs)
		symbols = append(symbols, fs.Symbols...)
	}

	return &Snapshot{
		Query:   fmt.Sprintf("git:%s", state),
		Files:   files,
		Symbols: symbols,
		Summary: fmt.Sprintf("Selected %d of %d %s changed files", len(files), len(changed), state),
	}, nil
}

func (e *Engine) DiffImpactGit(ctx context.Context, state GitState, depth int) ([]DiffImpact, error) {
	changed, err := e.GitChangedFiles(ctx, state)
	if err != nil {
		return nil, err
	}

	var impacts []DiffImpact
	for _, path := range changed {
		impact, err := e.DiffImpact(ctx, path, depth)
		if err != nil {
			continue
		}
		impacts = append(impacts, *impact)
	}

	return impacts, nil
}

func (e *Engine) GitDiff(ctx context.Context, state GitState, contextLines int) ([]GitDiffFile, error) {
	if err := e.ensureGitRepo(ctx); err != nil {
		return nil, err
	}

	if contextLines < 0 {
		contextLines = 0
	}

	switch state {
	case GitStateUnstaged:
		return e.gitDiff(ctx, false, contextLines)
	case GitStateStaged:
		return e.gitDiff(ctx, true, contextLines)
	case GitStateAll:
		unstaged, err := e.gitDiff(ctx, false, contextLines)
		if err != nil {
			return nil, err
		}
		staged, err := e.gitDiff(ctx, true, contextLines)
		if err != nil {
			return nil, err
		}
		return mergeGitDiffFiles(unstaged, staged), nil
	default:
		return nil, fmt.Errorf("unsupported git state: %s", state)
	}
}

func (e *Engine) ensureGitRepo(ctx context.Context) error {
	_, err := e.runGit(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("not a git repository at %s: %w", e.root, err)
	}
	return nil
}

func (e *Engine) gitDiffNames(ctx context.Context, staged bool) ([]string, error) {
	args := []string{"diff", "--name-only", "--diff-filter=ACMR"}
	if staged {
		args = append(args, "--cached")
	}
	out, err := e.runGit(ctx, args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func (e *Engine) gitDiff(ctx context.Context, staged bool, contextLines int) ([]GitDiffFile, error) {
	args := []string{"diff", "--no-color", "--diff-filter=ACMR", fmt.Sprintf("--unified=%d", contextLines)}
	if staged {
		args = append(args, "--cached")
	}
	out, err := e.runGit(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseGitDiff(out, contextLines)
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseGitDiff(out string, contextLines int) ([]GitDiffFile, error) {
	lines := strings.Split(out, "\n")
	results := make([]GitDiffFile, 0)

	var curFile *GitDiffFile
	var curHunk *DiffHunk

	flushHunk := func() {
		if curFile == nil || curHunk == nil {
			return
		}
		curFile.Hunks = append(curFile.Hunks, *curHunk)
		curFile.Snippets = append(curFile.Snippets, extractChangedSnippets(curHunk.Content, contextLines)...)
		curHunk = nil
	}

	flushFile := func() {
		if curFile == nil {
			return
		}
		flushHunk()
		if curFile.Path == "" {
			curFile = nil
			return
		}
		if len(curFile.Hunks) > 0 {
			results = append(results, *curFile)
		}
		curFile = nil
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			curFile = &GitDiffFile{Path: parseDiffHeaderPath(line)}
		case strings.HasPrefix(line, "+++ "):
			if curFile != nil {
				if p := parsePlusPlusPlusPath(line); p != "" {
					curFile.Path = p
				}
			}
		case strings.HasPrefix(line, "@@ "):
			if curFile == nil {
				continue
			}
			flushHunk()
			h, err := parseDiffHunkHeader(line)
			if err != nil {
				return nil, err
			}
			curHunk = &h
		default:
			if curHunk != nil {
				if curHunk.Content == "" {
					curHunk.Content = line
				} else {
					curHunk.Content += "\n" + line
				}
			}
		}
	}

	flushFile()
	return results, nil
}

func parseDiffHeaderPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return ""
	}
	return strings.TrimPrefix(parts[3], "b/")
}

func parsePlusPlusPlusPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return ""
	}
	if parts[1] == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(parts[1], "b/")
}

func parseDiffHunkHeader(line string) (DiffHunk, error) {
	m := hunkHeaderRe.FindStringSubmatch(line)
	if len(m) != 5 {
		return DiffHunk{}, fmt.Errorf("invalid diff hunk header: %q", line)
	}

	oldStart, err := strconv.Atoi(m[1])
	if err != nil {
		return DiffHunk{}, fmt.Errorf("invalid old start in hunk header %q: %w", line, err)
	}
	oldLines := 1
	if m[2] != "" {
		oldLines, err = strconv.Atoi(m[2])
		if err != nil {
			return DiffHunk{}, fmt.Errorf("invalid old lines in hunk header %q: %w", line, err)
		}
	}

	newStart, err := strconv.Atoi(m[3])
	if err != nil {
		return DiffHunk{}, fmt.Errorf("invalid new start in hunk header %q: %w", line, err)
	}
	newLines := 1
	if m[4] != "" {
		newLines, err = strconv.Atoi(m[4])
		if err != nil {
			return DiffHunk{}, fmt.Errorf("invalid new lines in hunk header %q: %w", line, err)
		}
	}

	return DiffHunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
	}, nil
}

func extractChangedSnippets(content string, contextLines int) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	windows := make([][2]int, 0)

	for i, line := range lines {
		if line == "" {
			continue
		}
		prefix := line[0]
		if prefix != '+' && prefix != '-' {
			continue
		}

		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}

		if len(windows) == 0 || start > windows[len(windows)-1][1]+1 {
			windows = append(windows, [2]int{start, end})
		} else if end > windows[len(windows)-1][1] {
			windows[len(windows)-1][1] = end
		}
	}

	if len(windows) == 0 {
		return []string{content}
	}

	snippets := make([]string, 0, len(windows))
	for _, w := range windows {
		snippets = append(snippets, strings.Join(lines[w[0]:w[1]+1], "\n"))
	}
	return snippets
}

func mergeGitDiffFiles(groups ...[]GitDiffFile) []GitDiffFile {
	byPath := make(map[string]*GitDiffFile)
	for _, group := range groups {
		for _, f := range group {
			existing := byPath[f.Path]
			if existing == nil {
				copyFile := f
				byPath[f.Path] = &copyFile
				continue
			}
			existing.Hunks = append(existing.Hunks, f.Hunks...)
			existing.Snippets = append(existing.Snippets, f.Snippets...)
		}
	}

	out := make([]GitDiffFile, 0, len(byPath))
	for _, f := range byPath {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

func (e *Engine) runGit(ctx context.Context, args ...string) (string, error) {
	allArgs := append([]string{"-C", e.root}, args...)
	cmd := exec.CommandContext(ctx, "git", allArgs...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(err.Error())
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), msg)
	}

	return stdout.String(), nil
}

func splitLines(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, "\n")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func dedupStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	unique := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		unique = append(unique, item)
	}
	sort.Strings(unique)
	return unique
}
