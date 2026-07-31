package skills

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// InstallSkill validates the skill at src (local folder or git clone result),
// then copies it to destRoot/<skill name>.
// Returns the installed, loaded skill.
func InstallSkill(ctx context.Context, src, destRoot string) (*Skill, error) {
	fm, _, err := parseSkillFile(src)
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(destRoot, fm.Name)
	// Keamanan: pastikan tujuan tetap di dalam destRoot.
	rootAbs, err := filepath.Abs(destRoot)
	if err != nil {
		return nil, err
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if destAbs != rootAbs && !strings.HasPrefix(destAbs, rootAbs+string(filepath.Separator)) {
		return nil, fmt.Errorf("skill destination escapes install directory")
	}
	if err := copyDir(ctx, src, dest); err != nil {
		return nil, fmt.Errorf("failed to copy skill: %w", err)
	}
	return LoadSkill(dest)
}

// InstallFromGit clones a skill repo and installs it.
// The URL can point directly at a skill folder inside the repo:
//
//	https://github.com/org/repo            → use the entire repo
//	https://github.com/org/repo/tree/main/skills/foo → specific folder (sparse checkout)
//
// Note: for a specific folder inside a repo, use sparse checkout.
func InstallFromGit(ctx context.Context, url, destRoot string) (*Skill, error) {
	tmp, err := os.MkdirTemp("", "skill-git-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	gitArgs := []string{"clone", "--depth", "1"}
	if sub := subdirFromURL(url); sub != "" {
		gitArgs = append(gitArgs, "--filter=blob:none", "--sparse")
	}
	gitArgs = append(gitArgs, baseRepoURL(url), tmp)

	if err := runGit(ctx, gitArgs...); err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}
	if sub := subdirFromURL(url); sub != "" {
		if err := runGit(ctx, "-C", tmp, "sparse-checkout", "set", sub); err != nil {
			return nil, fmt.Errorf("sparse-checkout failed: %w", err)
		}
	}

	src := tmp
	if sub := subdirFromURL(url); sub != "" {
		src = filepath.Join(tmp, sub)
	}
	return InstallSkill(ctx, src, destRoot)
}

func runGit(ctx context.Context, args ...string) error {
	cmd := gitCmd(ctx, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// maxCopyBytes membatasi total ukuran skill yang disalin (50 MiB).
const maxCopyBytes = 50 << 20

// copyDir menyalin isi src ke dst dengan perlindungan keamanan:
//   - symlink TIDAK diikuti (mencegah exfiltrasi file lokal via repo)
//   - direktori .git dilewati
//   - total byte dibatasi (maxCopyBytes)
func copyDir(ctx context.Context, src, dst string) error {
	var copied int64
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// lewati .git dan artefak version-control lain
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if part == ".git" || part == ".hg" || part == ".svn" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		// jangan ikuti symlink apa pun (file maupun dir)
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if copied+info.Size() > maxCopyBytes {
			return fmt.Errorf("skill exceeds %d bytes total", maxCopyBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		copied += int64(len(data))
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
		return nil
	})
}
