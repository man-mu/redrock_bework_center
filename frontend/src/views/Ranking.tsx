import { useEffect, useState } from "react";
import { getLeaderboard, getOverview, type LeaderboardRow, type LessonOverview } from "../api";

function ExtIcon() {
  return (
    <svg className="ext-icon" width="12" height="12" viewBox="0 0 12 12" fill="none">
      <path
        d="M4.5 2H2v8h8V7.5M7 1h4v4M11 1 5.5 6.5"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// 排行榜：按完成题数（GET /api/v1/leaderboard?lesson=&q=）
// 课次筛选 + 关键字搜索（防抖 200ms）；名次、并列规则由后端给出
export default function Ranking() {
  const [lessons, setLessons] = useState<LessonOverview[]>([]);
  const [lesson, setLesson] = useState("all");
  const [kw, setKw] = useState("");
  const [rows, setRows] = useState<LeaderboardRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // 课次元数据（编号 / slug / 标题）来自 overview 接口
  useEffect(() => {
    let alive = true;
    getOverview()
      .then((rows) => alive && setLessons(rows))
      .catch((e: unknown) => alive && setError(String(e)));
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    let alive = true;
    const t = setTimeout(() => {
      getLeaderboard({ lesson, q: kw })
        .then((r) => alive && setRows(r))
        .catch((e: unknown) => alive && setError(String(e)));
    }, kw ? 200 : 0);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [lesson, kw]);

  if (error) {
    return (
      <div className="empty">
        <div className="empty-title">加载失败</div>
        {error}
      </div>
    );
  }

  return (
    <>
      <div className="page-head">
        <div>
          <div className="page-title">排行榜</div>
          <div className="page-desc">
            完成 = 该题全部用例通过（自报值） · 总完成为各课相加，题目多的课权重更大 · 并列同名次
          </div>
        </div>
      </div>

      <div className="card" style={{ marginTop: 0 }}>
        <div className="table-toolbar">
          <div className="filter-pills">
            <button
              className={"filter-pill" + (lesson === "all" ? " active" : "")}
              onClick={() => setLesson("all")}
            >
              全部课次
            </button>
            {lessons.map((L) => (
              <button
                key={L.slug}
                className={"filter-pill" + (lesson === L.slug ? " active" : "")}
                onClick={() => setLesson(L.slug)}
              >
                第 {L.lesson} 课 · {L.title || L.slug}
              </button>
            ))}
          </div>
          <input
            className="search-input"
            type="search"
            value={kw}
            onChange={(e) => setKw(e.target.value)}
            placeholder="搜索姓名 / owner / 仓库"
          />
        </div>

        <div className="toolbar-meta" style={{ marginBottom: 12 }}>
          共 {rows?.length ?? 0} 个仓库
        </div>

        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>名次</th>
                <th>姓名</th>
                <th>仓库</th>
                <th>完成题数</th>
              </tr>
            </thead>
            <tbody>
              {rows === null ? (
                <tr>
                  <td colSpan={4}>
                    <div className="empty">加载中…</div>
                  </td>
                </tr>
              ) : rows.length === 0 ? (
                <tr>
                  <td colSpan={4}>
                    <div className="empty">
                      <div className="empty-title">没有匹配的仓库</div>
                      换个关键字或课次试试
                    </div>
                  </td>
                </tr>
              ) : (
                rows.map((e) => (
                  <tr key={e.repo}>
                    <td>
                      <span className={"rank-badge" + (e.rank <= 3 ? ` rank-${e.rank}` : "")}>{e.rank}</span>
                    </td>
                    <td className="cell-name">
                      <div className="nm">{e.name}</div>
                    </td>
                    <td className="cell-repo">
                      <a href={e.repo_url} target="_blank" rel="noopener noreferrer" title="打开 GitHub 仓库">
                        {e.repo}
                        <ExtIcon />
                      </a>
                    </td>
                    <td className="cell-total">{e.completed}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}
