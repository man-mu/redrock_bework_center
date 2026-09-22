package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

// 兼容 https://github.com/owner/name(.git) 与 git@github.com:owner/name.git
var repoURLRe = regexp.MustCompile(`github\.com[/:]([A-Za-z0-9-]+[A-Za-z0-9_.-]*)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

func parseRepo(raw string) (owner, name string, ok bool) {
	m := repoURLRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// Report 上报写入流程（§4）：全部校验先行，任何 4xx 不得留下半条记录；
// (student_id, commit) 唯一 → Deduplicated；同一课次重复上报覆盖快照。
func (s *Service) Report(ctx context.Context, in *ReportInput) (*ReportResult, error) {
	// —— 校验（§3.1/§3.2）：全部在任何写入之前 ——
	if in.RepoURL == "" || in.Commit == "" || in.Lesson == "" {
		return nil, &ValidationError{Detail: "repo_url / commit / result 缺失"}
	}
	if in.Tests == nil {
		return nil, &ValidationError{Detail: "result.tests 缺失（可为空数组，但字段必须存在）"}
	}
	if in.Event != "" && in.Event != "push" {
		return nil, &ValidationError{Detail: "event 仅支持 push"}
	}
	for _, t := range in.Tests {
		if t.Status != "pass" && t.Status != "fail" {
			return nil, &ValidationError{Detail: "tests[].status 只允许 pass / fail"}
		}
	}
	owner, name, ok := parseRepo(in.RepoURL)
	if !ok {
		return nil, &ValidationError{Detail: "repo_url 无法解析出 GitHub owner/name"}
	}
	repo := owner + "/" + name
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1) 课次必须存在（§3.2 422），在任何写入之前确认
	var lessonID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM lessons WHERE slug = ?`, in.Lesson).Scan(&lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &UnknownLessonError{Lesson: in.Lesson}
	}
	if err != nil {
		return nil, err
	}

	// 2) 幂等：同 (student, commit) 只收一次（§3.3）
	var studentID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM students WHERE repo = ?`, repo).Scan(&studentID)
	switch {
	case err == nil:
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM reports WHERE student_id = ? AND "commit" = ?`,
			studentID, in.Commit).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return &ReportResult{Deduplicated: true}, nil
		}
	case errors.Is(err, sql.ErrNoRows):
		// 新学生，稍后 upsert
	default:
		return nil, err
	}

	// 3) upsert students：name 取 config.name，空则保留原值（§4 步骤 5）
	configName := strings.TrimSpace(in.Name)
	if _, err := tx.ExecContext(ctx, `INSERT INTO students(repo, owner, repo_url, name, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(repo) DO UPDATE SET
			name       = CASE WHEN excluded.name = '' THEN students.name ELSE excluded.name END,
			repo_url   = excluded.repo_url,
			updated_at = excluded.updated_at`,
		repo, owner, in.RepoURL, configName, now); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM students WHERE repo = ?`, repo).Scan(&studentID); err != nil {
		return nil, err
	}

	// 4) 追加 reports（原始 JSON 留档，只增不改）
	if _, err := tx.ExecContext(ctx, `INSERT INTO reports
		(student_id, lesson_id, "commit", ref, payload, received_at) VALUES(?, ?, ?, ?, ?, ?)`,
		studentID, lessonID, in.Commit, in.Ref, in.Payload, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: reports.") {
			// 并发窗口内撞上同 (student, commit)：按幂等语义返回
			return &ReportResult{Deduplicated: true}, nil
		}
		return nil, err
	}

	// 5) 计算完成题数并覆盖该课快照（§4 步骤 7-8）
	completed := 0
	for _, t := range in.Tests {
		if t.Status == "pass" {
			completed++
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO student_lessons
		(student_id, lesson_id, completed, "commit", reported_at) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(student_id, lesson_id) DO UPDATE SET
			completed   = excluded.completed,
			"commit"    = excluded."commit",
			reported_at = excluded.reported_at`,
		studentID, lessonID, completed, in.Commit, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ReportResult{Completed: completed}, nil
}
