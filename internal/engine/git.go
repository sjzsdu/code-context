package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

type ChangedSymbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	FilePath string `json:"file"`
	Line     int    `json:"line"`
}

type RiskScore struct {
	Level   string   `json:"level"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

type TestCommand struct {
	Command string   `json:"command"`
	Files   []string `json:"files,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type TestImpact struct {
	State            GitState        `json:"state"`
	ChangedFiles     []string        `json:"changed_files"`
	ChangedSymbols   []ChangedSymbol `json:"changed_symbols"`
	RecommendedTests []string        `json:"recommended_tests"`
	TestCommands     []TestCommand   `json:"recommended_test_commands,omitempty"`
	Summary          string          `json:"summary"`
}

type GitImpact struct {
	State            GitState        `json:"state"`
	ChangedFiles     []string        `json:"changed_files"`
	ChangedSymbols   []ChangedSymbol `json:"changed_symbols"`
	FileImpacts      []DiffImpact    `json:"file_impacts"`
	SymbolImpacts    []SymbolImpact  `json:"symbol_impacts"`
	RecommendedTests []string        `json:"recommended_tests,omitempty"`
	TestCommands     []TestCommand   `json:"recommended_test_commands,omitempty"`
	Risk             RiskScore       `json:"risk"`
	Summary          string          `json:"summary"`
}

type ReviewContext struct {
	State                GitState           `json:"state"`
	ChangedFiles         []string           `json:"changed_files"`
	ChangedSymbols       []ChangedSymbol    `json:"changed_symbols"`
	Routes               []api.Route        `json:"routes"`
	RelatedDocs          []api.DocumentLink `json:"related_docs"`
	RecommendedTests     []string           `json:"recommended_tests"`
	TestCommands         []TestCommand      `json:"recommended_test_commands,omitempty"`
	Risk                 RiskScore          `json:"risk"`
	SuggestedReviewOrder []string           `json:"suggested_review_order"`
	Summary              string             `json:"summary"`
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

	recommendedFiles := snapshotRecommendedFiles(files, maxFiles)
	analysis := mergeGraphAnalysesFromFiles(files)
	summary := fmt.Sprintf("Selected %d of %d %s changed files", len(files), len(changed), state)
	if len(recommendedFiles) > 0 {
		summary += fmt.Sprintf(". Recommended next files: %s", strings.Join(recommendedFiles, ", "))
	}

	return &Snapshot{
		Query:            fmt.Sprintf("git:%s", state),
		Files:            files,
		Symbols:          symbols,
		Summary:          summary,
		RecommendedFiles: recommendedFiles,
		Analysis:         analysis,
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

func (e *Engine) ImpactGit(ctx context.Context, state GitState, depth int) (*GitImpact, error) {
	files, err := e.GitChangedFiles(ctx, state)
	if err != nil {
		return nil, err
	}
	if depth <= 0 {
		depth = 3
	}

	var fileImpacts []DiffImpact
	for _, path := range files {
		impact, err := e.DiffImpact(ctx, path, depth)
		if err != nil {
			continue
		}
		fileImpacts = append(fileImpacts, *impact)
	}

	diffs, _ := e.GitDiff(ctx, state, 0)
	changedSymbols := e.changedSymbolsForDiffs(ctx, diffs)
	seenSymbols := map[string]bool{}
	var symbolImpacts []SymbolImpact
	for _, sym := range changedSymbols {
		key := sym.FilePath + ":" + sym.Name + fmt.Sprint(sym.Line)
		if seenSymbols[key] {
			continue
		}
		seenSymbols[key] = true
		impact, err := e.SymbolImpact(ctx, sym.Name)
		if err != nil {
			continue
		}
		symbolImpacts = append(symbolImpacts, *impact)
	}
	tests := e.recommendedTestsForFilesAndSymbols(ctx, files, changedSymbols)
	commands := recommendedTestCommands(tests)
	risk := e.gitImpactRisk(ctx, files, changedSymbols, fileImpacts, symbolImpacts, tests)

	return &GitImpact{State: state, ChangedFiles: files, ChangedSymbols: changedSymbols, FileImpacts: fileImpacts, SymbolImpacts: symbolImpacts, RecommendedTests: tests, TestCommands: commands, Risk: risk, Summary: fmt.Sprintf("%d changed files, %d changed symbols, %d file impacts, %d symbol impacts, risk %s (%d)", len(files), len(changedSymbols), len(fileImpacts), len(symbolImpacts), risk.Level, risk.Score)}, nil
}

func (e *Engine) gitImpactRisk(ctx context.Context, files []string, syms []ChangedSymbol, fileImpacts []DiffImpact, symbolImpacts []SymbolImpact, tests []string) RiskScore {
	score := 0
	reasons := []string{}
	if len(files) > 5 {
		score += 15
		reasons = append(reasons, fmt.Sprintf("large change set: %d files", len(files)))
	}
	if len(tests) == 0 && len(files) > 0 {
		score += 20
		reasons = append(reasons, "no related tests found")
	}
	for _, impact := range fileImpacts {
		if len(impact.Dependents) > 3 {
			score += 10
			reasons = append(reasons, fmt.Sprintf("%s has %d dependent files", impact.File, len(impact.Dependents)))
		}
	}
	for _, impact := range symbolImpacts {
		if len(impact.Routes) > 0 {
			score += 20
			reasons = append(reasons, fmt.Sprintf("%s handles %d routes", impact.Symbol.Name, len(impact.Routes)))
		}
		if len(impact.Callers) > 5 {
			score += 15
			reasons = append(reasons, fmt.Sprintf("%s has %d callers", impact.Symbol.Name, len(impact.Callers)))
		}
		if impact.Risk.Score > 0 {
			score += min(impact.Risk.Score, 20)
			reasons = append(reasons, impact.Risk.Reasons...)
		}
	}
	if len(symbolImpacts) == 0 && len(syms) > 0 {
		score += 10
		reasons = append(reasons, "changed symbols could not be fully resolved for symbol impact")
	}
	level := "low"
	if score >= 60 {
		level = "high"
	} else if score >= 30 {
		level = "medium"
	}
	return RiskScore{Level: level, Score: score, Reasons: dedupStrings(reasons)}
}

func (e *Engine) TestImpact(ctx context.Context, state GitState) (*TestImpact, error) {
	files, err := e.GitChangedFiles(ctx, state)
	if err != nil {
		return nil, err
	}
	diffs, _ := e.GitDiff(ctx, state, 0)
	changedSymbols := e.changedSymbolsForDiffs(ctx, diffs)
	tests := e.recommendedTestsForFilesAndSymbols(ctx, files, changedSymbols)
	commands := recommendedTestCommands(tests)
	return &TestImpact{State: state, ChangedFiles: files, ChangedSymbols: changedSymbols, RecommendedTests: tests, TestCommands: commands, Summary: fmt.Sprintf("%d changed files, %d changed symbols, %d recommended tests", len(files), len(changedSymbols), len(tests))}, nil
}

func (e *Engine) ReviewContext(ctx context.Context, state GitState) (*ReviewContext, error) {
	files, err := e.GitChangedFiles(ctx, state)
	if err != nil {
		return nil, err
	}
	diffs, _ := e.GitDiff(ctx, state, 0)
	changedSymbols := e.changedSymbolsForDiffs(ctx, diffs)
	routes := e.routesForFiles(ctx, files)
	docs := e.docsForFilesAndSymbols(ctx, files, changedSymbols)
	tests := e.recommendedTestsForFilesAndSymbols(ctx, files, changedSymbols)
	commands := recommendedTestCommands(tests)
	risk := e.reviewRisk(ctx, files, changedSymbols, routes, tests)
	order := suggestedReviewOrder(files, routes, tests)
	return &ReviewContext{State: state, ChangedFiles: files, ChangedSymbols: changedSymbols, Routes: routes, RelatedDocs: docs, RecommendedTests: tests, TestCommands: commands, Risk: risk, SuggestedReviewOrder: order, Summary: fmt.Sprintf("Review %d files (%d symbols). Risk: %s (%d)", len(files), len(changedSymbols), risk.Level, risk.Score)}, nil
}

func (e *Engine) changedSymbolsForDiffs(ctx context.Context, diffs []GitDiffFile) []ChangedSymbol {
	seen := map[string]bool{}
	var out []ChangedSymbol
	for _, df := range diffs {
		syms, err := e.store.GetFileSymbols(ctx, df.Path)
		if err != nil || len(syms) == 0 {
			continue
		}
		for _, h := range df.Hunks {
			start, end := h.NewStart, h.NewStart+h.NewLines
			for _, s := range syms {
				se := s.EndLine
				if se <= 0 {
					se = s.Line
				}
				if s.Line <= end && se >= start {
					key := df.Path + ":" + s.Name + fmt.Sprint(s.Line)
					if !seen[key] {
						seen[key] = true
						out = append(out, ChangedSymbol{Name: s.Name, Kind: string(s.Kind), FilePath: df.Path, Line: s.Line})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func (e *Engine) routesForFiles(ctx context.Context, files []string) []api.Route {
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	routes, _ := e.store.ListRoutes(ctx, "")
	var out []api.Route
	for _, r := range routes {
		if set[r.FilePath] {
			out = append(out, r)
		}
	}
	return out
}

func (e *Engine) docsForFilesAndSymbols(ctx context.Context, files []string, syms []ChangedSymbol) []api.DocumentLink {
	seen := map[string]bool{}
	var out []api.DocumentLink
	queries := append([]string{}, files...)
	for _, s := range syms {
		queries = append(queries, s.Name)
	}
	for _, q := range queries {
		refs, err := e.DocsFor(ctx, q)
		if err != nil {
			continue
		}
		for _, l := range refs.Links {
			key := l.DocumentPath + fmt.Sprint(l.Line) + l.TargetType + l.TargetValue
			if !seen[key] {
				seen[key] = true
				out = append(out, l)
			}
		}
	}
	return out
}

func (e *Engine) recommendedTestsForFilesAndSymbols(ctx context.Context, files []string, syms []ChangedSymbol) []string {
	seen := map[string]bool{}
	var tests []string
	add := func(p string) {
		if p != "" && !seen[p] {
			if _, err := os.Stat(filepath.Join(e.root, p)); err == nil {
				seen[p] = true
				tests = append(tests, p)
			}
		}
	}
	for _, f := range files {
		ext := filepath.Ext(f)
		base := strings.TrimSuffix(f, ext)
		dir := filepath.Dir(f)
		name := strings.TrimSuffix(filepath.Base(f), ext)
		switch ext {
		case ".go":
			add(base + "_test.go")
		case ".py":
			add(filepath.Join(dir, "test_"+name+ext))
			add(base + "_test.py")
		case ".ts", ".tsx", ".js", ".jsx":
			add(base + ".test" + ext)
			add(base + ".spec" + ext)
		case ".java":
			add(base + "Test.java")
		case ".rs":
			add(strings.TrimSuffix(f, ".rs") + "_test.rs")
		}
	}
	return tests
}

func recommendedTestCommands(tests []string) []TestCommand {
	seen := map[string]*TestCommand{}
	order := []string{}
	add := func(command, file, reason string) {
		if command == "" {
			return
		}
		if existing, ok := seen[command]; ok {
			existing.Files = appendUnique(existing.Files, file)
			return
		}
		seen[command] = &TestCommand{Command: command, Files: []string{file}, Reason: reason}
		order = append(order, command)
	}
	for _, test := range tests {
		ext := filepath.Ext(test)
		dir := filepath.Dir(test)
		if dir == "." {
			dir = ""
		}
		switch ext {
		case ".go":
			pkg := "./" + filepath.ToSlash(dir)
			if dir == "" {
				pkg = "."
			}
			add("go test "+pkg, test, "Go package test")
		case ".py":
			add("pytest "+shellQuotePath(test), test, "pytest file test")
		case ".ts", ".tsx", ".js", ".jsx":
			add("npm test -- "+shellQuotePath(test), test, "JavaScript/TypeScript test file")
		case ".java":
			className := strings.TrimSuffix(filepath.Base(test), ".java")
			add("mvn test -Dtest="+className, test, "JUnit/Maven test class")
		case ".rs":
			add("cargo test", test, "Rust cargo tests")
		}
	}
	var out []TestCommand
	for _, command := range order {
		out = append(out, *seen[command])
	}
	return out
}

func shellQuotePath(path string) string {
	if path == "" || strings.ContainsAny(path, " \t\n'\"") {
		return strconv.Quote(path)
	}
	return path
}

func appendUnique(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func (e *Engine) reviewRisk(ctx context.Context, files []string, syms []ChangedSymbol, routes []api.Route, tests []string) RiskScore {
	score := 0
	reasons := []string{}
	if len(routes) > 0 {
		score += 30
		reasons = append(reasons, fmt.Sprintf("%d route handlers affected", len(routes)))
	}
	if len(tests) == 0 && len(files) > 0 {
		score += 20
		reasons = append(reasons, "no related tests found")
	}
	for _, s := range syms {
		if s.Kind != "function" && s.Kind != "method" && s.Kind != "type" && s.Kind != "class" && s.Kind != "interface" {
			continue
		}
		callers, _ := e.Callers(ctx, s.Name)
		if len(callers) > 5 {
			score += 15
			reasons = append(reasons, fmt.Sprintf("%s has %d callers", s.Name, len(callers)))
		}
	}
	if len(files) > 5 {
		score += 15
		reasons = append(reasons, fmt.Sprintf("large change set: %d files", len(files)))
	}
	level := "low"
	if score >= 50 {
		level = "high"
	} else if score >= 25 {
		level = "medium"
	}
	return RiskScore{Level: level, Score: score, Reasons: dedupStrings(reasons)}
}

func suggestedReviewOrder(files []string, routes []api.Route, tests []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, r := range routes {
		add(r.FilePath)
	}
	for _, f := range files {
		add(f)
	}
	for _, t := range tests {
		add(t)
	}
	return out
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
