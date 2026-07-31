package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// AgentConfig is a YAML-based agent definition (like agents.yaml).
type AgentConfig struct {
	Name          string      `yaml:"name"`
	Description   string      `yaml:"description"`
	SystemPrompt  string      `yaml:"system_prompt"`
	MaxIterations int         `yaml:"max_iterations"`
	Skills        []SkillSpec `yaml:"skills"`
	MCP           []MCPSpec   `yaml:"mcp"`
}

// SkillSpec declares a skill to install at agent boot.
type SkillSpec struct {
	Path   string `yaml:"path"`   // local skill folder (or clone result)
	Source string `yaml:"source"` // git URL (alternative to path; e.g. .../tree/main/skills/foo)
}

// MCPSpec declares an MCP server (stdio) to launch at agent boot.
type MCPSpec struct {
	Name    string            `yaml:"name"`    // registry prefix for its tools
	Command string            `yaml:"command"` // executable (e.g. npx)
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

// LoadAgentConfig reads an agent definition from a YAML file.
// Supports environment variable expansion with ${VAR} syntax.
func LoadAgentConfig(path string) (*AgentConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = expandEnv(raw)

	var cfg AgentConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid YAML in %s: %w", path, err)
	}
	if cfg.Name == "" {
		cfg.Name = "agent"
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 8
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are a helpful agent. Use the available tools when needed."
	}
	return &cfg, nil
}

var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${VAR} with the environment value.
func expandEnv(raw []byte) []byte {
	return envVarRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(m[2 : len(m)-1])
		if v, ok := os.LookupEnv(name); ok {
			return []byte(v)
		}
		return m // leave as-is when the variable does not exist
	})
}
