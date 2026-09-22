package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"be_homework_center/backend/internal/db"
	"be_homework_center/backend/internal/oidc"
	"be_homework_center/backend/internal/service"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func newTestRouter(t *testing.T) (*gin.Engine, *sql.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	svc := &service.Service{DB: d}
	g := gin.New()
	api := g.Group("/api/v1")
	api.GET("/overview", Overview(svc))
	api.GET("/leaderboard", Leaderboard(svc))
	api.POST("/reports", Report(svc))
	return g, d
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

// makeReportBody 构造 §3.1 形状的请求 JSON。
func makeReportBody(repoURL, commit, configName, lesson string, pass, fail int) string {
	var tests []string
	for i := 0; i < pass; i++ {
		tests = append(tests, fmt.Sprintf(`{"test":"test%02d","status":"pass"}`, i+1))
	}
	for i := 0; i < fail; i++ {
		tests = append(tests, fmt.Sprintf(`{"test":"test%02d","status":"fail"}`, pass+i+1))
	}
	nameJSON := "null"
	if configName != "" {
		nameJSON = `"` + configName + `"`
	}
	return fmt.Sprintf(`{
		"repo_url": %q,
		"commit": %q,
		"ref": "refs/heads/main",
		"event": "push",
		"config": {"name": %s, "lesson": %q},
		"result": {"lesson": %q, "tests": [%s]}
	}`, repoURL, commit, nameJSON, lesson, lesson, strings.Join(tests, ","))
}

func doJSON(t *testing.T, g *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	return w
}

func TestHandlerStatusCodes(t *testing.T) {
	g, d := newTestRouter(t)
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01", "test02", "test03")

	valid := makeReportBody("https://github.com/zhangsan/redrock_backend_practice_2026",
		"9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234", "张三", "lesson-01-basics", 2, 1)

	// 合法 → 202 + completed
	w := doJSON(t, g, "POST", "/api/v1/reports", valid)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"completed":2`) {
		t.Fatalf("valid: want 202 completed=2, got %d body=%s", w.Code, w.Body.String())
	}

	// 同 (student, commit) → 200 deduplicated
	w = doJSON(t, g, "POST", "/api/v1/reports", valid)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"deduplicated":true`) {
		t.Fatalf("duplicate: want 200 deduplicated, got %d body=%s", w.Code, w.Body.String())
	}

	// 未知课次 → 422 unknown_lesson（§3.2）
	unknown := strings.ReplaceAll(valid, "lesson-01-basics", "lesson-09-nope")
	w = doJSON(t, g, "POST", "/api/v1/reports", unknown)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"unknown_lesson"`) {
		t.Fatalf("unknown lesson: want 422, got %d body=%s", w.Code, w.Body.String())
	}

	// service 校验失败 → 400 invalid_payload + detail
	w = doJSON(t, g, "POST", "/api/v1/reports",
		strings.Replace(valid, `"repo_url": "https://github.com/zhangsan/redrock_backend_practice_2026"`, `"repo_url": ""`, 1))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_payload"`) {
		t.Fatalf("missing repo_url: want 400, got %d body=%s", w.Code, w.Body.String())
	}

	// 坏 JSON → 400
	w = doJSON(t, g, "POST", "/api/v1/reports", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: want 400, got %d", w.Code)
	}

	// result.tests 字段缺失 → 400
	w = doJSON(t, g, "POST", "/api/v1/reports",
		`{"repo_url":"https://github.com/a/b","commit":"c1","result":{"lesson":"lesson-01-basics"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing tests: want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandlerReadEndpoints(t *testing.T) {
	g, d := newTestRouter(t)
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01", "test02", "test03", "test04", "test05", "test06")

	for _, c := range []struct{ owner, commit, name string }{
		{"zhangsan", "c1", "张三"}, {"lisi", "c2", "李四"},
	} {
		w := doJSON(t, g, "POST", "/api/v1/reports",
			makeReportBody("https://github.com/"+c.owner+"/redrock_backend_practice_2026", c.commit, c.name, "lesson-01-basics", 3, 0))
		if w.Code != http.StatusAccepted {
			t.Fatalf("report %s: got %d body=%s", c.name, w.Code, w.Body.String())
		}
	}

	// overview → 200 JSON 数组
	w := doJSON(t, g, "GET", "/api/v1/overview", "")
	if w.Code != http.StatusOK {
		t.Fatalf("overview: got %d", w.Code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("overview 应为 1 课的数组: body=%s err=%v", w.Body.String(), err)
	}

	// leaderboard 支持课次与关键字参数
	w = doJSON(t, g, "GET", "/api/v1/leaderboard?q=%E5%BC%A0", "") // 张
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "zhangsan") {
		t.Fatalf("leaderboard q=张: got %d body=%s", w.Code, w.Body.String())
	}
	w = doJSON(t, g, "GET", "/api/v1/leaderboard?lesson=01", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"completed":3`) {
		t.Fatalf("leaderboard lesson=01: got %d body=%s", w.Code, w.Body.String())
	}
	w = doJSON(t, g, "GET", "/api/v1/leaderboard?lesson=lesson-99-nope", "")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("未知课次应返回 [], got %d body=%s", w.Code, w.Body.String())
	}
}

// testIdP 用测试 RSA 密钥模拟 GitHub 的 OIDC Provider（完整辅助在 oidc 包测试内）。
type testIdP struct {
	key *rsa.PrivateKey
	kid string
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥: %v", err)
	}
	return &testIdP{key: key, kid: "test-key-1"}
}

func (p *testIdP) jwksJSON() json.RawMessage {
	pub := p.key.PublicKey
	raw, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": p.kid,
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// sign 签发携带 repository / sha 声明的 OIDC 令牌。
func (p *testIdP) sign(t *testing.T, repo, sha string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":        oidc.Issuer,
		"aud":        []string{"homework-center"},
		"exp":        time.Now().Add(5 * time.Minute).Unix(),
		"repository": repo,
		"sha":        sha,
	})
	tok.Header["kid"] = p.kid
	raw, err := tok.SignedString(p.key)
	if err != nil {
		t.Fatalf("签发令牌: %v", err)
	}
	return raw
}

// postReport 以可选 Bearer 令牌 POST 一份报告。
func postReport(t *testing.T, g *gin.Engine, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	return w
}

func TestHandlerOIDCAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idp := newTestIdP(t)
	kf, err := keyfunc.NewJWKSetJSON(idp.jwksJSON())
	if err != nil {
		t.Fatalf("构造测试 keyfunc: %v", err)
	}
	verifier := oidc.NewWithKeyfunc(kf, "homework-center")

	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()
	d.SetMaxOpenConns(1)
	if err := db.EnsureSchema(d); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	seedLesson(t, d, 1, "lesson-01-basics", "basics", "test01", "test02")
	svc := &service.Service{DB: d}

	g := gin.New()
	api := g.Group("/api/v1")
	api.POST("/reports", OIDCAuth(verifier), Report(svc))

	const (
		repo   = "zhangsan/redrock_backend_practice_2026"
		commit = "9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234"
	)
	body := makeReportBody("https://github.com/"+repo, commit, "张三", "lesson-01-basics", 2, 0)

	// 无令牌 → 401 invalid_token
	if w := postReport(t, g, "", body); w.Code != http.StatusUnauthorized ||
		!strings.Contains(w.Body.String(), `"invalid_token"`) {
		t.Fatalf("无令牌: want 401, got %d body=%s", w.Code, w.Body.String())
	}

	// 垃圾令牌 → 401
	if w := postReport(t, g, "not.a.jwt", body); w.Code != http.StatusUnauthorized {
		t.Fatalf("垃圾令牌: want 401, got %d body=%s", w.Code, w.Body.String())
	}

	// 令牌仓库与载荷 repo_url 不符 → 403 repository_mismatch
	w := postReport(t, g, idp.sign(t, "attacker/other_repo", commit), body)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"repository_mismatch"`) {
		t.Fatalf("仓库不匹配: want 403, got %d body=%s", w.Code, w.Body.String())
	}

	// 令牌 sha 与载荷 commit 不符 → 403 commit_mismatch
	w = postReport(t, g, idp.sign(t, repo, "deadbeef"), body)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"commit_mismatch"`) {
		t.Fatalf("commit 不匹配: want 403, got %d body=%s", w.Code, w.Body.String())
	}

	// 全部匹配 → 202，且数据库确实入库
	w = postReport(t, g, idp.sign(t, repo, commit), body)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"completed":2`) {
		t.Fatalf("匹配: want 202, got %d body=%s", w.Code, w.Body.String())
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM reports`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("通过鉴权的上报应入库, reports=%d err=%v", n, err)
	}

	// 鉴权失败（401/403）不应留下任何数据
	var students int
	_ = d.QueryRow(`SELECT COUNT(*) FROM students`).Scan(&students)
	if students != 1 { // 仅合法那次入库的学生
		t.Fatalf("students 应只有 1 条, got %d", students)
	}
	var reports int
	if err := d.QueryRow(`SELECT COUNT(*) FROM reports`).Scan(&reports); err != nil || reports != 1 {
		t.Fatalf("reports 应只有 1 条, got %d", reports)
	}
}
