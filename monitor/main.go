package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	path := flag.String("config", "/etc/tianji-monitor/config.json", "配置 JSON 路径")
	dry := flag.Bool("dry-run", false, "只读取和报告，不写入文章、不提交或推送")
	flag.Parse()
	c, err := loadConfig(*path)
	if err != nil {
		log.Fatal(err)
	}
	if err = os.MkdirAll(c.StateDir, 0700); err != nil {
		log.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(c.StateDir, "monitor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Fatal("另一个采集任务正在运行，或无法获取进程锁")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err = run(ctx, c, *dry); err != nil {
		log.Fatal(err)
	}
}
func run(ctx context.Context, c Config, dry bool) error {
	j, err := loadJournal(c)
	if err != nil {
		return err
	}
	if !dry {
		if err = recoverPartial(c, j); err != nil {
			return err
		}
		if err = checkWorktree(ctx, c, j); err != nil {
			return err
		}
		if len(j.Pending) > 0 {
			if err = commitPending(ctx, c, &j); err != nil {
				return err
			}
		}
		// Retry an earlier committed but unpushed batch before collecting anything else.
		if err = syncRepo(ctx, c); err != nil {
			return err
		}
	}
	idx, err := loadArchive(c.RepoPath)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	var problems []error
	count := 0
	for _, s := range c.Sources {
		items, e := readArticles(ctx, client, s, c.StartDate)
		if e != nil {
			problems = append(problems, fmt.Errorf("%s: %w", s.Account, e))
			continue
		}
		fresh := 0
		for _, item := range items {
			if c.StartDate != "" && feedDate(item) != "" && feedDate(item) < c.StartDate {
				continue
			}
			_, id, e := articleURL(item.Link)
			if e == nil && idx.contains(Article{Account: s.Account, Identity: id, Date: feedDate(item), Title: strings.Join(strings.Fields(item.Title), " ")}) {
				continue
			}
			a, e := itemArticle(s, item)
			if e != nil {
				problems = append(problems, fmt.Errorf("%s: %w", s.Account, e))
				continue
			}
			if idx.contains(a) {
				continue
			}
			if dry {
				log.Printf("[预览] %s %s %s", a.Account, a.Date, a.Title)
			} else {
				rel, e := writeArticle(c, &j, a)
				if e != nil {
					return e
				}
				log.Printf("新增: %s", rel)
			}
			idx.add(a)
			fresh++
			count++
		}
		log.Printf("%s: RSS %d 条，新增 %d 篇", s.Account, len(items), fresh)
	}
	if !dry && len(j.Pending) > 0 {
		if err = commitPending(ctx, c, &j); err != nil {
			return err
		}
		if c.GitPush {
			if _, err = git(ctx, c, "push", c.Remote, "HEAD:refs/heads/"+c.Branch); err != nil {
				return err
			}
		}
	}
	log.Printf("本次共 %d 篇新增（dry_run=%t, git_push=%t）", count, dry, c.GitPush)
	return errors.Join(problems...)
}
