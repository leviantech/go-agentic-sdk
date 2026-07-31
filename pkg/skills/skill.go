// Package skills handles the skill lifecycle: format validation,
// installation, loading instructions, and registering skill tools in the registry.
//
// Skill format (following the Anthropic Agent Skills / Claude Skills convention):
//
//	<skill dir>/
//	├── SKILL.md   # YAML frontmatter + markdown instructions (required)
//	├── scripts/   # scripts executed as tools (optional)
//	└── assets/    # supporting files (optional)
package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Limits protect the host from misbehaving skills.
const (
	// MaxSkillFileBytes caps the SKILL.md size (1 MiB).
	MaxSkillFileBytes = 1 << 20
	// MaxScriptOutputBytes caps the script output per tool invocation (1 MiB).
	MaxScriptOutputBytes = 1 << 20
	// ScriptTimeout is the default per-tool execution timeout (30s) used
	// when the calling context has no deadline.
	ScriptTimeout = 30 * time.Second
)

// skillNameRe constrains skill names: safe for both paths and registry names.
var skillNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrUnsafeSkillName signals a skill name with path traversal.
var ErrUnsafeSkillName = errors.New("skill name is not safe for a filesystem path")

// ToolSpec defines a tool provided by a skill.
type ToolSpec struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Command     string         `yaml:"command"`    // script path relative to the skill root
	Parameters  map[string]any `yaml:"parameters"` // JSON Schema
}

// frontmatter is the skill metadata from the ---...--- block of SKILL.md.
type frontmatter struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Version     string     `yaml:"version"`
	Tools       []ToolSpec `yaml:"tools"`
}

// Skill is an installed skill: instructions for the LLM + tool definitions.
type Skill struct {
	Name         string
	Description  string
	Version      string
	Path         string
	Instructions string
	Tools        []ToolSpec
}

// LoadSkill loads a skill from a directory containing SKILL.md.
func LoadSkill(path string) (*Skill, error) {
	fm, body, err := parseSkillFile(path)
	if err != nil {
		return nil, err
	}
	return &Skill{
		Name:         fm.Name,
		Description:  fm.Description,
		Version:      fm.Version,
		Path:         path,
		Instructions: strings.TrimSpace(body),
		Tools:        fm.Tools,
	}, nil
}

func parseSkillFile(path string) (frontmatter, string, error) {
	var fm frontmatter
	raw, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return fm, "", fmt.Errorf("SKILL.md not found in %s: %w", path, err)
	}
	if len(raw) > MaxSkillFileBytes {
		return fm, "", fmt.Errorf("SKILL.md in %s exceeds %d bytes", path, MaxSkillFileBytes)
	}
	head, body := splitFrontmatter(raw)
	if head == "" {
		return fm, "", fmt.Errorf("SKILL.md in %s has no YAML frontmatter", path)
	}
	if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
		return fm, "", fmt.Errorf("invalid frontmatter in %s: %w", path, err)
	}
	if fm.Name == "" {
		return fm, "", fmt.Errorf("frontmatter in %s must have a name field", path)
	}
	// Security: the skill name becomes the install destination folder name.
	// Reject path traversal (../../), separators, and other odd characters.
	if !skillNameRe.MatchString(fm.Name) {
		return fm, "", fmt.Errorf("%w: %q", ErrUnsafeSkillName, fm.Name)
	}
	return fm, body, nil
}

// splitFrontmatter splits the ---...--- block (frontmatter) from the markdown body.
func splitFrontmatter(raw []byte) (string, string) {
	s := strings.TrimPrefix(string(raw), "\ufeff")
	s = strings.TrimLeft(s, "\r\n")
	if !strings.HasPrefix(s, "---") {
		return "", s
	}
	rest := strings.TrimPrefix(s[3:], "\r\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", s
	}
	head := strings.TrimSuffix(rest[:idx], "\r")
	return head, strings.TrimPrefix(rest[idx+4:], "\r\n")
}

// RunScript runs a skill script as a subprocess.
// JSON arguments are sent via stdin; stdout becomes the tool result;
// stderr is attached to the error message.
//
// Security: the command must reference a file inside the skill root
// (absolute paths and ".." are rejected), so a skill cannot execute an
// executable outside its own directory.
func (s *Skill) RunScript(ctx context.Context, command string, args map[string]any) (string, error) {
	abs, err := s.resolveScript(command)
	if err != nil {
		return "", err
	}

	argJSON, err := json.Marshal(args)
	if err != nil {
		return "", err
	}

	// Default timeout when the caller provides no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ScriptTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, abs)
	cmd.Dir = s.Path
	cmd.Stdin = bytes.NewReader(argJSON)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, max: MaxScriptOutputBytes}
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("script %s failed: %w (stderr: %s)",
			command, err, strings.TrimSpace(errBuf.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// resolveScript converts a command field value into an absolute path
// guaranteed to live inside the skill root. Absolute paths and ".."
// traversal supplied by the skill frontmatter are rejected.
func (s *Skill) resolveScript(command string) (string, error) {
	clean := filepath.Clean(command)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("command %q must be a relative path inside the skill directory", command)
	}
	abs := filepath.Join(s.Path, clean)
	rootAbs, err := filepath.Abs(s.Path)
	if err != nil {
		return "", err
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	// Containment: ensure the resolved path stays inside the skill root.
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("command %q escapes the skill directory", command)
	}
	// Reject symlinks: a script that is a link could point outside the
	// skill root, defeating the containment check above. This mirrors the
	// no-symlink rule used during install (copyDir).
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("command %q must not be a symlink", command)
	}
	if info.IsDir() {
		return "", fmt.Errorf("command %q is a directory, not an executable", command)
	}
	return abs, nil
}

// limitedWriter caps the number of bytes written (tool output limit).
type limitedWriter struct {
	w   *bytes.Buffer
	max int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	room := lw.max - lw.w.Len()
	if room <= 0 {
		return len(p), fmt.Errorf("tool output exceeds %d bytes", lw.max)
	}
	if len(p) > room {
		lw.w.Write(p[:room])
		return len(p), fmt.Errorf("tool output exceeds %d bytes", lw.max)
	}
	return lw.w.Write(p)
}
