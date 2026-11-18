const $ = id => document.getElementById(id);
const state = { token: "", pages: [], settings: {}, page: null, version: "", files: [], file: null, busy: false };
let editor = null;
let editorKey = "";

function editorValue() {
  return editor ? editor.state.doc.toString() : $("source").value;
}

function report(message, error = false) {
  $("status").textContent = message;
  $("status").dataset.error = String(error);
}

async function request(path, { method = "GET", json, body, headers = {}, raw = false } = {}) {
  const options = {
    method, body, cache: "no-store", credentials: "omit", redirect: "error",
    headers: { ...headers, Authorization: "Bearer " + state.token },
  };
  if (json !== undefined) {
    options.body = JSON.stringify(json);
    options.headers["Content-Type"] = "application/json";
  }
  const response = await fetch("/admin/api" + path, options);
  if (!response.ok) {
    const failure = await response.json().catch(() => ({}));
    throw new Error(failure.error || "请求失败（" + response.status + "）");
  }
  return raw ? response : response.json();
}

function action(handler) {
  return async event => {
    event?.preventDefault();
    if (state.busy) return;
    state.busy = true;
    try {
      await handler(event);
    } catch (error) {
      report(error.message, true);
    } finally {
      state.busy = false;
    }
  };
}

function on(id, event, handler) {
  $(id).addEventListener(event, action(handler));
}

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function button(label, handler, active) {
  const node = element("button", label);
  node.type = "button";
  if (active) node.setAttribute("aria-current", "true");
  node.addEventListener("click", action(handler));
  return node;
}

function promptDialog(title, message, fields = [], submit = "确认") {
  const dialog = $("dialog");
  $("dialog-title").textContent = title;
  $("dialog-message").textContent = message;
  $("confirm-dialog").textContent = submit;
  $("dialog-fields").replaceChildren();
  for (const field of fields) {
    const fieldID = "field-" + field.name;
    const label = element("label", field.label);
    label.htmlFor = fieldID;
    const input = element("input");
    input.id = fieldID;
    input.name = field.name;
    input.value = field.value ?? "";
    input.required = field.optional !== true;
    if (field.pattern) input.pattern = field.pattern;
    if (field.maxLength) input.maxLength = field.maxLength;
    $("dialog-fields").append(label, input);
  }
  dialog.returnValue = "";
  dialog.showModal();
  return new Promise(resolve => {
    dialog.addEventListener("close", () => {
      const confirmed = dialog.returnValue === "confirm";
      dialog.returnValue = "";
      resolve(confirmed ? Object.fromEntries(new FormData($("dialog-form"))) : null);
    }, { once: true });
  });
}

const pagePath = (id = state.page.id) => "/pages/" + encodeURIComponent(id);
function encodedPath(path) {
  const segments = path.split("/");
  if (path.length > 512 || segments.some(part =>
    !/^[a-zA-Z0-9_@-][a-zA-Z0-9_.@ -]{0,127}$/.test(part) || /[. ]$/.test(part))) {
    throw new Error("文件路径无效。请使用字母、数字、短横线或下划线，并用 / 分隔目录。");
  }
  return segments.map(encodeURIComponent).join("/");
}
const filesPath = version => pagePath() + (version ? "/versions/" + encodeURIComponent(version) : "") + "/files";

function dirty() {
  return Boolean(state.file && !state.file.version && state.file.text !== null
    && (state.file.tag === "new" || editorValue() !== state.file.text));
}

async function safeToLeave() {
  return !dirty() || Boolean(await promptDialog("放弃未保存的编辑？", "离开后，编辑器中的修改将丢失。", [], "放弃修改"));
}

window.addEventListener("beforeunload", event => {
  if (dirty()) {
    event.preventDefault();
    event.returnValue = true;
  }
});

function listRow(list, child) {
  const row = element("li");
  row.append(child);
  list.append(row);
  return row;
}

function renderPages() {
  const list = $("pages");
  list.replaceChildren();
  const select = $("default-page");
  select.replaceChildren(new Option("未设置", ""));
  for (const p of state.pages) {
    const card = element("button", undefined, "page-card");
    card.type = "button";
    if (p.current) card.classList.add("published");
    if (p.private) card.classList.add("private");
    card.append(element("strong", p.title), element("code", "/p/" + p.id + "/"));
    const status = [p.id === state.settings.defaultPage ? "默认" : "",
      p.current ? (p.private ? "私有 · 已发布" : "已发布") : "草稿"].filter(Boolean).join(" · ");
    card.append(element("span", status, "state"));
    card.addEventListener("click", action(async () => {
      if (p.id === state.page?.id || !await safeToLeave()) return;
      await openPage(p.id);
    }));
    const row = element("li");
    row.append(card);
    list.append(row);
    if (p.current && !p.private) select.append(new Option(p.title, p.id));
  }
  $("wall-empty").hidden = state.pages.length > 0;
  select.value = state.settings.defaultPage || "";
  $("require-api-auth").checked = state.settings.requireApiAuth;
  $("require-mcp-auth").checked = state.settings.requireMcpAuth;
  $("api-token").value = state.settings.apiToken || "";
  $("mcp-copy-help").textContent = state.settings.requireMcpAuth
    ? "复制的配置包含 API 令牌。"
    : "复制的配置只包含连接参数。";
  const info = $("service-info");
  info.replaceChildren();
  for (const [label, value] of [
    ["版本", state.settings.version],
    ["正式站点", state.settings.publicUrl],
    ["MCP 地址", location.origin + "/mcp"],
  ]) info.append(element("dt", label), element("dd", value));
  renderMCPConfig();
}

async function refreshPages() {
  [state.pages, state.settings] = await Promise.all([request("/pages"), request("/settings")]);
  renderPages();
}

function renderFile() {
  const f = state.file;
  const editing = f && !f.version;
  $("file-name").textContent = f?.path || "文件编辑器";
  $("source").hidden = !f || f.text === null;
  $("source").readOnly = !editing;
  $("source").value = f?.text ?? "";
  if (f && f.text !== null) f.text = $("source").value;
  $("file-message").textContent = !f ? "选择或新建文件以开始编辑。"
    : f.text === null ? "此文件无法以文本编辑。可下载文件，或上传同名文件覆盖草稿。"
      : "Ctrl+S 保存，Tab 缩进。";
  $("file-message").hidden = !$("file-message").textContent;
  $("save").hidden = !editing || f.text === null;
  $("delete-file").hidden = !editing || f.tag === "new";
  $("download").hidden = !f || f.tag === "new";
  renderEditor(f);
}

function destroyEditor() {
  if (editor) editor.destroy();
  editor = null;
  editorKey = "";
  $("editor-host").replaceChildren();
}

function renderEditor(f) {
  const host = $("editor-host");
  if (!f || f.text === null || !window.OpenPageEditor) {
    destroyEditor();
    host.hidden = true;
    $("source").hidden = !f || f.text === null;
    return;
  }
  const key = f.path + "\n" + f.version;
  host.hidden = false;
  $("source").hidden = true;
  if (editor && editorKey === key) return;
  destroyEditor();
  editor = window.OpenPageEditor.create(host, {
    doc: f.text,
    path: f.path,
    readOnly: Boolean(f.version),
    onSave: () => { if (!$("save").hidden) $("save").click(); },
  });
  editorKey = key;
}

async function openFile(path) {
  if (state.file?.path === path || !await safeToLeave()) return;
  const response = await request(filesPath(state.version) + "/" + encodedPath(path), { raw: true });
  const bytes = await response.arrayBuffer();
  let text = null;
  try {
    const decoded = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    if (!decoded.includes("\0")) text = decoded;
  } catch {
    // Binary files are offered as downloads.
  }
  state.file = { path, text, version: state.version, tag: response.headers.get("ETag") };
  renderFiles();
  renderFile();
}

function renderFiles() {
  $("files-title").textContent = state.version ? "历史版本文件" : "工作文件";
  $("draft").hidden = !state.version;
  $("new-file").hidden = Boolean(state.version);
  $("choose-upload").hidden = Boolean(state.version);
  const list = $("files");
  list.replaceChildren();
  for (const path of state.files) {
    listRow(list, button(path, () => openFile(path), path === state.file?.path));
  }
}

function renderPage() {
  $("empty").hidden = Boolean(state.page);
  $("page").hidden = !state.page;
  $("settings-page").hidden = true;
  $("back").hidden = !state.page;
  $("more-menu").hidden = true;
  $("more-toggle").setAttribute("aria-expanded", "false");
  if (!state.page) return;
  const p = state.page;
  $("page-id").textContent = p.id;
  $("page-title").textContent = p.title;
  $("page-state").textContent = p.current ? "已发布" : "草稿";
  $("page-state").setAttribute("variant", p.current ? "success" : "neutral");
  $("current").textContent = p.current ? "版本 " + p.current.slice(0, 8) : "";
  $("current").title = p.current || "";
  $("visibility").textContent = p.private ? "开启公开访问" : "关闭公开访问";
  $("public-link").hidden = !p.current || p.private;
  $("public-link").href = p.url;
}

function renderVersions(versions) {
  const list = $("versions");
  list.replaceChildren();
  if (!versions.length) {
    listRow(list, element("span", "暂无发布记录", "muted"));
    return;
  }
  for (const version of versions) {
    const detail = element("div");
    detail.append(element("code", version.id));
    const subtitle = [new Date(version.created).toLocaleString(), version.protected ? "已保护" : "", version.restoredFrom ? "恢复自 " + version.restoredFrom : ""].filter(Boolean);
    detail.append(element("p", subtitle.join(" · "), "muted"));
    if (version.description) detail.append(element("p", version.description));
    const operations = element("div", undefined, "actions");
    operations.append(
      button("查看文件", async () => {
        if (!await safeToLeave()) return;
        await openFiles(version.id);
        $("editor-drawer").hidden = false;
      }),
      button(version.protected ? "取消保护" : "保护", async () => {
        await request(pagePath() + "/versions/" + version.id, { method: "PUT", json: { protected: !version.protected } });
        await refreshHistory();
        report("版本保护状态已更新");
      }),
      button("恢复发布", async () => {
        if (!await promptDialog("恢复发布版本？", version.id + " 的内容会作为新版本发布。", [], "恢复发布")) return;
        await request(pagePath() + "/restore", { method: "POST", json: { version: version.id } });
        await refreshHistory();
        report("历史版本已重新发布");
      }),
      button("重置草稿", async () => {
        const draft = await request(filesPath(""));
        if (!await promptDialog("重置草稿？", "当前工作文件将由所选版本替换，编辑器里未保存的内容也会丢失。", [], "重置草稿")) return;
        await request(pagePath() + "/reset", { method: "POST", json: { version: version.id, expected: draft.revision } });
        await openFiles("");
        await refreshPreview();
        report("草稿已重置");
      }),
    );
    const row = listRow(list, detail);
    row.className = "version";
    row.append(operations);
  }
}

async function refreshPreview() {
  if (!state.page) {
    $("preview-frame").removeAttribute("src");
    return;
  }
  const preview = await request(pagePath() + "/preview", { method: "POST" });
  $("preview-frame").src = preview.url;
}

async function openPage(id) {
  const endpoint = pagePath(id);
  const [page, listing, versions] = await Promise.all([
    request(endpoint), request(endpoint + "/files"), request(endpoint + "/versions"),
  ]);
  state.page = page;
  state.version = "";
  state.files = listing.files;
  state.file = null;
  history.replaceState(null, "", "#/p/" + encodeURIComponent(id));
  renderPages();
  renderPage();
  renderFiles();
  renderFile();
  renderVersions(versions);
  report("");
  try {
    await refreshPreview();
  } catch (error) {
    report(error.message, true);
  }
}

function showWall() {
  state.page = null;
  state.file = null;
  state.version = "";
  state.files = [];
  $("editor-drawer").hidden = true;
  $("history-drawer").hidden = true;
  $("preview-frame").removeAttribute("src");
  history.replaceState(null, "", "#/");
  renderPage();
  renderFiles();
  renderFile();
  renderVersions([]);
  report("");
}

function showSettings() {
  state.page = null;
  state.file = null;
  state.version = "";
  state.files = [];
  $("editor-drawer").hidden = true;
  $("history-drawer").hidden = true;
  $("preview-frame").removeAttribute("src");
  $("empty").hidden = true;
  $("page").hidden = true;
  $("settings-page").hidden = false;
  $("back").hidden = false;
  history.replaceState(null, "", "#/settings");
  renderPages();
  report("");
}

async function route() {
  if (!state.token) return;
  if (location.hash === "#/settings") {
    showSettings();
    return;
  }
  const match = location.hash.match(/^#\/p\/([a-z0-9][a-z0-9-]*)$/);
  if (!match) {
    showWall();
    return;
  }
  try {
    await openPage(match[1]);
  } catch (error) {
    report(error.message, true);
    showWall();
  }
}

async function openFiles(version) {
  state.files = (await request(filesPath(version))).files;
  state.version = version;
  state.file = null;
  renderFiles();
  renderFile();
}

async function refreshFiles() {
  state.files = (await request(filesPath(state.version))).files;
  renderFiles();
}

async function refreshHistory() {
  const [p, versions] = await Promise.all([request(pagePath()), request(pagePath() + "/versions"), refreshPages()]);
  state.page = p;
  renderPage();
  renderVersions(versions);
}

function fileHeaders(tag) {
  return tag === "new" ? { "If-None-Match": "*" } : { "If-Match": tag };
}

on("login-form", "submit", async () => {
  state.token = $("login-token").value.trim();
  try {
    await refreshPages();
  } catch (error) {
    state.token = "";
    throw error;
  }
  $("login-token").value = "";
  $("login").hidden = true;
  $("workspace").hidden = false;
  $("settings-open").hidden = false;
  $("logout").hidden = false;
  await route();
  if (!state.page && $("settings-page").hidden) $("new-page").focus();
  report("已登录");
});
on("logout", "click", async () => {
  if (await safeToLeave()) {
    state.file = null;
    location.reload();
  }
});
function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, char => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[char]);
}

function starterPage(title) {
  const name = escapeHTML(title || "新页面");
  const template = state.settings.starterHtml;
  if (!template) return "<!doctype html><title>" + name + "</title><h1>" + name + "</h1>\n";
  return template
    .replace("<title>OpenPage</title>", "<title>" + name + "</title>")
    .replace("<h1>OpenPage</h1>", "<h1>" + name + "</h1>");
}

function makePageID(title) {
  const base = title.toLowerCase().normalize("NFKD").replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "").slice(0, 40);
  let id = /^[a-z0-9]/.test(base) ? base : "p-" + randomToken().slice(0, 8);
  while (state.pages.some(p => p.id === id)) {
    id = (base || "p").slice(0, 32) + "-" + randomToken().slice(0, 4);
  }
  return id;
}

async function createPage() {
  if (!await safeToLeave()) return;
  const data = await promptDialog("新建页面", "标题必填；页面地址留空会按标题自动生成，创建后不能修改。", [
    { name: "title", label: "标题", maxLength: 120 },
    { name: "id", label: "页面地址（选填）", optional: true, pattern: "[a-z0-9][a-z0-9\\-]{0,62}", maxLength: 63 },
  ], "创建");
  if (!data) return;
  data.title = data.title.trim();
  if (!data.title) throw new Error("请填写标题。");
  data.id = data.id.trim() || makePageID(data.title);
  await request("/pages", { method: "POST", json: data });
  const html = starterPage(data.title);
  const saved = await request(pagePath(data.id) + "/files/index.html", {
    method: "PUT", headers: { "If-None-Match": "*" }, body: html,
  });
  await refreshPages();
  await openPage(data.id);
  state.file = { path: "index.html", text: html, version: "", tag: '"' + saved.etag + '"' };
  renderFiles();
  renderFile();
  $("editor-drawer").hidden = false;
  $("source").focus();
  report("页面已创建，可以直接编辑 index.html");
}
$("new-page").addEventListener("click", action(createPage));
on("rename", "click", async () => {
  const data = await promptDialog("修改标题", "", [
    { name: "title", label: "标题", value: state.page.title, maxLength: 120 },
  ], "保存");
  if (!data) return;
  state.page = await request(pagePath(), { method: "PUT", json: data });
  renderPage();
  await refreshPages();
  report("标题已更新");
});
on("back", "click", async () => {
  if (await safeToLeave()) showWall();
});
on("edit-toggle", "click", async () => {
  if (!state.file && state.files.includes("index.html")) await openFile("index.html");
  $("editor-drawer").hidden = false;
  $("source").focus();
});
$("editor-close").addEventListener("click", action(async () => {
  if (await safeToLeave()) $("editor-drawer").hidden = true;
}));
on("history-open", "click", () => {
  $("history-drawer").hidden = false;
});
$("history-close").addEventListener("click", () => {
  $("history-drawer").hidden = true;
});
on("settings-open", "click", () => {
  showSettings();
});
$("more-toggle").addEventListener("click", event => {
  event.stopPropagation();
  const menu = $("more-menu");
  menu.hidden = !menu.hidden;
  $("more-toggle").setAttribute("aria-expanded", String(!menu.hidden));
});
document.addEventListener("click", event => {
  const menu = $("more-menu");
  if (!menu.hidden && !event.target.closest(".more")) {
    menu.hidden = true;
    $("more-toggle").setAttribute("aria-expanded", "false");
  }
});
on("visibility", "click", async () => {
  const makePrivate = !state.page.private;
  const label = makePrivate ? "关闭公开访问" : "开启公开访问";
  const detail = makePrivate ? "正式地址将停止展示该页面。" : "访客可在正式地址查看已发布的版本。";
  if (!await promptDialog(label + "？", detail, [], label)) return;
  state.page = await request(pagePath(), { method: "PUT", json: { private: makePrivate } });
  renderPage();
  await refreshPages();
  report(makePrivate ? "公开访问已关闭" : "公开访问已开启");
});
on("publish", "click", async () => {
  if (dirty()) throw new Error("请先保存当前文件，再发布草稿。");
  const listing = await request(filesPath(""));
  const published = await request(pagePath() + "/publish", { method: "POST", json: { expected: listing.revision } });
  await refreshHistory();
  report("已发布版本 " + published.id);
});
on("new-file", "click", async () => {
  if (!await safeToLeave()) return;
  const data = await promptDialog("新建工作文件", "文件名可以包含相对目录，例如 assets/style.css。", [
    { name: "path", label: "文件路径", maxLength: 512 },
  ], "新建");
  if (!data) return;
  encodedPath(data.path);
  if (state.files.includes(data.path)) throw new Error("该文件已存在，请从文件列表打开。");
  state.file = { path: data.path, text: "", tag: "new", version: "" };
  renderFiles();
  renderFile();
  $("source").focus();
});
on("save", "click", async () => {
  const file = state.file;
  const text = editorValue();
  const data = await request(filesPath("") + "/" + encodedPath(file.path), {
    method: "PUT", headers: fileHeaders(file.tag), body: text,
  });
  file.tag = '"' + data.etag + '"';
  file.text = text;
  $("delete-file").hidden = false;
  $("download").hidden = false;
  await refreshFiles();
  await refreshPreview();
  report("文件已保存");
});
$("source").addEventListener("keydown", event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
    event.preventDefault();
    if (!$("save").hidden) $("save").click();
    return;
  }
  if (event.key === "Tab") {
    event.preventDefault();
    event.target.setRangeText("  ", event.target.selectionStart, event.target.selectionEnd, "end");
  }
});
on("delete-file", "click", async () => {
  const file = state.file;
  if (!await promptDialog("删除工作文件？", file.path, [], "删除文件")) return;
  await request(filesPath("") + "/" + encodedPath(file.path), { method: "DELETE", headers: fileHeaders(file.tag) });
  state.file = null;
  renderFile();
  await refreshFiles();
  await refreshPreview();
  report("工作文件已删除");
});
on("choose-upload", "click", () => $("upload").click());
on("upload", "change", async () => {
  const [file] = $("upload").files;
  $("upload").value = "";
  if (!file || !await safeToLeave()) return;
  const data = await promptDialog("上传工作文件", "存在同名文件时会替换草稿中的内容。", [
    { name: "path", label: "文件路径", value: file.name, maxLength: 512 },
  ], "上传");
  if (!data) return;
  if (file.size > state.settings.maxFileBytes) throw new Error("该文件超过单文件大小限制。");
  const url = filesPath("") + "/" + encodedPath(data.path);
  let tag = "new";
  if (state.files.includes(data.path)) {
    const existing = await request(url, { method: "HEAD", raw: true });
    tag = existing.headers.get("ETag");
    if (!tag) throw new Error("无法读取现有文件版本。");
  }
  await request(url, { method: "PUT", headers: fileHeaders(tag), body: file });
  state.file = null;
  renderFile();
  await refreshFiles();
  await refreshPreview();
  report("文件已上传");
});
on("download", "click", async () => {
  const f = state.file;
  const response = await request(filesPath(f.version) + "/" + encodedPath(f.path), { raw: true });
  const url = URL.createObjectURL(await response.blob());
  const link = element("a");
  link.href = url;
  link.download = f.path.split("/").at(-1);
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 15000);
});
on("draft", "click", async () => {
  await openFiles("");
  await refreshPreview();
  report("已返回工作版本");
});
on("delete-page", "click", async () => {
  const p = state.page;
  if (!await promptDialog("删除页面及其发布历史？", p.title + " 的工作文件和所有版本将被删除。", [], "删除页面")) return;
  await request(pagePath() + "?confirm=" + encodeURIComponent(p.id), { method: "DELETE" });
  await refreshPages();
  showWall();
  report("页面已删除");
});
function randomToken() {
  const bytes = crypto.getRandomValues(new Uint8Array(24));
  return Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
}

on("generate-token", "click", () => {
  $("api-token").value = randomToken();
  report("已生成新的 API 令牌，保存设置后生效。");
});

on("settings-form", "submit", async () => {
  const data = {
    defaultPage: $("default-page").value,
    requireApiAuth: $("require-api-auth").checked,
    requireMcpAuth: $("require-mcp-auth").checked,
  };
  const apiToken = $("api-token").value.trim();
  if (apiToken && apiToken !== state.settings.apiToken) {
    if (apiToken.length < 24) throw new Error("API 令牌至少 24 个字符。");
    data.newToken = apiToken;
  }
  await request("/settings", { method: "PUT", json: data });
  await refreshPages();
  report("系统设置已保存");
});
on("change-password", "click", () => {
  $("password-form").reset();
  $("password-error").textContent = "";
  $("password-dialog").showModal();
});
$("cancel-password").addEventListener("click", () => $("password-dialog").close());
$("password-dialog").addEventListener("close", () => $("password-form").reset());
$("password-dialog").addEventListener("cancel", event => {
  if ($("save-password").disabled) event.preventDefault();
});
on("password-form", "submit", async () => {
  const password = $("new-password").value;
  const error = $("password-error");
  error.textContent = "";
  if (!/^[ -~]{12,256}$/.test(password) || password.trim() !== password) {
    error.textContent = "请输入 12 至 256 个英文字符，首尾不能有空格。";
    $("new-password").focus();
    return;
  }
  if (password !== $("confirm-password").value) {
    error.textContent = "两次输入的口令不一致。";
    $("confirm-password").focus();
    return;
  }
  const controls = [$("save-password"), $("cancel-password"), $("new-password"), $("confirm-password")];
  controls.forEach(control => { control.disabled = true; });
  try {
    await request("/settings", { method: "PUT", json: { newPassword: password } });
    state.token = password;
    $("password-dialog").close();
    report("登录口令已修改，下次登录请使用新口令；API 和 MCP 令牌不受影响。");
  } catch (failure) {
    error.textContent = failure.message;
  } finally {
    controls.forEach(control => { control.disabled = false; });
    if ($("password-dialog").open) $("save-password").focus();
  }
});


function mcpConfig() {
  const url = location.origin + "/mcp";
  const authorization = state.settings.requireMcpAuth ? "Bearer " + state.settings.apiToken : "";
  const format = $("mcp-format").value;
  if (format === "opencode") {
    const server = { type: "remote", url, oauth: false };
    if (authorization) server.headers = { Authorization: authorization };
    return JSON.stringify({ mcp: { servers: { openpage: server } } }, null, 2) + "\n";
  }
  if (format === "claude") {
    const server = { type: "http", url };
    if (authorization) server.headers = { Authorization: authorization };
    return JSON.stringify({ mcpServers: { openpage: server } }, null, 2) + "\n";
  }
  const lines = ["[mcp_servers.openpage]", "url = " + JSON.stringify(url)];
  if (authorization) lines.push("http_headers = { Authorization = " + JSON.stringify(authorization) + " }");
  return lines.join("\n") + "\n";
}

function renderMCPConfig() {
  const preview = $("mcp-config-preview");
  if (preview) preview.textContent = mcpConfig();
}

for (const tab of document.querySelectorAll(".format-tabs [data-format]")) {
  tab.addEventListener("click", () => {
    $("mcp-format").value = tab.dataset.format;
    for (const other of document.querySelectorAll(".format-tabs [data-format]")) {
      other.setAttribute("aria-selected", String(other === tab));
    }
    renderMCPConfig();
  });
}

on("copy-mcp-config", "click", async () => {
  const config = $("mcp-config-preview").textContent || mcpConfig();
  try {
    await navigator.clipboard.writeText(config);
    report("MCP 配置已复制");
  } catch {
    $("mcp-config-text").value = config;
    $("mcp-config-dialog").showModal();
    $("mcp-config-text").select();
  }
});
$("mcp-config-dialog").addEventListener("close", () => {
  $("mcp-config-text").value = "";
});
window.addEventListener("hashchange", () => {
  if (state.token) route();
});
