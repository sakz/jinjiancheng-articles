package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func git(ctx context.Context, c Config, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", c.RepoPath}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=20")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s 失败（退出码/连接/权限问题；请按部署文档检查）", args[0])
	}
	return strings.TrimSpace(string(out)), nil
}
func checkWorktree(ctx context.Context, c Config, j Journal) error {
	branch, err := git(ctx, c, "branch", "--show-current")
	if err != nil {
		return err
	}
	if branch != c.Branch {
		return fmt.Errorf("当前分支 %s，不是配置分支 %s", branch, c.Branch)
	}
	allowed := map[string]bool{}
	for _, p := range j.Pending {
		allowed[p] = true
	}
	// -z avoids quoted Chinese filenames and detects staged changes as well.
	cmd := exec.CommandContext(ctx, "git", "-C", c.RepoPath, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(b), "\x00") {
		if entry == "" {
			continue
		}
		if len(entry) < 4 {
			return fmt.Errorf("无法识别工作区状态")
		}
		if (entry[:2] != "??" && entry[:2] != "A ") || !allowed[entry[3:]] {
			return fmt.Errorf("仓库存在不属于监控待提交记录的改动，请使用专用干净克隆")
		}
	}
	return nil
}
func commitPending(ctx context.Context, c Config, j *Journal) error {
	var files []string
	for _, p := range j.Pending {
		info, err := os.Lstat(filepath.Join(c.RepoPath, p))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("待提交文章不是普通文件")
		}
		files = append(files, p)
	}
	if len(files) > 0 {
		args := append([]string{"add", "--"}, files...)
		if _, err := git(ctx, c, args...); err != nil {
			return err
		}
		names, err := git(ctx, c, "diff", "--cached", "--name-only", "-z")
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, p := range files {
			allowed[p] = true
		}
		for _, p := range strings.Split(names, "\x00") {
			if p != "" && !allowed[p] {
				return fmt.Errorf("暂存区含无关改动，拒绝提交")
			}
		}
		if names != "" {
			if _, err = git(ctx, c, "-c", "user.name=Tianji Article Monitor", "-c", "user.email=article-monitor@users.noreply.github.com", "commit", "-m", fmt.Sprintf("feat(articles): 自动归档 %d 篇公众号文章", len(files))); err != nil {
				return err
			}
		}
	}
	j.Pending = nil
	return saveJournal(c, *j)
}
func syncRepo(ctx context.Context, c Config) error {
	if _, err := git(ctx, c, "fetch", c.Remote, c.Branch); err != nil {
		return err
	}
	ref := c.Remote + "/" + c.Branch
	counts, err := git(ctx, c, "rev-list", "--left-right", "--count", "HEAD..."+ref)
	if err != nil {
		return err
	}
	var ahead, behind int
	if _, err = fmt.Sscanf(counts, "%d %d", &ahead, &behind); err != nil {
		return err
	}
	if ahead > 0 && behind > 0 {
		return fmt.Errorf("本地和远端分支已分叉；请手动解决后重试，不自动覆盖或强制推送")
	}
	if ahead > 0 && c.GitPush {
		commits, e := git(ctx, c, "log", "--format=%H", ref+"..HEAD")
		if e != nil {
			return e
		}
		for _, hash := range strings.Fields(commits) {
			subject, e := git(ctx, c, "show", "-s", "--format=%s", hash)
			if e != nil {
				return e
			}
			if !strings.HasPrefix(subject, "feat(articles): 自动归档 ") {
				return fmt.Errorf("存在非监控程序的本地提交，拒绝自动推送")
			}
			changes, e := git(ctx, c, "diff-tree", "--no-commit-id", "--name-status", "-z", "-r", hash)
			if e != nil {
				return e
			}
			entries := strings.Split(strings.TrimSuffix(changes, "\x00"), "\x00")
			if len(entries) == 0 || len(entries)%2 != 0 {
				return fmt.Errorf("本地提交结构不符合归档要求")
			}
			for i := 0; i < len(entries); i += 2 {
				if entries[i] != "A" || !validArticlePath(entries[i+1]) {
					return fmt.Errorf("本地提交包含非新增文章改动，拒绝自动推送")
				}
			}
		}
		if _, err = git(ctx, c, "push", c.Remote, "HEAD:refs/heads/"+c.Branch); err != nil {
			return err
		}
	}
	if behind > 0 {
		_, err = git(ctx, c, "merge", "--ff-only", ref)
	}
	return err
}
