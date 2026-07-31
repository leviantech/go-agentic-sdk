// Command agent-cli is an example runner: loads config,
// installs skills, and runs the agent (single prompt or interactive chat).
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/leviantech/go-agentic-sdk/pkg/agent"
	"github.com/leviantech/go-agentic-sdk/pkg/config"
	"github.com/leviantech/go-agentic-sdk/pkg/llm/openai"
	"github.com/leviantech/go-agentic-sdk/pkg/memory"
	"github.com/leviantech/go-agentic-sdk/pkg/skills"
	"github.com/leviantech/go-agentic-sdk/pkg/tools"
	"github.com/leviantech/go-agentic-sdk/pkg/tools/builtin"
)

func main() {
	var (
		cfgPath    = flag.String("config", "", "path to agents.yaml file")
		skillPath  = flag.String("skill", "", "path to local skill folder to install")
		skillssh   = flag.String("skillssh", "", "skills.sh reference (owner/repo/skill or https://www.skills.sh/...)")
		installdir = flag.String("installdir", "installed-skills", "directory where skills are installed")
		chatMode   = flag.Bool("chat", false, "interactive chat mode")
	)
	flag.Parse()

	ctx := context.Background()

	// --- 1. LLM config (env) ---
	cfg := config.Get()
	if cfg.LLM.APIKey == "" {
		log.Fatal("set OPENAI_API_KEY (a compatible endpoint can be pointed to via OPENAI_BASE_URL)")
	}

	// --- 2. Tool registry + skill manager ---
	reg := tools.NewRegistry()
	if err := builtin.RegisterBuiltin(reg); err != nil {
		log.Fatalf("builtin tool: %v", err)
	}
	mgr := skills.NewManager(reg)

	// --- 3. Load YAML config (if any) ---
	var ac *config.AgentConfig
	var err error
	if *cfgPath != "" {
		ac, err = config.LoadAgentConfig(*cfgPath)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		for _, s := range ac.Skills {
			switch {
			case s.Source == "":
				if _, err = mgr.Install(ctx, s.Path, *installdir); err != nil {
					log.Fatalf("install skill %q: %v", s.Path, err)
				}
			case isSkillsSHURL(s.Source):
				if _, err = mgr.InstallFromSkillsSH(ctx, s.Source, *installdir); err != nil {
					log.Fatalf("install skillssh %q: %v", s.Source, err)
				}
			default:
				if _, err = mgr.InstallFromGit(ctx, s.Source, *installdir); err != nil {
					log.Fatalf("install skill %q: %v", s.Source, err)
				}
			}
		}
	} else {
		// Load skills from a previous install (if any)
		if _, err := mgr.LoadInstalled(*installdir); err != nil {
			log.Fatalf("load installed skills: %v", err)
		}
	}
	if *skillPath != "" {
		if _, err := mgr.Install(ctx, *skillPath, *installdir); err != nil {
			log.Fatalf("install skill: %v", err)
		}
	}
	if *skillssh != "" {
		if _, err := mgr.InstallFromSkillsSH(ctx, *skillssh, *installdir); err != nil {
			log.Fatalf("install skills.sh skill: %v", err)
		}
	}

	// --- 4. Build agent ---
	basePrompt := agent.DefaultSystemPrompt()
	if ac != nil {
		basePrompt = ac.SystemPrompt
	}
	systemPrompt := mgr.BuildSystemPrompt(basePrompt)

	a, err := agent.NewAgent(
		agent.WithName(agentName(ac)),
		agent.WithLLM(openai.NewClient(openai.Config{
			APIKey:  cfg.LLM.APIKey,
			BaseURL: cfg.LLM.BaseURL,
			Model:   cfg.LLM.Model,
		})),
		agent.WithMemory(memory.NewConversationBuffer()),
		agent.WithTools(reg.List()...),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithMaxIterations(maxIter(ac)),
	)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}

	// --- 5. Run ---
	if *chatMode {
		chat(ctx, a)
		return
	}
	input := strings.Join(flag.Args(), " ")
	if input == "" {
		log.Fatal("provide a prompt, e.g.: agent-cli run \"Hello! What time is it?\"")
	}
	out, err := a.Run(ctx, input)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	fmt.Println(out)
}

// isSkillsSHURL reports whether the ref points to a skills.sh page.
func isSkillsSHURL(ref string) bool {
	return strings.HasPrefix(ref, "https://www.skills.sh/") ||
		strings.HasPrefix(ref, "https://skills.sh/")
}

func agentName(ac *config.AgentConfig) string {
	if ac != nil {
		return ac.Name
	}
	return "default"
}

func maxIter(ac *config.AgentConfig) int {
	if ac != nil {
		return ac.MaxIterations
	}
	return 8
}

func chat(ctx context.Context, a *agent.Agent) {
	sc := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return
		}
		out, err := a.Run(ctx, line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else {
			fmt.Println(out)
		}
		fmt.Print("> ")
	}
}
