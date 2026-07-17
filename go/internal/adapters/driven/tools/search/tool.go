// Package search implements ports.Tool as a workspace-scoped content
// search: a pure-Go grep, requiring no external binary (the Rust
// reference's xai-grok-tools-api describes a search tool without pinning
// it to any particular backend; ripgrep-via-shellexec is the other option
// noted in ROADMAP.md's Phase 3 — this is the no-external-binary path).
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Name is the tool identifier advertised to the model.
const Name = "search"

// maxResults caps how many matching lines are returned, so a broad
// pattern over a large workspace can't blow up the response size.
const maxResults = 200

// Tool searches file contents rooted at a fixed workspace directory,
// refusing to search outside it via ".." or an absolute path — the same
// guard as readfile.Tool and writefile.Tool.
type Tool struct {
	root string
}

// New builds a search.Tool scoped to root.
func New(root string) *Tool {
	return &Tool{root: root}
}

type args struct {
	// Pattern is a Go regular expression (RE2 syntax) matched against
	// each line.
	Pattern string `json:"pattern"`
	// Glob optionally restricts which filenames are searched (e.g.
	// "*.go"). Matched against the base filename only. Empty means "every
	// file".
	Glob string `json:"glob"`
	// Path optionally restricts the search to a subdirectory of the
	// workspace root, relative to it. Empty means "the whole workspace".
	Path string `json:"path"`
}

// Name implements ports.Tool.
func (t *Tool) Name() string { return Name }

// Description implements ports.Tool.
func (t *Tool) Description() string {
	return "Searches file contents in the workspace for lines matching a regular expression, optionally restricted to a glob and/or subdirectory."
}

// JSONSchema implements ports.Tool.
func (t *Tool) JSONSchema() string {
	return `{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "A regular expression (RE2 syntax) to match against each line."},
    "glob": {"type": "string", "description": "Optional filename glob (e.g. \"*.go\") restricting which files are searched."},
    "path": {"type": "string", "description": "Optional subdirectory to search, relative to the workspace root."}
  },
  "required": ["pattern"]
}`
}

// Execute implements ports.Tool.
func (t *Tool) Execute(_ context.Context, argumentsJSON string) (string, error) {
	var a args
	if err := json.Unmarshal([]byte(argumentsJSON), &a); err != nil {
		return "", fmt.Errorf("search: parse arguments: %w", err)
	}
	if a.Pattern == "" {
		return "", fmt.Errorf("search: pattern is required")
	}

	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("search: invalid pattern: %w", err)
	}

	searchRoot, err := t.resolve(a.Path)
	if err != nil {
		return "", err
	}

	var results []string
	err = filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(results) >= maxResults {
			return nil
		}
		if a.Glob != "" {
			matched, err := filepath.Match(a.Glob, d.Name())
			if err != nil {
				return fmt.Errorf("search: invalid glob: %w", err)
			}
			if !matched {
				return nil
			}
		}

		rel, err := filepath.Rel(t.root, path)
		if err != nil {
			return err
		}
		matches, err := grepFile(path, re, maxResults-len(results))
		if err != nil {
			// A file that can't be read as text (binary, permission
			// error, removed mid-walk) is skipped, not fatal to the
			// whole search.
			return nil
		}
		for _, m := range matches {
			results = append(results, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), m.line, m.text))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	return strings.Join(results, "\n"), nil
}

type lineMatch struct {
	line int
	text string
}

func grepFile(path string, re *regexp.Regexp, limit int) ([]lineMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []lineMatch
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() && len(matches) < limit {
		lineNum++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, lineMatch{line: lineNum, text: text})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

// resolve joins path onto the workspace root and rejects any result that
// escapes it — same guard as readfile.Tool/writefile.Tool, plus rejecting
// an absolute path outright (see writefile.Tool.resolve's comment for why).
func (t *Tool) resolve(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("search: path %q must be relative to the workspace root, not absolute", path)
	}

	root, err := filepath.Abs(t.root)
	if err != nil {
		return "", fmt.Errorf("search: resolve workspace root: %w", err)
	}
	joined := filepath.Join(root, path)
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("search: path %q escapes the workspace root", path)
	}
	return joined, nil
}
