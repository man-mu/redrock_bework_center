// api.ts —— 与后端读接口对齐（docs/site-data-model.md §8）
// 分离部署时把 BASE 改成站点地址（后端已开 CORS）；同源 / 反代 / 后端托管时留空。
export interface LessonOverview {
  lesson: number; // 课程号
  slug: string;
  title: string;
  submitted: number; // 去重后的学生数
  exercises: number; // 题目全集数
}

export interface LeaderboardRow {
  rank: number;
  repo: string;
  repo_url: string;
  name: string;
  completed: number;
}

const BASE = "";

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(BASE + "/api/v1" + path);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return (await res.json()) as T;
}

export function getOverview(): Promise<LessonOverview[]> {
  return getJSON<LessonOverview[]>("/overview");
}

// lesson 可传课程号（1 / 01）或 slug（lesson-01-basics）；q 搜索姓名 / owner / 仓库
export function getLeaderboard(query: { lesson?: string; q?: string } = {}): Promise<LeaderboardRow[]> {
  const p = new URLSearchParams();
  if (query.lesson && query.lesson !== "all") p.set("lesson", query.lesson);
  if (query.q) p.set("q", query.q);
  const qs = p.toString();
  return getJSON<LeaderboardRow[]>("/leaderboard" + (qs ? `?${qs}` : ""));
}
