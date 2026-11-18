package main

import "io/fs"

const (
	seedPageID   = "home"
	seedPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>OpenPage</title>
<style>
  body { margin: 0; min-height: 100vh; display: grid; place-items: center;
         font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background: #f6f7fb; color: #1f2430; }
  main { max-width: 34rem; padding: 2rem; }
  h1 { margin: 0 0 .75rem; font-size: 2rem; }
  p { margin: .6rem 0; line-height: 1.7; color: #4b5563; }
  code { padding: .1rem .35rem; border-radius: .3rem; background: #e8ebf2; }
  h2 { margin: 1.6rem 0 .4rem; font-size: 1.1rem; }
  h3 { margin: 1.1rem 0 .3rem; font-size: .95rem; }
  pre { margin: .4rem 0 1rem; padding: .9rem; border-radius: .5rem; overflow: auto;
        background: #12151c; color: #dbe7e1; font: .8rem/1.6 ui-monospace, monospace; white-space: pre; }
  a.button { display: inline-block; margin: .4rem 0 .2rem; padding: .65rem 1rem;
             border-radius: .5rem; background: #125a48; color: #fff; text-decoration: none; }
  @media (prefers-color-scheme: dark) {
    body { background: #12151c; color: #e8eaf0; }
    p { color: #aab0c0; }
    code { background: #1f2430; }
  }
</style>
</head>
<body>
<main>
<h1>OpenPage</h1>
<p>服务已经启动。这是首次运行时自动创建的默认页面。</p>
<p><a class="button" href="/admin/">进入管理台</a></p>
<h2>让 AI 改页面</h2>
<p>默认不需要令牌。把下面任意一份配置粘到 AI 客户端，就能让它修改页面。</p>
<h3>Claude Code</h3>
<pre id="claude-config"></pre>
<h3>Codex</h3>
<pre id="codex-config"></pre>
<h3>OpenCode</h3>
<pre id="opencode-config"></pre>
<p>如果之后在管理台开启了 MCP 鉴权，请到管理台「设置」页的「连接 AI」复制带令牌的配置。</p>
<script>
  var mcpUrl = location.origin + "/mcp";
  document.getElementById("claude-config").textContent = JSON.stringify({
    mcpServers: { openpage: { type: "http", url: mcpUrl } }
  }, null, 2) + "\n";
  document.getElementById("codex-config").textContent =
    "[mcp_servers.openpage]\nurl = " + JSON.stringify(mcpUrl) + "\n";
  document.getElementById("opencode-config").textContent = JSON.stringify({
    mcp: { servers: { openpage: { type: "remote", url: mcpUrl, oauth: false } } }
  }, null, 2) + "\n";
</script>
</main>
</body>
</html>
`
)

// seedDefaultPage publishes a starter page for a brand-new data directory so
// the root URL works immediately after the first deployment.
func (s *Store) seedDefaultPage() error {
	entries, err := fs.ReadDir(s.root.FS(), "pages")
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	p, err := s.create(seedPageID, "首页")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			s.cleanup(pageDir(seedPageID))
		}
	}()
	if _, err := s.editFile(p, "index.html", "new", []byte(seedPageHTML), false); err != nil {
		return err
	}
	revision, err := s.draftRevision(p)
	if err != nil {
		return err
	}
	if _, err := s.changeVersion(p, "publish", "", "初始页面", revision); err != nil {
		return err
	}
	s.settings.DefaultPage = seedPageID
	done = true
	return nil
}
