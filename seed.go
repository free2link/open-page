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
<p>打开 <code>/admin/</code> 进入管理台，编辑这个页面，或创建新页面并发布。</p>
<p>发布后可以在「系统设置」里把任意页面设为默认页面。</p>
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
