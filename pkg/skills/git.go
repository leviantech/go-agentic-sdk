package skills

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// gitCmd builds a git command; the executable can be overridden via env
// SKILLS_GIT_BIN (useful when git is not on PATH).
func gitCmd(ctx context.Context, args ...string) *exec.Cmd {
	bin := "git"
	if b := os.Getenv("SKILLS_GIT_BIN"); b != "" {
		bin = b
	}
	return exec.CommandContext(ctx, bin, args...)
}

// baseRepoURL strips the /tree/... part from a GitHub/GitLab URL
// so it points at the repo root for cloning.
func baseRepoURL(url string) string {
	if i := strings.Index(url, "/tree/"); i >= 0 {
		return url[:i]
	}
	return url
}

// subdirFromURL extracts the skill folder path from /tree/<ref>/<path>.
// Example: .../repo/tree/main/skills/foo → skills/foo
func subdirFromURL(url string) string {
	const marker = "/tree/"
	i := strings.Index(url, marker)
	if i < 0 {
		return ""
	}
	rest := url[i+len(marker):]
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
