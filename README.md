# OpenPage

OpenPage 是面向个人开发者和家庭 NAS 的网页托管服务。除了管理台和 HTTP API，它还提供 MCP：把地址配置给 AI 客户端后，AI 可以直接创建、修改和发布页面，也能继续修改它自己发布过的页面。页面使用 HTML、CSS、JavaScript 和普通静态文件，工作文件可以单独预览，发布时生成历史版本，已打开的正式页面通过 SSE 自动刷新。

## 快速开始

### 拉取预构建镜像

直接运行：

```sh
docker run -d --name openpage --restart unless-stopped \
  -p 3000:3000 \
  -e OPENPAGE_URL=http://192.168.1.10:3000 \
  -v /volume1/docker/openpage/data:/data \
  --read-only \
  ghcr.io/free2link/open-page:latest
```

也可以新建 `compose.yaml`：

```yaml
services:
  openpage:
    image: ghcr.io/free2link/open-page:latest
    ports:
      - "3000:3000"
    environment:
      # 填浏览器访问 NAS 的地址，用于生成页面和预览链接
      OPENPAGE_URL: "http://192.168.1.10:3000"
    volumes:
      - "./data:/data"
    read_only: true
    init: false
    restart: unless-stopped
```

## MCP

MCP 地址为 `http://192.168.1.10:3000/mcp`，使用 Streamable HTTP。新数据目录默认关闭 MCP 鉴权，下面的配置可以直接使用；开启鉴权后，登录管理台「设置」页的「连接 AI」复制带当前令牌的配置。

连接后服务会告诉 AI：页面按目标浏览器选择写法，新浏览器可用 Web Components，目标浏览器较旧或未说明时用普通 HTML/CSS；资源使用相对路径，发布必须有根目录 `index.html`，并把写入、发布所需的 ETag 和 revision 流程一并说明。

OpenCode（`opencode.json`）：

```json
{
  "mcp": {
    "servers": {
      "openpage": {
        "type": "remote",
        "url": "http://192.168.1.10:3000/mcp",
        "oauth": false
      }
    }
  }
}
```

Codex（`~/.codex/config.toml`）：

```toml
[mcp_servers.openpage]
url = "http://192.168.1.10:3000/mcp"
```

Claude Code（项目根目录 `.mcp.json`）：

```json
{
  "mcpServers": {
    "openpage": {
      "type": "http",
      "url": "http://192.168.1.10:3000/mcp"
    }
  }
}
```

开启 MCP 鉴权后，在 OpenCode 和 Claude Code 配置里加 `headers`，在 Codex 配置里加 `http_headers`，值用 API 令牌；管理台「设置」页会直接给出带令牌的完整配置。工具列表可由客户端发现。

典型发布流程为 `create_page` → `write_file` → `list_files` → `publish_page`。新建文件时传入 `expected: "new"`。覆盖文件时使用 `read_file` 返回的 `etag`。二进制内容使用 `encoding: "base64"`。发布和重置草稿使用 `list_files` 返回的 `revision` 作为 `expected`。

启动：

```sh
docker compose pull
docker compose up -d
```

拉取预构建镜像不需要 `--build`。首次启动会自动创建并发布默认页面，浏览器打开 `http://192.168.1.10:3000` 即可看到；管理台在 `/admin/`，新数据目录的初始口令为 `admin`。

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

新数据目录的登录口令和 API 令牌都是 `admin`。首次登录后请在「系统设置」修改登录口令，并按需重新生成 API 令牌：登录口令用于管理台，API 令牌用于 HTTP API 和 MCP。

数据保存在 compose 文件旁的 `./data`，目录不存在时 Docker 会自动创建。容器以 root 运行，文件系统须支持原子重命名和文件锁，一个数据目录由一个服务实例管理。

通过 HTTPS 反向代理时，`OPENPAGE_URL` 应使用对外的 HTTPS 来源，并为 `/events` 关闭代理缓冲。管理台只在当前页面内存中使用登录口令。修改登录口令后，旧口令和已有预览链接失效，API 令牌不受影响。正式页面和草稿在浏览器沙箱内执行，仍可加载静态资源、运行 JavaScript 模块和接收 SSE。同源存储（如 localStorage）及 Service Worker 无法在沙箱内使用。

## HTTP API

新数据目录默认不校验 HTTP API 和 MCP，方便本地 AI 直接接入；可在管理台「设置」里分别开启。开启后使用 `Authorization: Bearer <API 令牌>`。关闭期间，能访问服务的客户端可读写页面和草稿，管理台登录和系统设置仍需要登录口令。创建页面后，按以下流程写入并发布：

```sh
admin=http://localhost:3000
TOKEN=admin # 使用实际 API 令牌

curl -fsS "$admin/api/pages" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"dashboard","title":"家庭驾驶舱"}'

curl -fsS "$admin/api/pages/dashboard/files/index.html" -X PUT \
  -H "Authorization: Bearer $TOKEN" -H 'If-None-Match: *' \
  --data-binary @index.html

curl -fsS "$admin/api/pages/dashboard/files" -H "Authorization: Bearer $TOKEN"

# 将上一步返回的 revision 放入 expected。
curl -fsS "$admin/api/pages/dashboard/publish" -H "Authorization: Bearer $TOKEN" \
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
| `GET /api/settings`、`PUT /api/settings` | 查询设置；修改 `defaultPage`，用 `requireApiAuth` 和 `requireMcpAuth` 配置接口鉴权，用 `newPassword` 改登录口令，用 `newToken` 改 API 令牌 |

发生并发修改时，服务返回 `409`。重新读取相关文件或草稿修订号后，再决定如何合并和重试。预览链接本身授予读取草稿的权限。

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
