package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNamaSkillPathTraversal verifies that malicious skill names are rejected.
func TestNamaSkillPathTraversal(t *testing.T) {
	for _, name := range []string{"../../etc", "..", "../evil", "a/b", `a\b`, "a b", ".hidden", "-flag"} {
		dir := t.TempDir()
		content := "---\nname: " + name + "\n---\n# x\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSkill(dir); err == nil {
			t.Errorf("name %q must be rejected", name)
		}
	}
}

// TestInstallDestContainment ensures install never writes outside destRoot.
func TestInstallDestContainment(t *testing.T) {
	dest := t.TempDir()
	// a valid skill with a safe name
	sk, err := InstallSkill(context.Background(), "../../examples/hello-skill", dest)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	want := filepath.Join(dest, "hello-skill")
	if sk.Path != want {
		t.Fatalf("wrong location: %s", sk.Path)
	}
}

// TestCommandEscapeDitolak verifies that commands outside the skill root are rejected.
func TestCommandEscapeDitolak(t *testing.T) {
	sk, err := LoadSkill("../../examples/hello-skill")
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		"/bin/echo",
		"../../bin/sh",
		"../escape.sh",
		"..",
		"../../../../etc/passwd",
	} {
		if _, err := sk.RunScript(context.Background(), cmd, map[string]any{}); err == nil {
			t.Errorf("command %q must be rejected", cmd)
		}
	}
}

// TestSymlinkTidakDiikuti ensures symlinks inside a repo are not copied.
func TestSymlinkTidakDiikuti(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a valid SKILL.md
	skmd := "---\nname: symlink-skill\n---\n# x\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(skmd), 0o644); err != nil {
		t.Fatal(err)
	}
	// a sensitive file outside src
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a symlink inside the repo pointing to the secret file
	if err := os.Symlink(secret, filepath.Join(src, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if _, err := InstallSkill(context.Background(), src, dest); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "symlink-skill", "leak.txt")); err == nil {
		t.Fatal("symlink must be skipped, but it was copied")
	}
}

// TestSkillFileSizeCap ensures an oversized SKILL.md is rejected.
func TestSkillFileSizeCap(t *testing.T) {
	dir := t.TempDir()
	huge := "---\nname: big\n---\n" + strings.Repeat("x", MaxSkillFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSkill(dir); err == nil {
		t.Fatal("oversized SKILL.md must be rejected")
	}
}

// TestFrontmatterCRLF ensures Windows-style files still load.
func TestFrontmatterCRLF(t *testing.T) {
	dir := t.TempDir()
	content := "---\r\nname: crlf-skill\r\ndescription: tes\r\n---\r\n# Halo\r\n\r\ninstruksi\r\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sk, err := LoadSkill(dir)
	if err != nil {
		t.Fatalf("CRLF frontmatter must load: %v", err)
	}
	if sk.Name != "crlf-skill" {
		t.Fatalf("wrong name: %s", sk.Name)
	}
	if !strings.Contains(sk.Instructions, "instruksi") {
		t.Fatalf("body not loaded: %q", sk.Instructions)
	}
}

// TestHostSkillsSHHostDitolak verifies hosts other than skills.sh/github.com are rejected.
func TestHostSkillsSHHostDitolak(t *testing.T) {
	if _, _, _, err := parseSkillsSHRef("https://evil.com/a/b"); err == nil {
		t.Fatal("foreign host must be rejected")
	}
	if _, _, _, err := parseSkillsSHRef("https://github.com/ok/repo"); err != nil {
		t.Fatalf("github.com must be accepted: %v", err)
	}
}
