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

// Limits melindungi host dari skill yang tidak wajar.
const (
	// MaxSkillFileBytes membatasi ukuran SKILL.md (1 MiB).
	MaxSkillFileBytes = 1 << 20
	// MaxScriptOutputBytes membatasi output script per pemanggilan tool (1 MiB).
	MaxScriptOutputBytes = 1 << 20
	// ScriptTimeout adalah timeout default tiap eksekusi tool (30 dtk)
	// bila context pemanggil tidak punya deadline.
	ScriptTimeout = 30 * time.Second
)

// skillNameRe membatasi nama skill: aman untuk path + nama registry.
var skillNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrUnsafeSkillName menandakan nama skill mengandung path traversal.
var ErrUnsafeSkillName = errors.New("skill name is not safe for a filesystem path")

// ToolSpec defines a tool provided by a skill.
type ToolSpec struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Command     string         `yaml:"command"`     // script path relative to the skill root
	Parameters  map[string]any `yaml:"parameters"`  // JSON Schema
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
	// Keamanan: nama skill dipakai sebagai nama direktori tujuan install.
	// Tolak path traversal (../../), separator, dan karakter aneh.
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
// Keamanan: command wajib merujuk ke file di dalam root skill
// (path absolut dan ".." ditolak), sehingga skill tidak bisa
// menjalankan executable di luar direktori sendiri.
func (s *Skill) RunScript(ctx context.Context, command string, args map[string]any) (string, error) {
	abs, err := s.resolveScript(command)
	if err != nil {
		return "", err
	}

	argJSON, err := json.Marshal(args)
	if err != nil {
		return "", err
	}

	// Timeout default bila pemanggil tidak memberi deadline.
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

// resolveScript mengonversi nilai field command ke path absolut yang
// dijamin berada di dalam root skill. Path absolut dan traversal ".."
// dari sisi user (frontmatter skill) ditolak.
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
	// Containment: pastikan hasil resolve masih di dalam root skill.
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("command %q escapes the skill directory", command)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("command %q is a directory, not an executable", command)
	}
	return abs, nil
}

// limitedWriter membatasi jumlah byte yang ditulis (cap output tool).
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
