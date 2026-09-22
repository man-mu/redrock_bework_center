# 注意事项

## 1. 网络问题处理

下载命令遇到网络失败时，先设置代理再执行：

```bash
export http_proxy=http://127.0.0.1:7890
export https_proxy=http://127.0.0.1:7890
```

------

## 2. 本机开发环境（macOS aarch64）

采用分层管理：

- **SDKMAN**：管理 JVM 工具链，包括 JDK、Maven、Tomcat
- **nvm**：管理 Node
- **pyenv**：管理 Python
- **Homebrew**：管理通用工具

### 关键路径

| 工具   | 路径                                  | 说明                                  |
| ------ | ------------------------------------- | ------------------------------------- |
| JDK    | `~/.sdkman/candidates/java/current`   | 默认 `21-tem`                         |
| Maven  | `~/.sdkman/candidates/maven/current`  | `~/.m2/settings.xml` 已配置阿里云镜像 |
| Tomcat | `~/.sdkman/candidates/tomcat/current` | 命令是 `catalina.sh`，不是 `catalina` |
| Go     | `/opt/homebrew/bin/go`                | `GOPATH`：`~/go`                      |

### Shell 初始化顺序

`~/.zshrc` 初始化顺序：

```text
PATH → Oh My Zsh → nvm → pyenv → SDKMAN（必须最后）
```

PATH 优先级：

```text
pyenv > nvm > SDKMAN > 用户 bin > Homebrew > 系统
```

------

## 3. 提交规范

遵循 [Conventional Commits](https://www.conventionalcommits.org/)，格式：

```text
<type>(<scope>): <subject>
```

常用 `type` 如下：

| type       | 说明          |
| ---------- | ------------- |
| `feat`     | 新功能        |
| `fix`      | 修复 bug      |
| `docs`     | 文档变更      |
| `style`    | 代码格式调整  |
| `refactor` | 重构          |
| `perf`     | 性能优化      |
| `test`     | 测试相关      |
| `chore`    | 构建 / 工具链 |
| `ci`       | CI 配置       |

------

## 4. 浏览器操作规范

- 打开公开网页、查阅资料等不依赖登录态的任务：优先使用内置浏览器（如果当前 agent 有内置浏览器）。
- 需要用户账号、登录态或会员身份的操作（如已登录的后台）：使用 `ego-browser`，它复用用户日常浏览器的登录状态。
- 用户明确指定浏览器时，以用户指定为准。

## 5. Docker Compose 规范

1. **每个独立的 Compose 项目，必须在顶层字段中显式声明 `services`、`networks`、`volumes`**；所有服务引用的网络与数据卷，均须在顶层对应字段中预先定义。不得使用未声明的命名资源，也不得以匿名卷或默认网络替代项目级资源声明。
2. **每个项目必须使用独立的项目专属网络，禁止与其他项目共享网络，也不得以默认网络替代项目级网络声明**
3. **凡需占用磁盘资源的服务，必须使用命名清晰、用途明确的命名数据卷**；严禁使用匿名数据卷；生产环境禁止使用绑定挂载承载持久化数据。
4. **本地开发环境下，`restart` 必须显式设置为 `no`**；仅当部署至生产服务器时，才允许将 `restart` 调整为 `always`，且不得在非生产环境中使用该策略。
5. **服务依赖必须通过 `depends_on` 显式声明**，并为被依赖服务配置有效 `healthcheck`，且 `condition` 必须设置为 `service_healthy`，以确保依赖服务应用层就绪后，当前服务方可启动。

