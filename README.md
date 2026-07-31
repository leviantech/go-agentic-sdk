# Agentic SDK (Go)

AI agentic SDK for Go: **loop** (think → tool call → execute → repeat), **tool registry**, **multi-turn memory**, **multi-provider LLM**, and a **skill system** installable from local folders or git repos.

Structure follows production agent SDK patterns (pkg/ layout, Tool interface, functional options) — written from scratch, with the skill system as an extra advantage.

## Structure

```
agentic-sdk/
├── pkg/
│   ├── agent/            # agentic loop + functional options
│   ├── llm/
│   │   ├── types.go      # Message, ToolCall, Role
│   │   ├── llm.go        # LLM interface (other providers can implement it)
│   │   ├── openai/       # OpenAI provider + compatible endpoints (Ollama/vLLM)
│   │   └── mock/         # fake LLM for tests
│   ├── tools/
│   │   ├── registry.go   # Tool interface + Registry
│   │   └── builtin/      # built-in tools (get_current_time)
│   ├── memory/           # multi-turn conversation history
│   ├── skills/           # ⭐ skill system: SKILL.md loader + installer
│   ├── config/           # env config + YAML-based agent definitions
├── cmd/agent-cli/        # runner: single run / interactive chat
└── examples/
    ├── hello-skill/      # example skill (SKILL.md + scripts)
    └── agents.yaml       # example YAML agent definition
```

## Skill = folder + SKILL.md

```markdown
---
name: hello-skill
description: Demo skill.
version: 1.0.0
tools:
  - name: greet
    description: Greets someone.
    command: scripts/greet.sh
    parameters:
      type: object
      properties:
        name: { type: string }
---

# Instructions for the LLM
Use the `greet` tool when the user greets you.
```

- Markdown instructions → injected into the system prompt (`Manager.BuildSystemPrompt`).
- `command` → script (bash/python/etc.) run via subprocess; JSON arguments via stdin, stdout becomes the tool result.
- Skill tools are automatically registered in the registry as `tools.Tool` (via the `skillTool` adapter).
- Skills from the [skills.sh](https://www.skills.sh) ecosystem work out of the box — install by `owner/repo/skill` or a skills.sh URL. The SDK clones the GitHub repo and installs the matching SKILL.md folder.

## Running the example

```bash
export OPENAI_API_KEY=sk-...
cd agentic-sdk

# full test without an API key (mock LLM + skill scripts)
go test ./...

# single run with skills via YAML config
go run ./cmd/agent-cli -config examples/agents.yaml "Hello! Greet me and tell me what time it is now."

# run with skill directly + interactive chat
go run ./cmd/agent-cli -skill examples/hello-skill -chat

# install a skill from the skills.sh ecosystem
go run ./cmd/agent-cli -skillssh anthropics/skills/frontend-design "Design an elegant landing page."
# or use the full URL:
go run ./cmd/agent-cli -skillssh "https://www.skills.sh/anthropics/skills/frontend-design" "..."
# install every skill in a repo:
go run ./cmd/agent-cli -skillssh mattpocock/skills "..."
```

Other OpenAI-compatible endpoints:

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=llama3.1
```

## Integration into a service

```go
import (
    "agentic-sdk/pkg/agent"
    "agentic-sdk/pkg/llm/openai"
    "agentic-sdk/pkg/skills"
    "agentic-sdk/pkg/tools"
)

ctx := context.Background()

reg := tools.NewRegistry()
// tool from code:
_ = reg.Register(&tools.FuncTool{
    N: "cek_stok",
    D: "Check product stock.",
    S: map[string]any{"type": "object", "properties": map[string]any{}},
    F: func(ctx context.Context, args map[string]any) (string, error) { return `{"stok":5}`, nil },
})

// install skill + register its tools:
mgr := skills.NewManager(reg)
sk, _ := mgr.Install(ctx, "path/to/skill", "installed-skills")
_ = sk
// or from git:
// mgr.InstallFromGit(ctx, "https://github.com/org/repo/tree/main/skills/foo", "installed-skills")
// or from the skills.sh ecosystem (owner/repo/skill or skills.sh URL):
// mgr.InstallFromSkillsSH(ctx, "anthropics/skills/frontend-design", "installed-skills")

a, _ := agent.NewAgent(
    agent.WithLLM(openai.NewClient(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})),
    agent.WithTools(reg.List()...),
    agent.WithSystemPrompt(mgr.BuildSystemPrompt(agent.DefaultSystemPrompt())),
    agent.WithMaxIterations(10),
)

jawaban, err := a.Run(ctx, "how much stock of item X?")
```

## New LLM provider

Implement one method:

```go
type LLM interface {
    Chat(ctx context.Context, messages []llm.Message, tools []tools.Tool) (llm.Message, error)
}
```

Examples: `pkg/llm/openai` (HTTP), `pkg/llm/mock` (tests). Just add `pkg/llm/anthropic`, `pkg/llm/gemini`, etc.

## Security

**Treat skills as untrusted code.** Installing a skill copies its files and can
execute its `scripts/*` as subprocesses with the same privileges as your process.
Install only from sources you trust.

Built-in protections:

- **Skill name sanitized** — `name` must match `[A-Za-z0-9][A-Za-z0-9._-]*`; path
  traversal (`../../`) is rejected before anything is written.
- **Install destination containment** — the target directory is verified to stay
  inside the install root.
- **Command containment** — skill `command` fields must be relative paths that
  resolve inside the skill directory; absolute paths and `..` are rejected.
- **Symlinks are not followed** during copy (no local-file exfiltration via a
  crafted repo).
- **Size caps** — `SKILL.md` ≤ 1 MiB, total copy ≤ 50 MiB, script stdout ≤ 1 MiB.
- **Script timeout** — 30s default when the caller provides no context deadline.
- **skills.sh host allowlist** — only `skills.sh` and `github.com` URLs are accepted.

Inherent risks to know:

- **Arbitrary code execution** — a skill's scripts run on your machine. A malicious
  skill can read files, send them anywhere, or delete data.
- **Prompt injection** — skill instructions are injected into the system prompt.
  An untrusted skill can try to override agent behavior. Do not grant the agent
  privileged tools (file write, shell, network) when using untrusted skills.

## Roadmap

- Streaming (`RunStream`, SSE)
- Summary memory + vector store
- Guardrails (rate limit, content filtering)
- Observability (per-step loop tracing)
- Multi-agent / sub-agents
