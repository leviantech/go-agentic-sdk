package skills

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// InstallFromSkillsSH installs skills from the skills.sh ecosystem.
// skills.sh is an index; skill content lives in GitHub repos as
// SKILL.md folders. Supported ref formats:
//
//	owner/repo                          → install every skill found in the repo
//	owner/repo/skill-name               → install one skill
//	https://www.skills.sh/owner/repo/skill-name
//	https://github.com/owner/repo       → install every skill found
//	https://github.com/owner/repo/tree/<branch>/<path> → install the skill at that path
func InstallFromSkillsSH(ctx context.Context, ref, destRoot string) ([]*Skill, error) {
	owner, repo, skill, err := parseSkillsSHRef(ref)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "skillssh-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	repoURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	if err := runGit(ctx, "clone", "--depth", "1", "--filter=blob:none", repoURL, tmp); err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}

	var dirs []string
	if skill != "" {
		dir, err := findSkillDir(tmp, skill)
		if err != nil {
			return nil, err
		}
		dirs = []string{dir}
	} else {
		dirs = findAllSkillDirs(tmp)
		if len(dirs) == 0 {
			return nil, fmt.Errorf("no skill (SKILL.md) found in %s/%s", owner, repo)
		}
	}

	installed := make([]*Skill, 0, len(dirs))
	for _, d := range dirs {
		sk, err := InstallSkill(ctx, d, destRoot)
		if err != nil {
			return nil, err
		}
		installed = append(installed, sk)
	}
	return installed, nil
}

// allowedSkillHosts restricts which hosts may be used as skills.sh
// references (prevents cloning from arbitrary hosts).
var allowedSkillHosts = map[string]bool{
	"www.skills.sh": true,
	"skills.sh":     true,
	"github.com":    true,
}

// parseSkillsSHRef extracts owner, repo, and optional skill name
// from a skills.sh / GitHub reference.
func parseSkillsSHRef(ref string) (owner, repo, skill string, err error) {
	s := strings.TrimSpace(ref)
	if s == "" {
		return "", "", "", fmt.Errorf("empty skills.sh reference")
	}
	if strings.HasPrefix(s, "http") {
		u, err := url.Parse(s)
		if err != nil {
			return "", "", "", fmt.Errorf("invalid URL %q: %w", ref, err)
		}
		if !allowedSkillHosts[strings.ToLower(u.Hostname())] {
			return "", "", "", fmt.Errorf("host %q is not allowed; use skills.sh or github.com", u.Hostname())
		}
		s = strings.Trim(u.Path, "/")
	}
	parts := make([]string, 0, 4)
	for _, p := range strings.Split(s, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("reference %q must be owner/repo[/skill]", ref)
	}
	owner, repo = parts[0], parts[1]
	// GitHub tree URLs: owner/repo/tree/<branch>/<path...> — use the last path segment.
	if len(parts) > 2 && parts[2] == "tree" {
		skill = parts[len(parts)-1]
	} else if len(parts) > 2 {
		skill = parts[len(parts)-1]
	}
	return owner, repo, skill, nil
}

// findSkillDir finds the first directory named name containing SKILL.md.
func findSkillDir(root, name string) (string, error) {
	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() || d.Name() != name {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("skill %q not found in repository", name)
	}
	return found, nil
}

// findAllSkillDirs collects every directory containing SKILL.md.
func findAllSkillDirs(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
				out = append(out, path)
			}
		}
		return nil
	})
	return out
}
