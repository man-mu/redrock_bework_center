# 中心站点 API 文档

练习仓库（学生用模板创建）与中心站点之间的 HTTP 接口契约。本文件是**练习仓库视角**的接口文档：练习仓库的 CI 产生报告并上报，中心站点校验、接收、聚合、展示。站点的内部实现（数据模型、建表 SQL、Go 结构体）见同目录的 [site-data-model.md](site-data-model.md)。

- 版本：`v1`
- 基础路径：`http://<center-site>/api/v1`（当前部署：`http://43.139.43.212:18080`）
- 内容类型：`application/json`
- 时间格式：RFC 3339 UTC，例如 `2026-09-22T07:54:02Z`

---

## 1. 定位与边界

| 项目 | 职责 |
| --- | --- |
| 练习仓库 CI | 运行 Go 测试，产出报告，上传到中心站点 |
| 中心站点 | 校验、接收、存储、聚合、展示报告 |

两点边界：

1. **站点不执行任何学生代码，也不做复算。** 报告里的结论由练习仓库的 CI 产生，站点只做校验与接收。
2. **站点不做防作弊设计。** 学生对自己的仓库有管理权限，能改测试与 CI，所以报告上的数字是**自报值**。站点只挡「格式上无法归类的上报」（见 §4.3），不做可信性判定；看板如实标注「成绩未经复核」。

由此站点侧不需要 Go 工具链、不需要容器或沙箱、也不需要任务队列。

---

## 2. 站点侧的配置

| 配置项 | 说明 |
| --- | --- |
| `--repo owner/name` | 模板仓库。**课次全集与题目全集的唯一来源**，站点从中定时同步（启动即同步一次，默认每 30 分钟刷新）。留空则看板正常服务，但任何上报都会因课次未知被 422 拒绝 |
| `--ref` | 模板仓库分支，默认 `main` |
| `--db` | SQLite 数据库路径（单文件，单写者） |
| `--web` | 前端构建产物目录；设置后由后端同源托管 SPA |
| `--addr` | HTTP 监听地址，默认 `:8080`（容器部署映射到宿主 18080） |
| `--oidc-aud <audience>` | 上报鉴权开关。**非空时** `POST /api/v1/reports` 仅接受 GitHub Actions 签发的 OIDC 令牌（`Authorization: Bearer`），audience 必须与此值一致；留空则上报不鉴权（本地调试 / 灰度期用）。部署侧经 `OIDC_AUD` 注入，约定值 `homework-center` |
| `GITHUB_TOKEN` | 可选。提升模板仓库同步的 GitHub API 限额（60 → 5000 req/h） |

课次与题目全集的识别规则：

| 实体 | 规则 | 例 |
|---|---|---|
| 课次 | 顶层目录匹配 `lesson-(\d+)-(.*)`，数字即课程号，其余是标题 | `lesson-01-basics` → number=1, title=basics |
| 题目 | 课次目录下匹配 `test\d+` 的子目录 | `lesson-01-basics/test06` → slug=`test06` |

**为什么必须由模板仓库提供题目全集**：载荷只上报「本次哪些题通过」，学生删掉题目目录后它就不会出现。以模板仓库的题目全集作分母，删题只会被算成「未完成」，不会变成「完成」。

---

## 3. 端点总览

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| POST | `/api/v1/reports` | Bearer OIDC 令牌（启用 `--oidc-aud` 后必填） | 上传一次报告 |
| GET | `/api/v1/overview` | 无 | 每课提交学生数 + 题目全集 |
| GET | `/api/v1/leaderboard` | 无 | 按完成题数排行（支持课次过滤与关键字搜索） |
| GET | `/healthz` | 无 | 健康检查 |

读接口与健康检查**开放无鉴权**。上报接口默认开放；站点启用 `--oidc-aud` 后，只接受 GitHub Actions 的 OIDC 令牌（见 §4.1 鉴权）。响应带 `Access-Control-Allow-Origin: *`（允许方法 `GET, POST, OPTIONS`，允许头 `Content-Type, Authorization`），方便前端分离部署调试。

健康检查：

```
GET /healthz
```

```json
{ "status": "ok" }
```

---

## 4. 上传报告

### 4.1 请求

```
POST /api/v1/reports
Authorization: Bearer <GitHub Actions OIDC 令牌>
Content-Type: application/json
```

#### 鉴权（站点启用 `--oidc-aud` 后必填）

站点只接受 **GitHub 在真实 workflow run 内签发的 OIDC ID Token**（短时效 JWT，私钥始终在 GitHub 手里，无法伪造）：

- 练习仓库 workflow 声明 `permissions: id-token: write` 后，CI 用 runner 预置的 `ACTIONS_ID_TOKEN_REQUEST_URL` / `ACTIONS_ID_TOKEN_REQUEST_TOKEN` 取回令牌（取法见 §6）；
- 站点用 GitHub 的 JWKS 公钥（`https://token.actions.githubusercontent.com/.well-known/jwks`）验签，并校验 `iss`、`aud`（= `--oidc-aud`）、`exp`；
- **身份绑定**（强校验，防拿别处令牌冒名）：
  - 令牌 `repository` 声明必须与载荷 `repo_url` 解析出的 `owner/name` 一致；
  - 令牌 `sha` 声明必须与载荷 `commit` 一致。

由此得到防伪闭环：令牌只能由 GitHub 在真实 run 里签发；学生只能为自己的仓库、自己那次 push 的 commit 上报。想刷分只能真跑 Actions，而真跑就等于真做题。

未启用 `--oidc-aud` 时（本地调试 / 灰度期），本节全部跳过，接口行为与旧版一致。

```json
{
  "repo_url": "https://github.com/man-mu/redrock_backend_practice_2026",
  "commit": "47f4160",
  "ref": "refs/heads/main",
  "event": "push",
  "config": { "name": "测试-七实", "lesson": "lesson-01-basics" },
  "result": {
    "lesson": "lesson-01-basics",
    "tests": [
      { "test": "test01", "status": "pass" },
      { "test": "test02", "status": "pass" },
      { "test": "test03", "status": "fail" }
    ]
  }
}
```

| 字段 | 必填 | 校验 |
| --- | --- | --- |
| `repo_url` | 是 | 必须能解析出 GitHub `owner/name`（支持 `https://github.com/owner/name` 与 `git@github.com:owner/name.git` 两种形状）；解析结果是学生的聚合主键 |
| `commit` | 是 | 非空字符串；与 `repo` 组成幂等键 |
| `result` | **是** | `result.lesson` 与 `result.tests` 都必填（`tests` 可以是空数组，但**字段必须存在**） |
| `result.lesson` | 是 | 非空，且必须是站点 `lessons` 表里存在的课次 slug |
| `result.tests[]` | 是 | 逐题结论：`{test, status}`；`status` 只允许 `pass` / `fail` |
| `ref` | 否 | 归档用；练习仓库当前恒为 `refs/heads/main` |
| `event` | 否 | 站点只接受 `push`（或不传）；其他值 400 |
| `config` | 否 | `{name, lesson}`，即 `config.json` 去掉 `center` 后的内容；`name` 用于看板显示，空则回落显示 `owner` |

**`status == pass` 的含义**：这道**题的全部测试用例都通过**。子测试归并到父测试，一道题编译失败记成一个 `__build__` 失败项——这些都在练习仓库侧处理完，站点只看结论。

**仓库标识**：站点从 `repo_url` 解析 `owner/name` 作为唯一标识与聚合主键，主机名不参与（假定只服务单一 Git 主机）。

**课次判定**：一次上报归属哪一课，取 `result.lesson`，由站点用**自己的**课次表判定，不采信 `config.lesson`——课次名源于学生仓库的目录名，学生可以造假目录通过本地校验。

### 4.2 响应

成功 `202 Accepted`：

```json
{ "status": "accepted", "completed": 2 }
```

- `completed` 是 `tests` 里 `status == "pass"` 的条数，即本次该课次的完成题数。

幂等重复 `200 OK`：

```json
{ "deduplicated": true }
```

- 同一 `repo` + `commit` 重复上传不重复入库（按 `(student_id, commit)` 唯一约束去重）。练习仓库的退避重试在「首次其实成功、只是响应丢失」时也会走到这里，属正常现象。

### 4.3 错误响应

| HTTP | 响应体 | 触发条件 |
| --- | --- | --- |
| 400 | `{"error": "invalid_payload", "detail": "..."}` | JSON 无法解析；`repo_url` / `commit` / `result` / `result.tests` 缺失；`repo_url` 解析不出 `owner/name`；`event` 不是 `push`；`tests[].status` 不是 `pass` / `fail` |
| 401 | `{"error": "invalid_token", "detail": "..."}` | 启用鉴权后：缺少 `Authorization: Bearer`；令牌不是 GitHub 签发的有效 OIDC 令牌（验签失败 / `iss`、`aud`、`exp` 不符） |
| 403 | `{"error": "repository_mismatch", "detail": "..."}` / `{"error": "commit_mismatch", "detail": "..."}` | 令牌 `repository` / `sha` 声明与载荷 `repo_url` / `commit` 不一致（身份与载荷绑定失败） |
| 422 | `{"error": "unknown_lesson", "lesson": "..."}` | `result.lesson` 不在站点的课次表里（含课次为空、站点未配置模板仓库） |
| 500 | `{"error": "internal"}` | 站点内部错误 |
| 404 | `{"error": "not_found"}` | 未注册的 API 路径（站点同源托管 SPA 时） |

四条规则：

- **4xx 零副作用**：全部校验（令牌绑定、能否解析、课次是否合法）在任何写入之前完成，被拒的上报不会留下半条记录；
- **422 是永久性失败**——改对载荷或等站点同步到新课次后重新 push 才有意义，重试同一份载荷没有用；
- **上传失败不得影响练习仓库的流水线结论**：调用方把非 2xx、超时、连接失败都视为「未送达」。

### 4.4 写入后的口径

- `completed = tests` 中 `pass` 的条数，写入该学生该课次的**快照**（同课次再次上报覆盖上一次，不做取最大值之类的处理）；
- 总览的「提交学生数」按学生去重，同一人多次 push 同一课只算一个；
- 排行榜的「完成题数」是学生各课次快照完成数的和。

---

## 5. 看板读取接口

### 5.1 每课总览

```
GET /api/v1/overview
```

响应 `200`（数组，按课程号升序）：

```json
[
  { "lesson": 1, "slug": "lesson-01-basics", "title": "basics", "submitted": 11, "exercises": 8 },
  { "lesson": 2, "slug": "lesson-02-collections", "title": "collections", "submitted": 10, "exercises": 2 }
]
```

| 字段 | 说明 |
| --- | --- |
| `lesson` | 课程号（来自课次目录名中的数字） |
| `slug` / `title` | 课次目录名 / 标题（目录名去掉 `lesson-NN-` 前缀的部分） |
| `submitted` | 去重后的提交学生数 |
| `exercises` | 题目全集数（来自模板仓库同步） |

### 5.2 排行榜

```
GET /api/v1/leaderboard?lesson=&q=
```

| 查询参数 | 默认 | 说明 |
| --- | --- | --- |
| `lesson` | 全部课次 | 课次过滤；可传课程号（`1` / `01`）或课次 slug（`lesson-01-basics`），也可传 `all`；未知课次返回空数组 |
| `q` | 空 | 关键字模糊匹配姓名 / owner / 仓库名；LIKE 通配符已转义 |

响应 `200`（数组，完成题数降序）：

```json
[
  { "rank": 1, "repo": "man-mu/redrock_backend_practice_2026", "repo_url": "https://github.com/man-mu/redrock_backend_practice_2026", "name": "测试-七实", "completed": 8 },
  { "rank": 2, "repo": "wangwu/redrock_backend_practice_2026", "repo_url": "https://github.com/wangwu/redrock_backend_practice_2026", "name": "王五", "completed": 7 }
]
```

- `completed`：学生完成题数（带 `lesson` 过滤时是该课次的快照值，否则是各课次之和）；
- 并列完成数**同名次**，名次密集顺延（8, 8, 7 → 1, 1, 2）；并列内按 `repo` 升序保证顺序稳定；
- `name` 优先取 `config.name`，为空回落 `owner`；
- 只列出有上报记录的仓库。

---

## 6. 与练习仓库的契约

练习仓库的 `.github/workflows/grade.yml` 只做步骤编排，实际逻辑在 `.github/scripts/` 下的三个脚本里：

| 脚本 | 职责 | 产出 |
| --- | --- | --- |
| `.github/scripts/check_config.py` | 校验 `config.json`、解析 `lesson` 与 `center` | 写入 `LESSON_PATH`、`TEST_ROOT`、`CENTER_API` 环境变量 |
| `.github/scripts/run_go_tests.py` | 按 `--root` 课次逐包执行 `go test -json` 并计分 | `result.json`、`score.md`、`test-results/` |
| `.github/scripts/notify_center.py` | 组装并上传报告 | 调用 `POST /api/v1/reports` |

站点可以依赖的行为：

- **触发**：只有默认分支 `main` 的 `push` 触发流水线，Pull Request 与其他分支不跑，所以 `event` 恒为 `push`、`ref` 恒为 `refs/heads/main`；
- **身份令牌**（站点启用 `--oidc-aud` 后必需）：
  - workflow（或上报所在 job）声明 `permissions: id-token: write`；
  - `notify_center.py` 上报前用 runner 预置环境变量取回令牌，并以 `Authorization: Bearer <token>` 头随报告一起发送：

    ```python
    import json, os, urllib.request

    url = os.environ["ACTIONS_ID_TOKEN_REQUEST_URL"] + "&audience=homework-center"
    req = urllib.request.Request(url, headers={
        "Authorization": "Bearer " + os.environ["ACTIONS_ID_TOKEN_REQUEST_TOKEN"]})
    token = json.load(urllib.request.urlopen(req))["value"]
    ```

  - audience 必须与站点 `--oidc-aud` 一致（约定 `homework-center`）；
  - 令牌时效约 5 分钟，取回后立即使用，不要缓存到跨 job；
- **站点地址**来自学生仓库根目录 `config.json` 的 `center`，由 `check_config.py` 解析后写进 `CENTER_API`；缺失时练习仓库跳过上传（因此**模板仓库必须在发布前预置真实的 center 地址**，否则学生的报告无处可去）；
- **载荷**：`config` 是 `config.json` 去掉 `center` 后的内容；`result` 由 `result.json` 压缩而来，只留课次与逐题结论；
- **重试**：上传失败最多退避重试 3 次（间隔 1s / 3s），对 4xx 也会重试——无害但略浪费（站点对 4xx 零副作用）；401 时应先重新取一枚令牌再重试；
- **成绩是尽力而为的**：上传成败不影响流水线结论，完整报告随 artifact `go-test-<sha>` 留存，可事后补取。

模板仓库（练习仓库）侧需同步完成 OIDC 改造：workflow 声明 `id-token: write`、`notify_center.py` 取令牌并携带 `Authorization` 头、401 时刷新令牌重试——改造规格文档由练习仓库维护，不在本仓库存档。

---

## 7. 展示口径

因为报告是自报值，看板上如实标注，避免误读：

- 页面上写明「成绩来自学生仓库的自测报告，未经复核」；
- 站点只呈现「做完没有」（`pass` / `fail`），不呈现分数——分数不进载荷是刻意的设计；
- 「未上报」与「上报了但题目没做完」是两种状态，看板上分开显示；
- 完成数是**各课快照的和**，跨课直接相加天然偏向题目多的课次（第一课 8 题，其余课 2~3 题），看板按课次过滤展示即可避免误读，需要百分比口径时改排行榜 SQL 一处即可。
