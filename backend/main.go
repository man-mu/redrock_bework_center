// 红岩网校作业统计看板后端 —— 应用装配层。
// 数据模型与接口约定见 docs/site-data-model.md；分包：
//
//	internal/db        打开 SQLite 与建表
//	internal/service   业务逻辑（上报写入 / 看板查询 / 模板仓库同步）
//	internal/handler   api 层（请求绑定、状态码映射、CORS）
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"be_homework_center/backend/internal/db"
	"be_homework_center/backend/internal/handler"
	"be_homework_center/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
	var (
		addr      = flag.String("addr", ":8080", "HTTP 监听地址")
		dbPath    = flag.String("db", "center.db", "SQLite 数据库路径")
		repo      = flag.String("repo", "", "模板仓库 owner/name（课次全集来源）")
		ref       = flag.String("ref", "main", "模板仓库分支")
		syncEvery = flag.Duration("sync-every", 30*time.Minute, "模板仓库同步间隔")
		webDir    = flag.String("web", "", "前端构建产物目录；设置后由后端托管 SPA（同源部署）")
	)
	flag.Parse()
	gin.SetMode(gin.ReleaseMode)

	if *repo == "" || !strings.Contains(*repo, "/") {
		log.Printf("[warn] 未配置模板仓库（--repo owner/name）：跳过课次同步，上报将因课次未知而 422")
		*repo = ""
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer database.Close()
	if err := db.EnsureSchema(database); err != nil {
		log.Fatalf("建表失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *repo != "" {
		s := &service.Syncer{DB: database, Repo: *repo, Ref: *ref, Token: os.Getenv("GITHUB_TOKEN")}
		go s.Run(ctx, *syncEvery)
	}

	svc := &service.Service{DB: database}

	g := gin.New()
	g.Use(gin.Logger(), gin.Recovery(), handler.CORS())

	api := g.Group("/api/v1")
	api.GET("/overview", handler.Overview(svc))
	api.GET("/leaderboard", handler.Leaderboard(svc))
	api.POST("/reports", handler.Report(svc))

	g.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	if *webDir != "" {
		mountSPA(g, *webDir)
	}

	srv := &http.Server{Addr: *addr, Handler: g}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("看板监听 %s（课次同步源: %s@%s）", *addr, orDefault(*repo, "未配置"), *ref)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP 服务退出: %v", err)
	}
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// mountSPA 托管前端构建产物；未命中的路径回退 index.html。
func mountSPA(g *gin.Engine, dir string) {
	fs := http.Dir(dir)
	fileServer := http.FileServer(fs)
	g.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		if f, err := fs.Open(c.Request.URL.Path); err == nil {
			f.Close()
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
