# OpenPage

OpenPage 是面向个人开发者和家庭 NAS 的自托管网页发布服务。通过管理台、HTTP API 或 MCP 编辑 HTML、CSS、JavaScript 和其他静态文件。工作文件可以单独预览，发布时生成历史版本，已打开的正式页面通过 SSE 自动刷新。

## 快速开始

### 拉取预构建镜像

新建 `compose.yaml`：

```yaml
services:
  openpage:
    image: ghcr.io/free2link/open-page:latest
    ports:
      - "${OPENPAGE_HOST_BIND:-127.0.0.1}:3000:3000"
    environment:
      OPENPAGE_URL: "${OPENPAGE_URL:-http://localhost:3000}"
    volumes:
      - "./data:/data"
    read_only: true
    init: false
    restart: unless-stopped
```

数据保存在 compose 文件旁的 `./data`。首次启动前创建目录并交给容器用户：`mkdir -p data && sudo chown 10001:10001 data`。

启动：

```sh
docker compose pull
docker compose up -d
```

拉取预构建镜像不需要 `--build`。首次启动会自动创建并发布默认页面，打开 `http://localhost:3000` 即可看到；管理台在 `/admin/`，新数据目录的初始口令为 `admin`。

### 从源码构建

在仓库根目录执行：

```sh
docker compose up -d --build
```

仓库自带的 `compose.yaml` 使用本地构建（`build: .` 和 `image: openpage:local`）。`--build` 表示强制按当前源码重新构建；只重启服务时用 `docker compose up -d` 即可，不需要 `--build`。

所有入口默认共用 `http://localhost:3000`。

| 入口 | 路径 |
| --- | --- |
| 管理台 | `/admin/` |
| HTTP API | `/api/` |
| MCP | `/mcp` |
| 正式站点 | `/` 和 `/p/{id}/` |
| 草稿预览 | `/d/{id}/{ticket}/`，由授权请求生成 |
| 健康检查 | `/healthz` |

新数据目录的默认管理口令是 `admin`，登录后可在系统设置中修改。如需在首次启动前指定口令，在 compose 中增加 `OPENPAGE_TOKEN` 环境变量（至少 24 个字符）。已有数据目录继续使用原来的管理凭据。HTTP API 和 MCP 将同一凭据作为 Bearer Token 发送。

快速开始使用 `./data` 保存数据，端口默认只发布到本机回环地址。该目录须允许容器用户 `10001:10001` 读写，并支持原子重命名和文件锁。一个数据目录由一个服务实例管理。

需要其他设备直接访问时，在 Compose 的 `.env` 中设置：

```dotenv
OPENPAGE_HOST_BIND=0.0.0.0
OPENPAGE_URL=http://nas.example.lan:3000
```

通过 HTTPS 反向代理时，`OPENPAGE_URL` 应使用对外的 HTTPS 来源，并为 `/events` 关闭代理缓冲。管理台只在当前页面内存中使用口令。修改管理口令时由用户输入并确认新口令。保存后旧口令和已有预览链接失效。正式页面和草稿在浏览器沙箱内执行，仍可加载静态资源、运行 JavaScript 模块和接收 SSE。同源存储（如 localStorage）及 Service Worker 无法在沙箱内使用。

## HTTP API

管理接口默认使用 `Authorization: Bearer <token>`。可在管理台「系统设置」中分别关闭 HTTP API 或 MCP 的口令校验。关闭后，能访问服务的客户端可读写页面和草稿。管理台登录和系统设置仍需要口令。创建页面后，按以下流程写入并发布：

```sh
admin=http://localhost:3000
OPENPAGE_TOKEN=admin # 使用实际管理口令

curl -fsS "$admin/api/pages" -H "Authorization: Bearer $OPENPAGE_TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"dashboard","title":"家庭驾驶舱"}'

curl -fsS "$admin/api/pages/dashboard/files/index.html" -X PUT \
  -H "Authorization: Bearer $OPENPAGE_TOKEN" -H 'If-None-Match: *' \
  --data-binary @index.html

curl -fsS "$admin/api/pages/dashboard/files" -H "Authorization: Bearer $OPENPAGE_TOKEN"

# 将上一步返回的 revision 放入 expected。
curl -fsS "$admin/api/pages/dashboard/publish" -H "Authorization: Bearer $OPENPAGE_TOKEN" \
  -H 'Content-Type: application/json' -d '{"expected":"<revision>","description":"首次发布"}'
```

| 方法与路径 | 行为 |
| --- | --- |
| `GET /api/pages`、`POST /api/pages` | 列表和创建 |
| `GET /api/pages/{id}`、`PUT /api/pages/{id}` | 读取详情，修改 `title` 或用 `private` 控制公开访问 |
| `DELETE /api/pages/{id}?confirm={id}` | 删除页面及历史。请先取消其默认页面设置 |
| `GET /api/pages/{id}/files` | 工作文件列表和草稿修订号 |
| `GET /api/pages/{id}/files/{path}` | 下载源文件，同时返回 ETag |
| `PUT /api/pages/{id}/files/{path}` | 上传原始字节，新建用 `If-None-Match: *`，覆盖用 `If-Match: "<etag>"` |
| `DELETE /api/pages/{id}/files/{path}` | 使用 `If-Match` 删除文件 |
| `POST /api/pages/{id}/preview` | 创建 30 分钟内可用的预览链接 |
| `POST /api/pages/{id}/publish` | 提交 `expected` 草稿修订号及可选的 `description` |
| `GET /api/pages/{id}/versions` | 发布历史 |
| `GET /api/pages/{id}/versions/{version}/files` | 列出历史文件，追加文件路径可下载 |
| `PUT /api/pages/{id}/versions/{version}` | 用 `{"protected":true}` 保护历史版本 |
| `POST /api/pages/{id}/restore` | 用 `version` 指定历史版本并重新发布，保留当前草稿 |
| `POST /api/pages/{id}/reset` | 提交 `version` 和 `expected`，用历史版本替换草稿 |
| `GET /api/settings`、`PUT /api/settings` | 查询设置，修改 `defaultPage`，用 `requireApiAuth` 和 `requireMcpAuth` 配置接口鉴权，或用 `newPassword` 设置自己选定的管理口令 |

发生并发修改时，服务返回 `409`。重新读取相关文件或草稿修订号后，再决定如何合并和重试。预览链接本身授予读取草稿的权限。

## MCP

使用支持 Streamable HTTP（可流式 HTTP）的客户端连接 `http://localhost:3000/mcp`，配置 `Authorization: Bearer <token>`。工具列表可由客户端发现。管理台「系统设置」可直接复制 Codex 或 OpenCode 的配置。开启 MCP 鉴权时，复制内容带有当前管理口令；关闭时仅包含连接参数。

典型发布流程为 `create_page` → `write_file` → `list_files` → `publish_page`。新建文件时传入 `expected: "new"`。覆盖文件时使用 `read_file` 返回的 `etag`。二进制内容使用 `encoding: "base64"`。发布和重置草稿使用 `list_files` 返回的 `revision` 作为 `expected`。

## 文件、版本和运行

- 使用相对资源路径。发布需要根目录下的 `index.html`。文件路径的目录分隔符为 `/`，每段接受 ASCII 字母、数字、短横线、下划线和 `@`，中间也可包含空格和点。
- 每个工作版本最多 64 MiB、4096 个文件，单文件最多 8 MiB。每页保留最近 20 个发布版本，以及受保护的旧版本。发布版本合计上限为 2 GiB、100 个。
- 服务器通过一个原子切换的版本清单为每次请求选择完整快照。一次浏览器加载中的独立资源请求仍可能跨过切换时点，SSE 会在发布后重新加载页面。
- 根路径保留 `admin`、`api`、`mcp`、`events`、`p`、`d`、`healthz` 和 `_openpage`。如网页设置自己的内容安全策略，请允许同源的 `/_openpage/live.js` 和 `/events`。
- 直接运行时，可通过 `OPENPAGE_PORT` 调整监听端口，通过 `OPENPAGE_URL` 设置对外来源。直接运行时，数据目录默认是 `./data`，可通过 `OPENPAGE_DATA_DIR` 设置。
- 发布和草稿元数据保存在数据卷中。应一并备份 `config/` 和 `pages/`。

### 本地开发

```sh
./scripts/vendor-admin.sh
go run .
go test -race ./...
```

本地需要 Go 1.25 或以上。资源脚本仅在开发和容器构建时使用 Node.js/npm。运行镜像使用 `scratch`，由单个 Go 程序提供服务和嵌入的管理台资源。

## 测量部署体积和内存

在运行 Docker 的主机上，可测量本次构建的镜像以及空闲或业务负载下的运行内存：

```sh
docker image inspect openpage:local --format '{{.Size}}'
docker save openpage:local | gzip -c | wc -c
docker stats --no-stream "$(docker compose ps -q openpage)"
```
