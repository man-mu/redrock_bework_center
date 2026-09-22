package service

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// LessonOverview 对应 §5 的看板输出结构体。
type LessonOverview struct {
	Lesson    int    `json:"lesson"`    // 课程号
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Submitted int    `json:"submitted"` // 去重后的学生数
	Exercises int    `json:"exercises"` // 题目全集
}

// LeaderboardRow 对应 §5 的排行榜行。
type LeaderboardRow struct {
	Rank      int    `json:"rank"`
	Repo      string `json:"repo"`
	RepoURL   string `json:"repo_url"`
	Name      string `json:"name"`
	Completed int    `json:"completed"`
}

// Overview 每课提交学生数（§7.1）：口径是去重后的学生数，
// 同一人多次 push 同一课只算一个；题目全集数来自模板仓库同步结果。
func (s *Service) Overview(ctx context.Context) ([]LessonOverview, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT l.number, l.slug, l.title,
		       COUNT(sl.student_id)        AS submitted,
		       (SELECT COUNT(*) FROM exercises e
		         WHERE e.lesson_id = l.id) AS exercises
		FROM lessons l
		LEFT JOIN student_lessons sl ON sl.lesson_id = l.id
		GROUP BY l.id
		ORDER BY l.number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LessonOverview{}
	for rows.Next() {
		var o LessonOverview
		if err := rows.Scan(&o.Lesson, &o.Slug, &o.Title, &o.Submitted, &o.Exercises); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Leaderboard 按完成题数排行（§7.2）：
//   - lesson 可传 1 / 01 / lesson-01-basics；未知课次返回空数组；
//   - keyword 对姓名 / owner / 仓库做模糊匹配（LIKE 通配符已转义）；
//   - 完成数并列同名次（顺延），并列内按 repo 升序保证稳定。
func (s *Service) Leaderboard(ctx context.Context, lessonParam, keyword string) ([]LeaderboardRow, error) {
	lessonParam = strings.TrimSpace(lessonParam)
	keyword = strings.TrimSpace(keyword)

	var lessonID any // nil = 全部课次
	if lessonParam != "" && lessonParam != "all" {
		id, ok := resolveLessonID(s.DB, lessonParam)
		if !ok {
			return []LeaderboardRow{}, nil
		}
		lessonID = id
	}

	like := "%" + escapeLike(keyword) + "%"
	rows, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(s.name, ''), s.owner) AS name,
		       s.repo, s.repo_url,
		       SUM(sl.completed) AS completed
		FROM student_lessons sl
		JOIN students s ON s.id = sl.student_id
		WHERE (? IS NULL OR sl.lesson_id = ?)
		  AND (? = ''
		       OR s.name  LIKE ? ESCAPE '\'
		       OR s.owner LIKE ? ESCAPE '\'
		       OR s.repo  LIKE ? ESCAPE '\')
		GROUP BY s.id
		ORDER BY completed DESC, s.repo ASC`,
		lessonID, lessonID, keyword, like, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LeaderboardRow{}
	rank, prev := 0, -1
	for rows.Next() {
		var r LeaderboardRow
		if err := rows.Scan(&r.Name, &r.Repo, &r.RepoURL, &r.Completed); err != nil {
			return nil, err
		}
		if r.Completed != prev {
			rank++
			prev = r.Completed
		}
		r.Rank = rank
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveLessonID 课程号解析（§7.2）：纯数字按 number 匹配，否则按 slug 匹配。
func resolveLessonID(db *sql.DB, v string) (int64, bool) {
	var id int64
	var err error
	if n, convErr := strconv.Atoi(v); convErr == nil {
		err = db.QueryRow(`SELECT id FROM lessons WHERE number = ?`, n).Scan(&id)
	} else {
		err = db.QueryRow(`SELECT id FROM lessons WHERE slug = ?`, v).Scan(&id)
	}
	if err != nil {
		return 0, false
	}
	return id, true
}

// escapeLike 转义 LIKE 通配符，避免关键字里的 % _ 影响匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
