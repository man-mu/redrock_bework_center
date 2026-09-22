package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"be_homework_center/backend/internal/db"
)

func newTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return &Service{DB: d}, d
}

func seedLesson(t *testing.T, d *sql.DB, number int, slug, title string, exercises ...string) {
	t.Helper()
	res, err := d.Exec(`INSERT INTO lessons(number, slug, title) VALUES(?, ?, ?)`, number, slug, title)
	if err != nil {
		t.Fatalf("seed lesson: %v", err)
	}
	id, _ := res.LastInsertId()
	for i, e := range exercises {
		if _, err := d.Exec(`INSERT INTO exercises(lesson_id, slug, position) VALUES(?, ?, ?)`, id, e, i+1); err != nil {
			t.Fatalf("seed exercise: %v", err)
		}
	}
}

func countRows(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func makeTests(pass, fail int) []TestState {
	tests := make([]TestState, 0, pass+fail)
	for i := 0; i < pass; i++ {
		tests = append(tests, TestState{Test: testSlug(i + 1), Status: "pass"})
	}
	for i := 0; i < fail; i++ {
		tests = append(tests, TestState{Test: testSlug(pass + i + 1), Status: "fail"})
	}
	return tests
}

func testSlug(i int) string {
	if i < 10 {
		return "test0" + string(rune('0'+i))
	}
	return "test" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

const testRepoURL = "https://github.com/zhangsan/redrock_backend_practice_2026"

func TestReportLifecycle(t *testing.T) {
	svc, d := newTestService(t)
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01", "test02", "test03", "test04", "test05", "test06")

	base := func(commit, name string, pass, fail int) *ReportInput {
		return &ReportInput{
			RepoURL: testRepoURL,
			Commit:  commit,
			Ref:     "refs/heads/main",
			Event:   "push",
			Name:    name,
			Lesson:  "lesson-01-basics",
			Tests:   makeTests(pass, fail),
			Payload: `{"probe":"payload"}`,
		}
	}
	ctx := context.Background()

	// 未知课次 → UnknownLessonError，且零副作用（§3.2 / §3.3）
	in := base("c0", "张三", 3, 1)
	in.Lesson = "lesson-09-nope"
	_, err := svc.Report(ctx, in)
	var ue *UnknownLessonError
	if !errors.As(err, &ue) || ue.Lesson != "lesson-09-nope" {
		t.Fatalf("未知课次应返回 UnknownLessonError, got %v", err)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM students`); n != 0 {
		t.Fatalf("422 之后 students 应为空, got %d", n)
	}

	// 合法上报 → completed = pass 条数
	res, err := svc.Report(ctx, base("9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234", "张三", 3, 1))
	if err != nil {
		t.Fatalf("valid report: %v", err)
	}
	if res.Deduplicated || res.Completed != 3 {
		t.Fatalf("结果不符: %+v", res)
	}
	var name, owner string
	if err := d.QueryRow(`SELECT name, owner FROM students`).Scan(&name, &owner); err != nil || name != "张三" || owner != "zhangsan" {
		t.Fatalf("students upsert 不符: name=%q owner=%q err=%v", name, owner, err)
	}
	var completed int
	if err := d.QueryRow(`SELECT completed FROM student_lessons`).Scan(&completed); err != nil || completed != 3 {
		t.Fatalf("快照 completed: want 3, got %d err=%v", completed, err)
	}
	var payload string
	if err := d.QueryRow(`SELECT payload FROM reports`).Scan(&payload); err != nil || payload != `{"probe":"payload"}` {
		t.Fatalf("原始载荷应留档, got %q err=%v", payload, err)
	}

	// 同 (student, commit) 重复 → Deduplicated，不重复计数（§3.2）
	res, err = svc.Report(ctx, base("9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234", "张三", 3, 1))
	if err != nil || !res.Deduplicated {
		t.Fatalf("重复上报应 Deduplicated: res=%+v err=%v", res, err)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM reports`); n != 1 {
		t.Fatalf("重复上报后 reports 应仍为 1 条, got %d", n)
	}

	// 新 commit 同课次 → 覆盖快照（§4 步骤 8）
	if _, err := svc.Report(ctx, base("aabbccdd11223344556677889900aabbccddeeff", "张三", 6, 0)); err != nil {
		t.Fatalf("second report: %v", err)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM reports`); n != 2 {
		t.Fatalf("reports 应追加为 2 条, got %d", n)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM student_lessons`); n != 1 {
		t.Fatalf("快照应仍为 1 条（覆盖式）, got %d", n)
	}
	if err := d.QueryRow(`SELECT completed FROM student_lessons`).Scan(&completed); err != nil || completed != 6 {
		t.Fatalf("快照应被覆盖为 6, got %d err=%v", completed, err)
	}

	// config.name 为空 → 保留原姓名（§4 步骤 5）
	if _, err := svc.Report(ctx, base("1111222233334444555566667777888899990000", "", 5, 1)); err != nil {
		t.Fatalf("empty name report: %v", err)
	}
	if err := d.QueryRow(`SELECT name FROM students`).Scan(&name); err != nil || name != "张三" {
		t.Fatalf("空 name 不应覆盖原值, got %q err=%v", name, err)
	}
}

func TestReportIdentityBinding(t *testing.T) {
	svc, d := newTestService(t)
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01", "test02", "test03")

	valid := &ReportInput{
		RepoURL: testRepoURL, // zhangsan/redrock_backend_practice_2026
		Commit:  "9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234",
		Event:   "push",
		Lesson:  "lesson-01-basics",
		Tests:   makeTests(2, 0),
	}
	ctx := context.Background()

	// 令牌仓库与上报仓库不一致 → BindError，零副作用
	in := *valid
	in.Identity = &Identity{Repository: "attacker/other_repo", SHA: valid.Commit}
	if _, err := svc.Report(ctx, &in); !errors.As(err, new(*BindError)) {
		t.Fatalf("仓库不匹配应返回 BindError, got %v", err)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM students`); n != 0 {
		t.Fatalf("403 之后 students 应为空, got %d", n)
	}

	// 令牌 SHA 与上报 commit 不一致 → BindError
	in = *valid
	in.Identity = &Identity{Repository: "zhangsan/redrock_backend_practice_2026", SHA: "deadbeef"}
	if _, err := svc.Report(ctx, &in); !errors.As(err, new(*BindError)) {
		t.Fatalf("commit 不匹配应返回 BindError, got %v", err)
	}

	// 完全一致 → 通过；SHA 大小写不同也应通过（GitHub SHA 恒小写，此处从宽兜底）
	in = *valid
	in.Identity = &Identity{
		Repository: "zhangsan/redrock_backend_practice_2026",
		SHA:        strings.ToUpper(valid.Commit),
	}
	if _, err := svc.Report(ctx, &in); err != nil {
		t.Fatalf("一致的身份应通过（含大小写差异）: %v", err)
	}

	// Identity 为 nil（未启用鉴权）→ 与旧行为一致
	if _, err := svc.Report(ctx, valid); err != nil {
		t.Fatalf("未启用鉴权时不应受影响: %v", err)
	}
}

func TestReportValidation(t *testing.T) {
	svc, d := newTestService(t)
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01")

	valid := &ReportInput{
		RepoURL: "https://github.com/a/b",
		Commit:  "c1",
		Event:   "push",
		Lesson:  "lesson-01-basics",
		Tests:   []TestState{},
	}
	bad := func(mutate func(*ReportInput)) *ReportInput {
		in := *valid
		in.Tests = []TestState{}
		mutate(&in)
		return &in
	}
	cases := []struct {
		name string
		in   *ReportInput
	}{
		{"缺 repo_url", bad(func(i *ReportInput) { i.RepoURL = "" })},
		{"缺 commit", bad(func(i *ReportInput) { i.Commit = "" })},
		{"缺 lesson", bad(func(i *ReportInput) { i.Lesson = "" })},
		{"tests 字段缺失", bad(func(i *ReportInput) { i.Tests = nil })},
		{"event 非 push", bad(func(i *ReportInput) { i.Event = "pull_request" })},
		{"非法 status", bad(func(i *ReportInput) { i.Tests = []TestState{{Test: "t01", Status: "PASS"}} })},
		{"repo_url 非 GitHub", bad(func(i *ReportInput) { i.RepoURL = "https://gitlab.com/a/b" })},
		{"repo_url 带多余路径", bad(func(i *ReportInput) { i.RepoURL = "https://github.com/a/b/pulls/1" })},
	}
	ctx := context.Background()
	for _, tc := range cases {
		_, err := svc.Report(ctx, tc.in)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: 应返回 ValidationError, got %v", tc.name, err)
		}
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM students`); n != 0 {
		t.Errorf("全部校验失败后 students 应为空, got %d", n)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM reports`); n != 0 {
		t.Errorf("全部校验失败后 reports 应为空, got %d", n)
	}

	// git@ SSH 形式的 repo_url 也应能解析
	ssh := &ReportInput{
		RepoURL: "git@github.com:lisi/redrock_backend_practice_2026.git",
		Commit:  "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Event:   "push",
		Lesson:  "lesson-01-basics",
		Tests:   makeTests(1, 0),
	}
	if _, err := svc.Report(ctx, ssh); err != nil {
		t.Fatalf("ssh repo_url: %v", err)
	}
	var owner string
	if err := d.QueryRow(`SELECT owner FROM students`).Scan(&owner); err != nil || owner != "lisi" {
		t.Fatalf("owner 应解析为 lisi, got %q err=%v", owner, err)
	}
}

func TestDashboard(t *testing.T) {
	svc, d := newTestService(t)
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01", "test02", "test03", "test04", "test05", "test06")
	seedLesson(t, d, 2, "lesson-02-collections", "collections", "test01", "test02", "test03")

	repo := func(u string) string { return "https://github.com/" + u + "/redrock_backend_practice_2026" }
	report := func(u, commit, name, lesson string, pass int) {
		t.Helper()
		_, err := svc.Report(context.Background(), &ReportInput{
			RepoURL: repo(u), Commit: commit, Event: "push",
			Name: name, Lesson: lesson, Tests: makeTests(pass, 0), Payload: "{}",
		})
		if err != nil {
			t.Fatalf("report %s: %v", u, err)
		}
	}
	report("wangwu", "c1", "王五", "lesson-01-basics", 6)
	report("wangwu", "c2", "王五", "lesson-02-collections", 3) // 总 9
	report("zhangsan", "c3", "张三", "lesson-01-basics", 6)     // 6
	report("lina", "c4", "李娜", "lesson-01-basics", 6)         // 6（并列）
	report("wuming2026", "c5", "", "lesson-01-basics", 2)      // 2，姓名空
	report("chenjing", "c6", "陈静", "lesson-02-collections", 1) // 1
	ctx := context.Background()

	// overview：去重学生数 + 题目全集
	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(overview) != 2 {
		t.Fatalf("overview 应有 2 课, got %d", len(overview))
	}
	if overview[0].Lesson != 1 || overview[0].Submitted != 4 || overview[0].Exercises != 6 {
		t.Fatalf("第一课: %+v", overview[0])
	}
	if overview[1].Lesson != 2 || overview[1].Submitted != 2 || overview[1].Exercises != 3 {
		t.Fatalf("第二课: %+v", overview[1])
	}

	// leaderboard 全量：9 → (6,6 并列) → 2 → 1；并列内 repo 升序；空姓名用 owner 兜底
	board, err := svc.Leaderboard(ctx, "", "")
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	want := []struct {
		rank      int
		name      string
		repo      string
		completed int
	}{
		{1, "王五", "wangwu/redrock_backend_practice_2026", 9},
		{2, "李娜", "lina/redrock_backend_practice_2026", 6},
		{2, "张三", "zhangsan/redrock_backend_practice_2026", 6},
		{3, "wuming2026", "wuming2026/redrock_backend_practice_2026", 2},
		{4, "陈静", "chenjing/redrock_backend_practice_2026", 1},
	}
	if len(board) != len(want) {
		t.Fatalf("board 行数: want %d, got %d", len(want), len(board))
	}
	for i, wc := range want {
		b := board[i]
		if b.Rank != wc.rank || b.Name != wc.name || b.Repo != wc.repo || b.Completed != wc.completed {
			t.Errorf("board[%d]: want %+v, got %+v", i, wc, b)
		}
	}

	// 课次过滤：lesson=01（课程号带前导零）；第一课 4 人：6/6/6/2
	l1, err := svc.Leaderboard(ctx, "01", "")
	if err != nil {
		t.Fatalf("lesson=01: %v", err)
	}
	if len(l1) != 4 || l1[0].Completed != 6 || !strings.HasPrefix(l1[0].Repo, "lina") ||
		l1[0].Rank != 1 || l1[2].Rank != 1 || l1[3].Rank != 2 {
		t.Fatalf("lesson=01: %+v", l1)
	}

	// 未知课次 → 空数组
	if rows, _ := svc.Leaderboard(ctx, "lesson-99-nope", ""); len(rows) != 0 {
		t.Fatalf("未知课次应返回空, got %+v", rows)
	}

	// 关键字：命中姓名 / owner
	if rows, _ := svc.Leaderboard(ctx, "", "张"); len(rows) != 1 || !strings.HasPrefix(rows[0].Repo, "zhangsan") {
		t.Fatalf("q=张: %+v", rows)
	}
	if rows, _ := svc.Leaderboard(ctx, "", "wuming"); len(rows) != 1 || rows[0].Name != "wuming2026" {
		t.Fatalf("q=wuming: %+v", rows)
	}
	// LIKE 通配符应被转义：没有任何姓名 / 仓库名含 %，若转义失效 % 会匹配全部
	if rows, _ := svc.Leaderboard(ctx, "", "%"); len(rows) != 0 {
		t.Fatalf("q=%% 不应匹配任何仓库（通配符已转义）, got %+v", rows)
	}
}
