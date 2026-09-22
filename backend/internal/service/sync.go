package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Syncer 从模板仓库目录树同步课次与题目全集（§2.1）。
type Syncer struct {
	DB    *sql.DB
	Repo  string // owner/name
	Ref   string
	Token string // 可选 GitHub token，提升速率限制
}

// Run 启动即同步一次，之后按间隔刷新；同步失败只记日志，不影响看板服务。
func (s *Syncer) Run(ctx context.Context, every time.Duration) {
	s.tick(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Syncer) tick(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := s.SyncOnce(c); err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("[sync] 模板仓库同步失败: %v", err)
		return
	}
	log.Printf("[sync] 模板仓库同步完成: %s@%s", s.Repo, s.Ref)
}

type ghTree struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

var (
	lessonRe   = regexp.MustCompile(`^lesson-(\d+)-(.*)$`)
	exerciseRe = regexp.MustCompile(`^test\d+$`)
)

type lessonFull struct {
	number    int
	slug      string
	title     string
	exercises map[string]bool
}

// SyncOnce 拉取目录树并重建 lessons / exercises；course_sync 记录同步结果供排障。
// 已消失的课次保留不删（学生历史数据仍挂在上面），题目全集按模板仓库现状重建。
func (s *Syncer) SyncOnce(ctx context.Context) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", s.Repo, s.Ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "homework-center")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	var tree ghTree
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return err
	}
	if tree.Truncated {
		return fmt.Errorf("目录树被截断（仓库过大），需要改用 git clone 同步")
	}

	// 识别规则（§2.1）：顶层目录 lesson-(\d+)- → 课次；其下 test\d+ 子目录 → 题目
	lessons := map[string]*lessonFull{}
	for _, e := range tree.Tree {
		if e.Type != "tree" {
			continue
		}
		parts := strings.SplitN(e.Path, "/", 3)
		m := lessonRe.FindStringSubmatch(parts[0])
		if m == nil {
			continue
		}
		l := lessons[parts[0]]
		if l == nil {
			n, _ := strconv.Atoi(m[1])
			l = &lessonFull{number: n, slug: parts[0], title: m[2], exercises: map[string]bool{}}
			lessons[parts[0]] = l
		}
		if len(parts) >= 2 && exerciseRe.MatchString(parts[1]) {
			l.exercises[parts[1]] = true
		}
	}
	if len(lessons) == 0 {
		return fmt.Errorf("目录树中未识别到 lesson-N-* 课次目录")
	}

	list := make([]*lessonFull, 0, len(lessons))
	for _, l := range lessons {
		list = append(list, l)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].number < list[j].number })

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, l := range list {
		if _, err := tx.ExecContext(ctx, `INSERT INTO lessons(number, slug, title) VALUES(?, ?, ?)
			ON CONFLICT(number) DO UPDATE SET slug = excluded.slug, title = excluded.title`,
			l.number, l.slug, l.title); err != nil {
			return err
		}
		var lessonID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM lessons WHERE number = ?`, l.number).Scan(&lessonID); err != nil {
			return err
		}
		// 题目全集是排行榜/进度的分母：按模板仓库现状重建
		if _, err := tx.ExecContext(ctx, `DELETE FROM exercises WHERE lesson_id = ?`, lessonID); err != nil {
			return err
		}
		exs := make([]string, 0, len(l.exercises))
		for slug := range l.exercises {
			exs = append(exs, slug)
		}
		sort.Slice(exs, func(i, j int) bool {
			a, _ := strconv.Atoi(strings.TrimPrefix(exs[i], "test"))
			b, _ := strconv.Atoi(strings.TrimPrefix(exs[j], "test"))
			return a < b
		})
		for i, slug := range exs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO exercises(lesson_id, slug, position) VALUES(?, ?, ?)`,
				lessonID, slug, i+1); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO course_sync(id, repo, ref, synced_at) VALUES(1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET repo = excluded.repo, ref = excluded.ref, synced_at = excluded.synced_at`,
		s.Repo, s.Ref, now); err != nil {
		return err
	}
	return tx.Commit()
}
