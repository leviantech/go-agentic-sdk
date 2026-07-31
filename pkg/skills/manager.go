package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

// Manager manages installed skills: installing, loading from a directory,
// registering their tools in the registry, and composing the system prompt.
type Manager struct {
	reg    *tools.Registry
	skills []*Skill
}

func NewManager(reg *tools.Registry) *Manager {
	return &Manager{reg: reg}
}

// Install validates, copies, then registers the skill's tools in the registry.
func (m *Manager) Install(ctx context.Context, src, destRoot string) (*Skill, error) {
	sk, err := InstallSkill(ctx, src, destRoot)
	if err != nil {
		return nil, err
	}
	if err := m.register(sk); err != nil {
		return nil, err
	}
	return sk, nil
}

// InstallFromGit clones a skill repo then installs it.
func (m *Manager) InstallFromGit(ctx context.Context, url, destRoot string) (*Skill, error) {
	sk, err := InstallFromGit(ctx, url, destRoot)
	if err != nil {
		return nil, err
	}
	if err := m.register(sk); err != nil {
		return nil, err
	}
	return sk, nil
}

// InstallFromSkillsSH installs skills from the skills.sh ecosystem
// (ref format: owner/repo[/skill] or a skills.sh / GitHub URL).
// Returns all skills that were installed and registered.
func (m *Manager) InstallFromSkillsSH(ctx context.Context, ref, destRoot string) ([]*Skill, error) {
	skills, err := InstallFromSkillsSH(ctx, ref, destRoot)
	if err != nil {
		return nil, err
	}
	for _, sk := range skills {
		if err := m.register(sk); err != nil {
			return nil, err
		}
	}
	return skills, nil
}

// LoadInstalled loads all skills already present in destRoot
// (e.g. from a previous session) without copying again.
func (m *Manager) LoadInstalled(destRoot string) (int, error) {
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sk, err := LoadSkill(filepath.Join(destRoot, e.Name()))
		if err != nil {
			continue // not a skill, skip
		}
		if err := m.register(sk); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (m *Manager) register(sk *Skill) error {
	for _, spec := range sk.Tools {
		if err := m.reg.Register(&skillTool{skill: sk, spec: spec}); err != nil {
			return fmt.Errorf("skill %s: %w", sk.Name, err)
		}
	}
	m.skills = append(m.skills, sk)
	return nil
}

// Skills returns all loaded skills.
func (m *Manager) Skills() []*Skill {
	out := make([]*Skill, len(m.skills))
	copy(out, m.skills)
	return out
}

// BuildSystemPrompt combines the base prompt with all skills' instructions.
// Skill instructions are appended as a separate section at the end.
func (m *Manager) BuildSystemPrompt(base string) string {
	if len(m.skills) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	for _, sk := range m.skills {
		fmt.Fprintf(&b, "\n\n## Skill: %s", sk.Name)
		if sk.Version != "" {
			fmt.Fprintf(&b, " (v%s)", sk.Version)
		}
		b.WriteString("\n")
		b.WriteString(sk.Instructions)
	}
	return b.String()
}

// skillTool adapts a skill ToolSpec into a tools.Tool.
type skillTool struct {
	skill *Skill
	spec  ToolSpec
}

func (t *skillTool) Name() string        { return t.spec.Name }
func (t *skillTool) Description() string { return t.spec.Description }
func (t *skillTool) Schema() map[string]any {
	if t.spec.Parameters != nil {
		return t.spec.Parameters
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *skillTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.skill.RunScript(ctx, t.spec.Command, args)
}

var _ tools.Tool = (*skillTool)(nil)
