package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Source struct {
	Account string `json:"account"`
	FeedURL string `json:"feed_url"`
}
type Config struct {
	RepoPath  string   `json:"repo_path"`
	StateDir  string   `json:"state_dir"`
	Branch    string   `json:"branch"`
	Remote    string   `json:"remote"`
	GitPush   bool     `json:"git_push"`
	StartDate string   `json:"start_date"`
	Sources   []Source `json:"sources"`
}

var accounts = map[string]bool{"金渐成": true, "天机奇谈": true, "生玑伯伯": true}
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

func loadConfig(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, fmt.Errorf("配置必须只包含一个 JSON 对象")
	}
	if !filepath.IsAbs(c.RepoPath) || !filepath.IsAbs(c.StateDir) {
		return c, fmt.Errorf("repo_path 和 state_dir 必须为绝对路径")
	}
	c.RepoPath = filepath.Clean(c.RepoPath)
	c.StateDir = filepath.Clean(c.StateDir)
	rel, _ := filepath.Rel(c.RepoPath, c.StateDir)
	if rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return c, fmt.Errorf("state_dir 必须位于仓库外")
	}
	if c.Branch == "" {
		c.Branch = "main"
	}
	if c.Remote == "" {
		c.Remote = "origin"
	}
	if strings.HasPrefix(c.Branch, "-") || strings.HasPrefix(c.Remote, "-") || strings.ContainsAny(c.Branch+c.Remote, " \n\t") {
		return c, fmt.Errorf("branch 或 remote 无效")
	}
	if c.StartDate != "" {
		if _, err = time.Parse("2006-01-02", c.StartDate); err != nil {
			return c, fmt.Errorf("start_date 必须为 YYYY-MM-DD")
		}
	}
	if len(c.Sources) == 0 {
		return c, fmt.Errorf("至少配置一个订阅源")
	}
	seen := map[string]bool{}
	for _, s := range c.Sources {
		if !accounts[s.Account] || seen[s.Account] {
			return c, fmt.Errorf("公众号目录无效或重复: %s", s.Account)
		}
		seen[s.Account] = true
		u, e := url.Parse(s.FeedURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("%s: 请填写有效的全文 RSS 地址", s.Account)
		}
	}
	return c, nil
}
