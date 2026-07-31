package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	yaml := `
name: riset
system_prompt: "Research assistant ${MODEL_NOTE}"
max_iterations: 5
skills:
  - path: examples/hello-skill
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MODEL_NOTE", "fast")

	ac, err := LoadAgentConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ac.Name != "riset" {
		t.Fatalf("name wrong: %s", ac.Name)
	}
	if ac.MaxIterations != 5 {
		t.Fatalf("max_iterations wrong: %d", ac.MaxIterations)
	}
	if ac.SystemPrompt != "Research assistant fast" {
		t.Fatalf("env expansion failed: %s", ac.SystemPrompt)
	}
	if len(ac.Skills) != 1 || ac.Skills[0].Path != "examples/hello-skill" {
		t.Fatalf("skills wrong: %+v", ac.Skills)
	}
}

func TestLoadAgentConfigDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(path, []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ac, err := LoadAgentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if ac.MaxIterations != 8 {
		t.Fatalf("default max_iterations must be 8, got %d", ac.MaxIterations)
	}
	if ac.SystemPrompt == "" {
		t.Fatal("default system_prompt must not be empty")
	}
}

func TestExpandEnvTanpaVariabel(t *testing.T) {
	out := expandEnv([]byte("a=${TAK_ADA}b"))
	if string(out) != "a=${TAK_ADA}b" {
		t.Fatalf("missing variable must be left as-is: %s", out)
	}
}
