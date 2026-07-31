package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNamaSkillPathTraversal memverifikasi nama skill jahat ditolak.
func TestNamaSkillPathTraversal(t *testing.T) {
	for _, name := range []string{"../../etc", "..", "../evil", "a/b", `a\b`, "a b", ".hidden", "-flag"} {
		dir := t.TempDir()
		content := "---\nname: " + name + "\n---\n# x\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSkill(dir); err == nil {
			t.Errorf("nama %q harus ditolak", name)
		}
	}
}

// TestInstallDestContainment memastikan install tidak menulis ke luar destRoot.
func TestInstallDestContainment(t *testing.T) {
	dest := t.TempDir()
	// skill valid dengan nama aman
	sk, err := InstallSkill(context.Background(), "../../examples/hello-skill", dest)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	want := filepath.Join(dest, "hello-skill")
	if sk.Path != want {
		t.Fatalf("lokasi salah: %s", sk.Path)
	}
}

// TestCommandEscapeDitolak memverifikasi command di luar root skill ditolak.
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
			t.Errorf("command %q harus ditolak", cmd)
		}
	}
}

// TestSymlinkTidakDiikuti memastikan symlink dalam repo tidak ikut disalin.
func TestSymlinkTidakDiikuti(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// SKILL.md valid
	skmd := "---\nname: symlink-skill\n---\n# x\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(skmd), 0o644); err != nil {
		t.Fatal(err)
	}
	// file sensitif di luar src
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("RAHASIA"), 0o644); err != nil {
		t.Fatal(err)
	}
	// symlink di dalam repo menunjuk ke file rahasia
	if err := os.Symlink(secret, filepath.Join(src, "leak.txt")); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if _, err := InstallSkill(context.Background(), src, dest); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "symlink-skill", "leak.txt")); err == nil {
		t.Fatal("symlink harus dilewati, tapi ikut tersalin")
	}
}

// TestSkillFileSizeCap memastikan SKILL.md raksasa ditolak.
func TestSkillFileSizeCap(t *testing.T) {
	dir := t.TempDir()
	huge := "---\nname: big\n---\n" + strings.Repeat("x", MaxSkillFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSkill(dir); err == nil {
		t.Fatal("SKILL.md terlalu besar harus ditolak")
	}
}

// TestFrontmatterCRLF memastikan file Windows-style tetap ter-load.
func TestFrontmatterCRLF(t *testing.T) {
	dir := t.TempDir()
	content := "---\r\nname: crlf-skill\r\ndescription: tes\r\n---\r\n# Halo\r\n\r\ninstruksi\r\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sk, err := LoadSkill(dir)
	if err != nil {
		t.Fatalf("CRLF frontmatter harus bisa di-load: %v", err)
	}
	if sk.Name != "crlf-skill" {
		t.Fatalf("nama salah: %s", sk.Name)
	}
	if !strings.Contains(sk.Instructions, "instruksi") {
		t.Fatalf("body tidak termuat: %q", sk.Instructions)
	}
}

// TestHostSkillsSHDitolak memverifikasi host selain skills.sh/github.com ditolak.
func TestHostSkillsSHHostDitolak(t *testing.T) {
	if _, _, _, err := parseSkillsSHRef("https://evil.com/a/b"); err == nil {
		t.Fatal("host asing harus ditolak")
	}
	if _, _, _, err := parseSkillsSHRef("https://github.com/ok/repo"); err != nil {
		t.Fatalf("github.com harus diterima: %v", err)
	}
}
