# syntax=docker/dockerfile:1
# 作业统计看板：单镜像交付（前端构建产物 + 后端二进制，SQLite 落数据卷）

# ---------- 阶段一：前端构建 ----------
FROM node:22-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY frontend/ ./
RUN npm run build

# ---------- 阶段二：后端构建（modernc/sqlite 为纯 Go，CGO_ENABLED=0 静态编译） ----------
FROM golang:1.26-alpine AS build
WORKDIR /src/backend
ENV GOPROXY=https://goproxy.cn,direct
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dashboard .

# ---------- 阶段三：运行时 ----------
# ca-certificates：模板仓库同步需要访问 GitHub API（HTTPS）
FROM alpine:3.21
RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 dashboard \
    && mkdir -p /data \
    && chown dashboard /data
COPY --from=build /out/dashboard /app/dashboard
COPY --from=web /src/frontend/dist /srv/web
USER dashboard
EXPOSE 8080
ENTRYPOINT ["/app/dashboard"]
