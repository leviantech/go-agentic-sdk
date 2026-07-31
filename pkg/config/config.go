package config

import "os"

// Config is the SDK runtime configuration from environment variables.
type Config struct {
	LLM struct {
		APIKey  string
		BaseURL string // empty = official OpenAI
		Model   string
	}
	SkillsDir string // directory storing installed skills
}

// Get reads configuration from the environment.
// OPENAI_API_KEY, OPENAI_BASE_URL, OPENAI_MODEL, SKILLS_DIR.
func Get() Config {
	var c Config
	c.LLM.APIKey = os.Getenv("OPENAI_API_KEY")
	c.LLM.BaseURL = os.Getenv("OPENAI_BASE_URL")
	c.LLM.Model = os.Getenv("OPENAI_MODEL")
	c.SkillsDir = firstNonEmpty(os.Getenv("SKILLS_DIR"), "installed-skills")
	return c
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
