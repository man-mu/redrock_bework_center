import { useEffect, useState } from "react";
import Overview from "./views/Overview";
import Ranking from "./views/Ranking";

type View = "overview" | "ranking";

const NAV: { hash: string; label: string; view: View }[] = [
  { hash: "#/overview", label: "总览", view: "overview" },
  { hash: "#/ranking", label: "排行榜", view: "ranking" },
];

function parseHash(): View {
  const v = (window.location.hash || "#/overview").replace("#/", "").split("/")[0];
  return v === "ranking" ? "ranking" : "overview";
}

export default function App() {
  const [view, setView] = useState<View>(parseHash);

  useEffect(() => {
    const onHash = () => {
      setView(parseHash());
      window.scrollTo(0, 0);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <div className="brand">
            <div className="brand-mark">红岩</div>
            <div className="brand-text">
              <div className="brand-name">作业统计看板</div>
              <div className="brand-sub">红岩网校 · Go 后端课程</div>
            </div>
          </div>
          <nav className="nav-pills">
            {NAV.map((n) => (
              <a key={n.view} className={"nav-pill" + (view === n.view ? " active" : "")} href={n.hash}>
                {n.label}
              </a>
            ))}
          </nav>
        </div>
      </header>
      <main className="container">{view === "overview" ? <Overview /> : <Ranking />}</main>
    </>
  );
}
