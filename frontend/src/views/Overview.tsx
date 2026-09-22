import { useEffect, useState } from "react";
import { getLeaderboard, getOverview, type LessonOverview } from "../api";

function StatCard({ label, value, hint }: { label: string; value: number; hint?: string }) {
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      <div className="stat-value">
        {value}
        <span className="unit">{hint}</span>
      </div>
    </div>
  );
}

// 总览：每课提交学生数 + 题目全集（GET /api/v1/overview）
// 「提交过的仓库」复用排行榜全量接口（至少上报一课的仓库数）
export default function Overview() {
  const [lessons, setLessons] = useState<LessonOverview[] | null>(null);
  const [repoCount, setRepoCount] = useState(0);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    Promise.all([getOverview(), getLeaderboard()])
      .then(([rows, board]) => {
        if (!alive) return;
        setLessons(rows);
        setRepoCount(board.length);
      })
      .catch((e: unknown) => {
        if (alive) setError(String(e));
      });
    return () => {
      alive = false;
    };
  }, []);

  if (error) {
    return (
      <div className="empty">
        <div className="empty-title">加载失败</div>
        {error}
      </div>
    );
  }
  if (!lessons) {
    return <div className="empty">加载中…</div>;
  }

  const totalExercises = lessons.reduce((s, r) => s + r.exercises, 0);

  return (
    <>
      <div className="page-head">
        <div>
          <div className="page-title">总览</div>
          <div className="page-desc">只读统计看板 · 成绩为 CI 自报值，未经复核</div>
        </div>
      </div>

      <div className="stat-strip">
        <StatCard label="课次数" value={lessons.length} hint="来自模板仓库" />
        <StatCard label="题目总数" value={totalExercises} hint="四课题目全集" />
        <StatCard label="提交过的仓库" value={repoCount} hint="至少上报一课" />
      </div>

      <div className="card" style={{ marginTop: 0 }}>
        <div className="card-title">课次提交进度</div>
        <div className="card-sub">
          同一人对同一课多次推送只计一次（去重学生数） · 题目全集来自模板仓库，删题只会被算成「未完成」
        </div>
        <div className="lesson-grid">
          {lessons.map((d) => (
            <div className="lesson-card" key={d.slug}>
              <div>
                <span className="lesson-no">第 {d.lesson} 课</span>
              </div>
              <div>
                <div className="lesson-name">{d.title || d.slug}</div>
                <div className="lesson-slug">{d.slug}</div>
              </div>
              <div className="lesson-nums">
                <span className="big">{d.submitted}</span>
                <span className="dim">人提交</span>
              </div>
              <div className="lesson-meta">
                <span>题目全集 {d.exercises} 道</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </>
  );
}
