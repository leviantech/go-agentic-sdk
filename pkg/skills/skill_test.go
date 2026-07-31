package skills

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLoadSkill(t *testing.T) {
	sk, err := LoadSkill("../../examples/hello-skill")
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if sk.Name != "hello-skill" {
		t.Fatalf("name wrong: %s", sk.Name)
	}
	if len(sk.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(sk.Tools))
	}
	if !strings.Contains(sk.Instructions, "greet") {
		t.Fatalf("instructions not loaded: %s", sk.Instructions)
	}
}

func TestRunScriptTool(t *testing.T) {
	sk, err := LoadSkill("../../examples/hello-skill")
	if err != nil {
		t.Fatal(err)
	}
	out, err := sk.RunScript(context.Background(), "scripts/greet.sh",
		map[string]any{"name": "Budi"})
	if err != nil {
		t.Fatalf("run script: %v", err)
	}
	if !strings.Contains(out, "Budi") {
		t.Fatalf("output does not contain the name: %s", out)
	}
}

func TestInstallSkill(t *testing.T) {
	dest := t.TempDir()
	sk, err := InstallSkill(context.Background(), "../../examples/hello-skill", dest)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if sk.Path != dest+"/hello-skill" {
		t.Fatalf("wrong location: %s", sk.Path)
	}
	// file was copied along
	if !exists(sk.Path + "/scripts/greet.sh") {
		t.Fatal("script was not copied")
	}
}

func TestManagerInstallRegister(t *testing.T) {
	reg := newTestRegistry()
	mgr := NewManager(reg)
	if _, err := mgr.Install(context.Background(), "../../examples/hello-skill", t.TempDir()); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(mgr.Skills()) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(mgr.Skills()))
	}
	// skill tool is registered & works
	tt, ok := reg.Get("greet")
	if !ok {
		t.Fatal("tool greet is not registered")
	}
	res, err := tt.Execute(context.Background(), map[string]any{"name": "Sari"})
	if err != nil || !strings.Contains(res, "Sari") {
		t.Fatalf("skill tool failed: %v / %s", err, res)
	}
	p := mgr.BuildSystemPrompt("base")
	if !strings.Contains(p, "## Skill: hello-skill") {
		t.Fatalf("system prompt does not contain the skill: %s", p)
	}
}

func TestLoadInstalledFromDir(t *testing.T) {
	dest := t.TempDir()
	reg := newTestRegistry()
	mgr := NewManager(reg)
	if _, err := mgr.Install(context.Background(), "../../examples/hello-skill", dest); err != nil {
		t.Fatal(err)
	}

	// new session: load from directory without reinstalling
	mgr2 := NewManager(newTestRegistry())
	n, err := mgr2.LoadInstalled(dest)
	if err != nil {
		t.Fatalf("load installed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 skill loaded, got %d", n)
	}
}

func TestSkillWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/SKILL.md", "# no frontmatter"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSkill(dir); err == nil {
		t.Fatal("must error for SKILL.md without frontmatter")
	}
}

// TestRunScriptCapsStderr: a script flooding stderr must not grow an
// unbounded buffer (memory exhaustion via stderr alone).
func TestRunScriptCapsStderr(t *testing.T) {
	root := t.TempDir()
	// ~2 MiB of stderr — well above MaxScriptOutputBytes (1 MiB)
	script := "#!/bin/sh\ni=0\nwhile [ $i -lt 2000 ]; do echo 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' >&2; i=$((i+1)); done\nexit 0\n"
	if err := writeFile(root+"/SKILL.md", `---
name: noisy
version: 1.0.0
tools:
  - name: noisy
    description: noisy
    command: scripts/noise.sh
---`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/scripts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(root+"/scripts/noise.sh", script); err != nil {
		t.Fatal(err)
	}
	sk, err := LoadSkill(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sk.RunScript(context.Background(), "scripts/noise.sh", nil); err == nil {
		t.Fatal("stderr overflow must surface as an error")
	}
}

// even if the link lives inside the skill root (its target may point
// outside, bypassing containment).
func TestRunScriptRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := writeFile(outside+"/evil.sh", "#!/bin/sh\necho pwned"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(root+"/SKILL.md", `---
name: evil
version: 1.0.0
tools:
  - name: evil
    description: evil
    command: scripts/evil.sh
---`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/scripts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside+"/evil.sh", root+"/scripts/evil.sh"); err != nil {
		t.Fatal(err)
	}
	sk, err := LoadSkill(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sk.RunScript(context.Background(), "scripts/evil.sh", nil); err == nil {
		t.Fatal("symlink script must be rejected")
	}
}
