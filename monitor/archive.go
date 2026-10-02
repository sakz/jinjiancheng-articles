package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var sourcePattern = regexp.MustCompile(`(?m)^>\s*原文地址[^\n]*?\]\((https?://[^)]+)\)`)
var headingPattern = regexp.MustCompile(`(?m)^#\s+([^\n]+)`)

type ArchiveIndex struct {
	IDs    map[string]bool
	Legacy map[string]bool
	Names  map[string]bool
}

func loadArchive(repo string) (ArchiveIndex, error) {
	idx := ArchiveIndex{map[string]bool{}, map[string]bool{}, map[string]bool{}}
	for account := range accounts {
		folder := filepath.Join(repo, "md", "天玑全集", account)
		entries, err := os.ReadDir(folder)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return idx, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return idx, fmt.Errorf("归档不允许符号链接: %s", entry.Name())
			}
			b, e := os.ReadFile(filepath.Join(folder, entry.Name()))
			if e != nil {
				return idx, e
			}
			m := sourcePattern.FindSubmatch(b)
			if len(m) != 2 {
				continue
			}
			_, id, e := articleURL(string(m[1]))
			if e != nil {
				continue
			}
			idx.IDs[account+"|"+id] = true
			title := headingPattern.FindSubmatch(b)
			if len(title) == 2 && len(entry.Name()) > 12 && entry.Name()[0] == '[' && entry.Name()[11] == ']' {
				key := account + "|" + entry.Name()[1:11] + "|" + strings.TrimSpace(string(title[1]))
				idx.Names[key] = true
				if strings.HasPrefix(id, "short:") {
					idx.Legacy[key] = true
				}
			}
		}
	}
	return idx, nil
}
func (idx ArchiveIndex) contains(a Article) bool {
	key := a.Account + "|" + a.Identity
	name := a.Account + "|" + a.Date + "|" + a.Title
	return idx.IDs[key] || idx.Legacy[name] || (strings.HasPrefix(a.Identity, "short:") && idx.Names[name])
}
func (idx ArchiveIndex) add(a Article) {
	idx.IDs[a.Account+"|"+a.Identity] = true
	idx.Names[a.Account+"|"+a.Date+"|"+a.Title] = true
	if strings.HasPrefix(a.Identity, "short:") {
		idx.Legacy[a.Account+"|"+a.Date+"|"+a.Title] = true
	}
}
func safeTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" {
		s = "未命名文章"
	}
	for len(s) > 180 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}
func articlePath(repo string, a Article) (string, error) {
	base := filepath.Join("md", "天玑全集", a.Account, "["+a.Date+"]"+safeTitle(a.Title)+".md")
	if _, err := os.Lstat(filepath.Join(repo, base)); os.IsNotExist(err) {
		return base, nil
	} else if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(a.Identity))
	base = strings.TrimSuffix(base, ".md") + "_" + hex.EncodeToString(h[:6]) + ".md"
	if _, err := os.Lstat(filepath.Join(repo, base)); os.IsNotExist(err) {
		return base, nil
	} else if err != nil {
		return "", err
	}
	return "", fmt.Errorf("文件名冲突，拒绝覆盖: %s", base)
}
func markdown(a Article) string {
	title := strings.Join(strings.Fields(a.Title), " ")
	return fmt.Sprintf("# %s\n\n%s %s\n\n> 原文地址: [%s](%s)\n\n%s\n\n> 归档说明：由 Go 监控程序从全文 RSS 保存，未采集微信留言。\n", title, a.Account, a.Date, a.URL, a.URL, a.Body)
}

// Temporary files stay outside Git; rename publishes a complete file on the same filesystem.
func atomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0600)
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".monitor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

type Journal struct {
	Pending []string `json:"pending"`
}

func loadJournal(c Config) (Journal, error) {
	var j Journal
	b, err := os.ReadFile(filepath.Join(c.StateDir, "pending.json"))
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	if err = json.Unmarshal(b, &j); err != nil {
		return j, err
	}
	for _, p := range j.Pending {
		if !validArticlePath(p) {
			return j, fmt.Errorf("待提交记录包含非法路径")
		}
	}
	return j, nil
}
func validArticlePath(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	return len(parts) == 4 && parts[0] == "md" && parts[1] == "天玑全集" && accounts[parts[2]] && strings.HasSuffix(parts[3], ".md") && filepath.Clean(p) == p
}
func saveJournal(c Config, j Journal) error {
	return atomicJSON(filepath.Join(c.StateDir, "pending.json"), j)
}
func writeArticle(c Config, j *Journal, a Article) (string, error) {
	rel, err := articlePath(c.RepoPath, a)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(filepath.Join(c.RepoPath, rel))
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	// Reject symlink parents: the monitor must never write outside the checkout.
	for p := dir; p != c.RepoPath; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("文章目录含符号链接")
		}
	}
	j.Pending = append(j.Pending, rel)
	if err = saveJournal(c, *j); err != nil {
		return "", err
	}
	full := filepath.Join(c.RepoPath, rel)
	temp := full + ".monitor-partial"
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	defer os.Remove(temp)
	_, err = f.WriteString(markdown(a))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	// A hard link atomically makes the complete file visible, without replacing an existing file.
	if err = os.Link(temp, full); err != nil {
		return "", err
	}
	return rel, nil
}
func recoverPartial(c Config, j Journal) error {
	for _, p := range j.Pending {
		full := filepath.Join(c.RepoPath, p)
		for dir := filepath.Dir(full); dir != c.RepoPath; dir = filepath.Dir(dir) {
			info, err := os.Lstat(dir)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("待提交目录包含符号链接")
			}
		}
		if err := os.Remove(full + ".monitor-partial"); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
