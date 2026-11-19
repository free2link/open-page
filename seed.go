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
  :root {
    --ink: #142c3d; --muted: #526674; --background: #f2f6f8;
    --paper: #fff; --line: #d6e1e7; --soft: #edf5f7; --accent: #076378;
    --navy: #10354b; --ice: #a6e6ec;
    font: 100%/1.6 "Avenir Next", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;
    color: var(--ink); background: var(--background);
  }
  *, *::before, *::after { box-sizing: border-box; }
  body { margin: 0; min-height: 100vh; min-height: 100dvh; }
  a { color: inherit; text-underline-offset: .25em; }
  button, textarea { font: inherit; }
  button { cursor: pointer; }
  :focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
  .masthead :focus-visible, .hero :focus-visible { outline-color: var(--ice); }
  .shell { width: min(68rem, calc(100% - clamp(2rem, 7vw, 6rem))); margin-inline: auto; }
  .landing { color: #fff; background: var(--navy); }
  .masthead {
    min-height: 4.1rem; display: flex; justify-content: space-between; align-items: center;
    gap: .75rem; border-bottom: 1px solid #446374;
  }
  .brand { display: inline-flex; gap: .72rem; align-items: center; font-size: 1.16rem; font-weight: 700; letter-spacing: -.025em; }
  .page-mark {
    position: relative; display: inline-block; flex: none; width: .93rem; height: 1.12rem;
    border: 1.5px solid var(--ice); border-radius: .15rem;
  }
  .page-mark::after {
    content: ""; position: absolute; inset: -.34rem -.34rem .25rem .2rem;
    border-top: 1.5px solid var(--ice); border-right: 1.5px solid var(--ice); border-radius: .15rem;
  }
  .masthead a { color: #e0edf2; font-size: .9rem; }
  .hero {
    display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: clamp(1.75rem, 5vw, 4.5rem); align-items: center; padding-block: clamp(2.5rem, 7vw, 5.4rem);
  }
  h1, h2, p { margin: 0; }
  h1 {
    font-size: clamp(2.6rem, 1.85rem + 4.6vw, 5.8rem);
    font-weight: 700; letter-spacing: -.045em; line-height: 1.04; overflow-wrap: anywhere;
  }
  .hero p { margin: 1.1rem 0 1.55rem; max-width: 28rem; color: #d3e6ed; }
  .button {
    display: inline-flex; justify-content: center; align-items: center; min-height: 2.75rem;
    padding: .55rem 1rem; border: 1px solid transparent; border-radius: 8px; font-weight: 650;
    text-decoration: none;
  }
  .hero .button { color: #10354b; background: var(--ice); }
  .hero .button:hover { background: #d0f5f7; }
  .windows { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); grid-template-rows: repeat(12, minmax(0, 1fr)); aspect-ratio: 16 / 10; }
  .window { display: grid; grid-template-rows: auto 1fr; overflow: hidden; border-radius: .75rem; }
  .draft-window {
    grid-area: 3 / 1 / 13 / 11; color: #c5dce8;
    border: 1px solid #638393; background: #21485c;
  }
  .live-window {
    grid-area: 1 / 3 / 11 / 13; z-index: 1; color: #142c3d;
    background: #eaf5f7; box-shadow: 0 14px 24px rgb(8 28 38 / .26);
  }
  .window-bar {
    display: flex; align-items: center; justify-content: space-between; gap: .6rem;
    min-height: 2.45rem; padding: .32rem .85rem; border-bottom: 1px solid #c8e0e6;
    font-size: .75rem; font-weight: 650;
  }
  .draft-window .window-bar { border-color: #638393; }
  .window-lights { display: inline-flex; gap: .24rem; }
  .window-lights i { width: .33rem; height: .33rem; border-radius: 50%; background: #71939e; }
  .window-art {
    display: grid; grid-template-columns: 1fr .8fr; align-items: center;
    gap: clamp(.55rem, 2vw, 1.4rem); margin: 9%; padding: 8%;
    border-radius: .55rem; background: #fff;
  }
  .paper-lines { display: grid; gap: clamp(.2rem, .8vw, .5rem); }
  .paper-lines i { height: .23rem; border-radius: 1rem; background: #cddfe4; }
  .paper-lines i:first-child { width: 75%; height: .5rem; background: #076378; }
  .paper-lines i:last-child { width: 62%; }
  .paper-image { width: 100%; aspect-ratio: 1.4; border-radius: .35rem; background: #a6e6ec; }
  .connect {
    display: grid; grid-template-columns: minmax(0, .7fr) minmax(0, 1.3fr);
    gap: clamp(1.4rem, 5vw, 4.5rem); padding-block: clamp(2.3rem, 6vw, 4.25rem);
  }
  h2 { font-size: clamp(1.45rem, 1.25rem + 1vw, 2rem); letter-spacing: -.025em; }
  .connect p { margin-top: .65rem; color: var(--muted); }
  .connect a { color: var(--accent); }
  .config { min-width: 0; }
  .config-box { overflow: hidden; border: 1px solid var(--line); border-radius: .85rem; background: var(--paper); }
  .config-bar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: .35rem; padding: .45rem; }
  .client-options { display: flex; flex-wrap: wrap; gap: .15rem; }
  .client-options button, .copy {
    min-height: 2.35rem; border: 1px solid transparent; border-radius: .48rem;
    padding: .3rem .6rem; background: transparent; color: var(--ink); font-size: .85rem;
  }
  .client-options button:hover, .client-options button[aria-pressed="true"], .copy:hover { background: var(--soft); color: var(--accent); }
  .client-options button[aria-pressed="true"] { font-weight: 700; }
  .copy { border-color: var(--line); }
  .config-box :focus-visible { outline-offset: -2px; }
  .config textarea {
    display: block; resize: vertical; width: 100%; min-height: 9rem; padding: .95rem 1rem;
    border: 0; border-radius: 0; color: #e1f3f7; background: #102c3d;
    font: .82rem/1.55 ui-monospace, Menlo, monospace;
  }
  .config-status { font-size: .84rem; }
  .config-status:empty { margin: 0; }
  @media (min-width: 56rem) and (min-aspect-ratio: 4/3) {
    body { display: flex; flex-direction: column; }
    .masthead { width: min(83rem, calc(100% - 3rem)); }
    .home-layout {
      display: grid; flex: 1;
      grid-template-columns: minmax(0, 1.05fr) minmax(0, 1fr);
      font-size: clamp(1rem, .85rem + .3vw, 1.25rem);
    }
    .home-layout > .landing { display: grid; align-items: center; }
    .hero, .connect {
      width: min(100%, 44rem); grid-template-columns: minmax(0, 1fr); align-content: center;
      gap: clamp(1rem, 2.3vh, 2rem);
      padding: clamp(1.5rem, 4vh, 3.5rem) clamp(1.5rem, 4vw, 4.5rem);
    }
    .hero { margin-inline: auto 0; }
    .hero p { margin-block: .85rem 1.2rem; }
    .windows {
      width: min(100%, 25rem, 36vh);
      width: min(100%, 25rem, 36dvh);
    }
    .connect { margin-inline: 0 auto; }
    .config textarea { font-size: clamp(.82rem, .62rem + .28vw, 1rem); }
    :focus-visible { outline-width: 3px; }
  }
  @media (min-width: 56rem) and (min-aspect-ratio: 4/3) and (max-height: 40rem) {
    .hero, .connect { padding-block: 1rem; }
  }
  @media (min-width: 56rem) and (min-aspect-ratio: 4/3) and (max-height: 34rem) {
    .windows { display: none; }
  }
  @media (max-width: 48rem) {
    .connect { grid-template-columns: minmax(0, 1fr); }
  }
  @media (max-width: 36rem) {
    .hero { grid-template-columns: minmax(0, 1fr); }
    .windows { width: min(100%, 26rem); }
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --ink: #e5f1f5; --muted: #bdcfd7; --background: #10232f;
      --paper: #173346; --line: #3c596a; --soft: #244657; --accent: #a6e6ec;
    }
  }
</style>
</head>
<body>
<header class="landing">
  <div class="shell masthead">
    <span class="brand"><span class="page-mark" aria-hidden="true"></span>OpenPage</span>
    <a href="#connect">连接 AI</a>
  </div>
</header>
<main class="home-layout">
  <section class="landing" aria-labelledby="home-title">
    <div class="shell hero">
      <div>
        <div id="home-title"><h1>OpenPage</h1></div>
        <p>上传 HTML，预览草稿，再发布为可访问的网页。你也可以连接 AI 来修改页面。</p>
        <a class="button" href="/admin/">进入管理台</a>
      </div>
      <div class="windows" aria-hidden="true">
        <div class="window draft-window"><div class="window-bar">草稿</div></div>
        <div class="window live-window">
          <div class="window-bar"><span class="window-lights"><i></i><i></i><i></i></span>已发布</div>
          <div class="window-art">
            <span class="paper-lines"><i></i><i></i><i></i></span>
            <span class="paper-image"></span>
          </div>
        </div>
      </div>
    </div>
  </section>
  <section id="connect" class="shell connect" aria-labelledby="connect-title">
    <div>
      <h2 id="connect-title">连接 AI</h2>
      <p>选择你的客户端，把配置粘贴到它的 MCP 设置中。</p>
      <p>如果已开启 MCP 鉴权，请在 <a href="/admin/#/settings">管理台设置</a>中复制带令牌的配置。</p>
    </div>
    <div class="config">
      <div class="config-box">
        <div class="config-bar">
          <div class="client-options" role="group" aria-label="AI 客户端">
            <button type="button" data-client="claude" aria-pressed="true">Claude Code</button>
            <button type="button" data-client="codex" aria-pressed="false">Codex</button>
            <button type="button" data-client="opencode" aria-pressed="false">OpenCode</button>
          </div>
          <button class="copy" id="copy-config" type="button">复制配置</button>
        </div>
        <textarea id="client-config" aria-label="MCP 配置" rows="11" readonly spellcheck="false"></textarea>
      </div>
      <p id="config-status" class="config-status" role="status"></p>
      <noscript><p>启用 JavaScript 后可按当前站点地址生成配置。</p></noscript>
    </div>
  </section>
</main>
<script>
  const mcpUrl = location.origin + "/mcp";
  const configs = {
    claude: JSON.stringify({
      mcpServers: { openpage: { type: "http", url: mcpUrl } }
    }, null, 2),
    codex: "[mcp_servers.openpage]\nurl = " + JSON.stringify(mcpUrl),
    opencode: JSON.stringify({
      mcp: { servers: { openpage: { type: "remote", url: mcpUrl, oauth: false } } }
    }, null, 2)
  };
  const field = document.getElementById("client-config");
  const status = document.getElementById("config-status");
  const clients = document.querySelectorAll("[data-client]");
  function showConfig(client) {
    field.value = configs[client] + "\n";
    field.scrollTop = 0;
    field.scrollLeft = 0;
    status.textContent = "";
    for (const button of clients) {
      button.setAttribute("aria-pressed", String(button.dataset.client === client));
    }
  }
  for (const button of clients) {
    button.addEventListener("click", () => showConfig(button.dataset.client));
  }
  document.getElementById("copy-config").addEventListener("click", async () => {
    const value = field.value;
    try {
      await navigator.clipboard.writeText(value);
      if (field.value === value) status.textContent = "配置已复制";
    } catch {
      if (field.value !== value) return;
      field.focus();
      field.select();
      status.textContent = "配置已选中，请使用浏览器或系统的复制操作。";
    }
  });
  showConfig("claude");
</script>
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
