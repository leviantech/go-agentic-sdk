package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillsSHRef(t *testing.T) {
	cases := []struct {
		in          string
		owner, repo string
		skill       string
		wantErr     bool
	}{
		{"anthropics/skills/frontend-design", "anthropics", "skills", "frontend-design", false},
		{"anthropics/skills", "anthropics", "skills", "", false},
		{"https://www.skills.sh/anthropics/skills/frontend-design", "anthropics", "skills", "frontend-design", false},
		{"https://skills.sh/mattpocock/skills/tdd", "mattpocock", "skills", "tdd", false},
		{"https://github.com/anthropics/skills/tree/main/frontend-design", "anthropics", "skills", "frontend-design", false},
		{"", "", "", "", true},
		{"onlyowner", "", "", "", true},
	}
	for _, c := range cases {
		owner, repo, skill, err := parseSkillsSHRef(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if owner != c.owner || repo != c.repo || skill != c.skill {
			t.Errorf("%q: got (%s, %s, %s), want (%s, %s, %s)",
				c.in, owner, repo, skill, c.owner, c.repo, c.skill)
		}
	}
}

func TestFindSkillDir(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("skills/foo", "---\nname: foo\n---\n")
	mk("skills/bar", "---\nname: bar\n---\n")
	// folder bernama sama tanpa SKILL.md harus diabaikan
	if err := os.MkdirAll(filepath.Join(root, "skills/bar/baz"), 0o755); err != nil {
		t.Fatal(err)
	}

	dir, err := findSkillDir(root, "bar")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if filepath.Base(dir) != "bar" {
		t.Fatalf("wrong dir: %s", dir)
	}

	if _, err := findSkillDir(root, "takada"); err == nil {
		t.Fatal("missing skill must error")
	}

	dirs := findAllSkillDirs(root)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 skill dirs, got %d", len(dirs))
	}
}

// TestInstallFromSkillsSHLive memverifikasi install nyata dari ekosistem
// skills.sh. Aktif hanya bila SKILLS_SH_LIVE_TEST=1 (butuh network).
func TestInstallFromSkillsSHLive(t *testing.T) {
	if os.Getenv("SKILLS_SH_LIVE_TEST") == "" {
		t.Skip("set SKILLS_SH_LIVE_TEST=1 untuk test install nyata")
	}
	dest := t.TempDir()
	skills, err := InstallFromSkillsSH(context.Background(),
		"https://www.skills.sh/anthropics/skills/frontend-design", dest)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("no skill installed")
	}
	sk := skills[0]
	if sk.Name == "" || sk.Instructions == "" {
		t.Fatalf("skill invalid: name=%q instructions=%q", sk.Name, sk.Instructions)
	}
}
