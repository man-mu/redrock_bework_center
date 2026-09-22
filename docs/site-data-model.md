# 红岩网校成绩中心：数据模型与接口约定

> 本文件**不属于练习仓库**。练习仓库只负责「按这份约定上报」，确认后请把本文件转移到站点项目的仓库里（与前一份 `center-site-api.md` 同样的处理方式）。
>
> 配套：练习仓库在本机 `/Users/manmu/code/golang/GitHub Classroom/redrock_backend_practice_2026`，产载荷的是 `.github/scripts/notify_center.py`。

## 1 定位

站点是一个**只读的统计看板**：接收练习仓库 CI 推来的报告，聚合成两个视图。

- 站点**不执行学生代码、不跑测试、不复算**。报告里的结论由学生自己仓库的 CI 产生，是自报值，未经复核。
- 站点**不做防作弊设计**。学生对自己的仓库有管理权限，理论上可以改测试与脚本。看板如实呈现上报值即可，不要引入「异常检测」「疑似作弊」之类的标记——这是明确的产品决定。
- 站点只有两个数据来源：**模板仓库**（课程全集）与 **CI 载荷**（上报事实）。没有名单导入、没有后台录入、没有第三方数据。

## 2 数据来源

| 来源 | 提供什么 | 怎么用 |
|---|---|---|
| 模板仓库（教师维护的练习仓库） | 课次全集与每课的题目全集 | 启动时与定时同步一次，写入 `lessons` / `exercises` |
| CI 载荷 | 「谁、哪一课、每题做完没有」 | `POST /api/v1/reports` 接收，写入 `reports` / `student_lessons` |

### 2.1 课程全集的同步

站点配置模板仓库的 `owner/name` 与分支（默认 `main`）。同步时取它的目录树：

- GitHub API：`GET /repos/{owner}/{repo}/git/trees/{ref}?recursive=1`；
- 或 `git clone --depth 1` 后本地遍历。

识别规则：

| 实体 | 规则 | 例 |
|---|---|---|
| 课次 | 顶层目录匹配 `lesson-(\d+)-`，数字即**课程号** | `lesson-01-basics` → number = 1 |
| 题目 | 课次目录下匹配 `test\d+` 的子目录 | `lesson-01-basics/test06` → slug = `test06` |

`get_trees` 一次请求就能拿到全部路径，不需要逐目录探测。同步结果记在 `course_sync`（仓库、ref、时间），供排障。

**为什么必须由模板仓库提供全集**：载荷只上报「本次做了哪些题」，学生删掉某个题目目录后它就不会出现。以模板仓库的题目全集作分母，删题只会被算成「未完成」，不会变成「完成」。

## 3 上报接口

### 3.1 请求

```http
POST /api/v1/reports
Content-Type: application/json
```

```json
{
  "repo_url": "https://github.com/zhangsan/redrock_backend_practice_2026",
  "commit": "9f2c1ab7c3d4e5f60718293a4b5c6d7e8f901234",
  "ref": "refs/heads/main",
  "event": "push",
  "config": { "name": "张三", "lesson": "lesson-03-goroutines" },
  "result": {
    "lesson": "lesson-03-goroutines",
    "tests": [
      { "test": "test01", "status": "pass" },
      { "test": "test02", "status": "pass" },
      { "test": "test03", "status": "fail" }
    ]
  }
}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `repo_url` | 是 | 完整仓库地址；**学生的主键由它解析出的 `owner/name`** |
| `commit` | 是 | 提交 SHA；与 `repo` 组成幂等键 |
| `ref` | 否 | 练习仓库现在只对 `main` 的 push 触发，恒为 `refs/heads/main` |
| `event` | 否 | 恒为 `push`；非 push 的报告应视为异常 |
| `config` | 否 | `{name, lesson}`，即 `config.json` 去掉 `center` 后的内容 |
| `result` | **是** | `{lesson, tests[]}`；缺失则整份报告不可用 |
| `result.lesson` | 是 | 本次统计的课次，必须是 `lessons` 表里存在的 slug |
| `result.tests[]` | 是 | 逐题结论：`{test, status}`，`status` 只有 `pass` / `fail` |

**`status == pass` 的含义**：这道**题的全部测试用例都通过**。子测试已归并到父测试，编译失败会记成一个 `__build__` 失败项——这些都在练习仓库侧处理完了，站点只看结论。

### 3.2 响应

| 状态 | 语义 | 说明 |
|---|---|---|
| 202 | 已接收 | 正常入库 |
| 200 + `{"deduplicated": true}` | 重复 | 同一 `repo` + `commit` 已收过，直接返回，**不重复计数** |
| 400 | `invalid_payload` | JSON 无法解析，或 `repo_url` / `commit` / `result` 缺失 |
| 422 | `unknown_lesson` | `result.lesson` 不在 `lessons` 表里 |

### 3.3 两条硬约束

1. **幂等是必需的**：练习仓库对任何失败都会退避重试 3 次。首次成功但响应丢失时，重试会重复上传。按 `(student_id, commit)` 去重。
2. **4xx 必须零副作用**：校验（能否解析、课次是否合法）必须在**任何写入之前**完成。一次 422 之后库里不能多出半条记录，否则重试会把它放大成脏数据。

## 4 写入流程

```
1. 解析 JSON                       → 失败：400
2. 校验 repo_url / commit / result → 缺失：400
3. 由 repo_url 解析 owner/name     → 得到学生主键
4. 查 lessons 表确认课次存在        → 不存在：422（此前不得写入任何东西）
5. upsert students（name 取 config.name，空则保留原值）
6. insert reports（违反 (student_id, commit) 唯一约束 → 返回 200 deduplicated 并结束）
7. 计算 completed = tests 中 status == pass 的条数
8. upsert student_lessons（覆盖该学生该课次的上一次）
9. 返回 202
```

`student_lessons` 是**每个学生在每一课的最新快照**——练习仓库一次只上报一个课次，同一课次再次推送会覆盖。这正是看板要的「这个人这门课现在的状态」。

## 5 数据模型（Go 结构体）

```go
// ---------- 入站 ----------

type ReportRequest struct {
    RepoURL string `json:"repo_url"`
    Commit  string `json:"commit"`
    Ref     string `json:"ref"`
    Event   string `json:"event"`
    Config  Config `json:"config"`
    Result  Result `json:"result"`
}

type Config struct {
    Name   string `json:"name"`
    Lesson string `json:"lesson"`
}

type Result struct {
    Lesson string      `json:"lesson"`
    Tests  []TestState `json:"tests"`
}

type TestState struct {
    Test   string `json:"test"`   // test01 …
    Status string `json:"status"` // pass | fail
}

// ---------- 存储 ----------

type Lesson struct {          // 来自模板仓库
    ID     int64
    Number int                // 课程号：1、2、3、4
    Slug   string             // lesson-01-basics
    Title  string
}

type Exercise struct {        // 来自模板仓库
    ID       int64
    LessonID int64
    Slug     string           // test01
    Position int
}

type Student struct {         // 主键语义 = repo
    ID        int64
    Repo      string          // owner/name
    Owner     string          // 姓名为空时的兜底
    RepoURL   string
    Name      string          // config.name，每次上报刷新
    UpdatedAt time.Time
}

type Report struct {          // 追加写，只增不改
    ID         int64
    StudentID  int64
    LessonID   int64
    Commit     string
    Ref        string
    Payload    string         // 原始 JSON，留作回溯与将来扩展
    ReceivedAt time.Time
}

type StudentLesson struct {   // 每人每课的最新快照
    StudentID  int64
    LessonID   int64
    Completed  int            // status == pass 的题数
    Commit     string
    ReportedAt time.Time
}

// ---------- 看板输出 ----------

type LessonOverview struct {
    Number    int    `json:"lesson"`     // 课程号
    Slug      string `json:"slug"`
    Title     string `json:"title"`
    Submitted int    `json:"submitted"`  // 去重后的学生数
    Exercises int    `json:"exercises"`  // 题目全集
}

type LeaderboardRow struct {
    Rank      int    `json:"rank"`
    Repo      string `json:"repo"`
    RepoURL   string `json:"repo_url"`
    Name      string `json:"name"`
    Completed int    `json:"completed"`
}
```

## 6 建表（SQLite）

```sql
PRAGMA foreign_keys = ON;

CREATE TABLE lessons (
    id     INTEGER PRIMARY KEY,
    number INTEGER NOT NULL UNIQUE,          -- 课程号
    slug   TEXT    NOT NULL UNIQUE,          -- lesson-01-basics
    title  TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE exercises (
    id        INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    slug      TEXT    NOT NULL,              -- test01
    position  INTEGER NOT NULL,
    UNIQUE (lesson_id, slug)
);

CREATE TABLE students (
    id         INTEGER PRIMARY KEY,
    repo       TEXT NOT NULL UNIQUE,         -- owner/name，聚合主键
    owner      TEXT NOT NULL,                -- 姓名兜底与搜索
    repo_url   TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',     -- config.name，可能为空
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_students_name ON students(name);

CREATE TABLE reports (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    lesson_id   INTEGER NOT NULL REFERENCES lessons(id),
    commit      TEXT    NOT NULL,
    ref         TEXT    NOT NULL DEFAULT '',
    payload     TEXT    NOT NULL,            -- 原始 JSON
    received_at TEXT    NOT NULL,
    UNIQUE (student_id, commit)              -- 幂等
);

CREATE TABLE student_lessons (
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    lesson_id   INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    completed   INTEGER NOT NULL,            -- status == pass 的题数
    commit      TEXT    NOT NULL,
    reported_at TEXT    NOT NULL,
    PRIMARY KEY (student_id, lesson_id)
);
CREATE INDEX idx_sl_lesson ON student_lessons(lesson_id);

CREATE TABLE course_sync (
    id        INTEGER PRIMARY KEY CHECK (id = 1),
    repo      TEXT NOT NULL,
    ref       TEXT NOT NULL,
    synced_at TEXT NOT NULL
);
```

## 7 看板查询

### 7.1 总览：每课提交学生数

```sql
SELECT l.number, l.slug, l.title,
       COUNT(sl.student_id) AS submitted,
       (SELECT COUNT(*) FROM exercises e WHERE e.lesson_id = l.id) AS exercises
FROM lessons l
LEFT JOIN student_lessons sl ON sl.lesson_id = l.id
GROUP BY l.id
ORDER BY l.number;
```

口径是**去重后的学生数**：同一人多次 push 同一课只算一个。`reports` 表里另有原始份数，需要活跃度时再取。

### 7.2 排行榜：按完成题目数

```sql
SELECT COALESCE(NULLIF(s.name, ''), s.owner) AS name,
       s.repo, s.repo_url,
       SUM(sl.completed) AS completed
FROM student_lessons sl
JOIN students s ON s.id = sl.student_id
WHERE (:lesson_id IS NULL OR sl.lesson_id = :lesson_id)   -- 课程号过滤，可空
  AND (:kw = ''                                            -- 关键字搜索，可空
       OR s.name  LIKE '%' || :kw || '%'
       OR s.owner LIKE '%' || :kw || '%'
       OR s.repo  LIKE '%' || :kw || '%')
GROUP BY s.id
ORDER BY completed DESC, s.repo ASC;                       -- 并列按 repo 升序，保证稳定
```

排名的名次在应用层算（并列同名次，下一名跳号或顺延均可，自行定）。

**课程号的解析**：用户输入可能是 `1`、`01`、`lesson-01-basics`。统一处理——纯数字按 `number` 匹配，否则按 `slug` 匹配。

## 8 读接口（供前端消费）

| 接口 | 返回 |
|---|---|
| `GET /api/v1/overview` | 课次数组，按课程号升序：课程号、slug、标题、提交学生数、题目全集数 |
| `GET /api/v1/leaderboard?lesson=01&q=张三` | 排行榜行数组：名次、repo、repo_url、姓名、完成题数 |

两个接口都是公开只读，无需鉴权。

## 9 口径与已知取舍

1. **完成 = 该题全部用例通过**（`status == pass`）。分数不进载荷，站点也算不出分数——这是刻意的：看板只呈现「做完没有」。
2. **分母来自模板仓库**，所以删题目录不会被算成完成。
3. **学生姓名可能为空**（`config.json` 的 `name` 没填）：显示与搜索都用 `repo` 的 owner 兜底。
4. ⚠️ **跨课求和天然偏向题目多的课次**：第一课 6 题，其余课 2~3 题。只做完第一课的人（6 题）可能压过做完其余三课的人（7 题）。若要公平，改成 `SUM(completed) * 100 / 题目全集总数` 的百分比口径——需要时改这一处 SQL 即可。
5. **成绩是自报值**：整套流程不做防作弊，看板按「成绩未经复核」如实标注。
6. `reports` 表保留原始载荷但不参与展示，作用是幂等去重与将来扩展；日志与更细的判定明细留在练习仓库的 artifact 里（保留 14 天）。
