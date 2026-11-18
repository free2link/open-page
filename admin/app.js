const $ = id => document.getElementById(id);
const state = { token: "", pages: [], settings: {}, page: null, version: "", files: [], file: null, busy: false };

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
  const response = await fetch("/api" + path, options);
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
    && (state.file.tag === "new" || $("source").value !== state.file.text));
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
    listRow(list, button(p.title, async () => {
      if (p.id === state.page?.id || !await safeToLeave()) return;
      await openPage(p.id);
    }, p.id === state.page?.id));
    if (p.current && !p.private) select.append(new Option(p.title, p.id));
  }
  select.value = state.settings.defaultPage || "";
  const info = $("service-info");
  info.replaceChildren();
  for (const [label, value] of [
    ["版本", state.settings.version],
    ["正式站点", state.settings.publicUrl],
    ["MCP 地址", state.settings.adminUrl + "/mcp"],
    ["访问权限", "页面可单独设置公开访问。管理操作使用访问令牌。预览链接有效期为 30 分钟。"],
  ]) info.append(element("dt", label), element("dd", value));
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
    : f.text === null ? "此文件无法以文本编辑。可下载文件，或上传同名文件覆盖草稿。" : "";
  $("file-message").hidden = !$("file-message").textContent;
  $("save").hidden = !editing || f.text === null;
  $("delete-file").hidden = !editing || f.tag === "new";
  $("download").hidden = !f || f.tag === "new";
}

function renderFiles() {
  $("files-title").textContent = state.version ? "历史版本文件" : "工作文件";
  $("draft").hidden = !state.version;
  $("new-file").hidden = Boolean(state.version);
  $("choose-upload").hidden = Boolean(state.version);
  const list = $("files");
  list.replaceChildren();
  for (const path of state.files) {
    listRow(list, button(path, async () => {
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
    }, path === state.file?.path));
  }
}

function renderPage() {
  $("empty").hidden = Boolean(state.page);
  $("page").hidden = !state.page;
  if (!state.page) return;
  const p = state.page;
  $("page-id").textContent = p.id;
  $("page-title").textContent = p.title;
  $("page-state").textContent = p.current ? "已发布" : "草稿";
  $("page-state").setAttribute("variant", p.current ? "success" : "neutral");
  $("current").textContent = p.current ? "版本 " + p.current : "";
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
        if (await safeToLeave()) await openFiles(version.id);
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
        report("草稿已重置");
      }),
    );
    const row = listRow(list, detail);
    row.className = "version";
    row.append(operations);
  }
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
  $("preview-result").hidden = true;
  renderPages();
  renderPage();
  renderFiles();
  renderFile();
  renderVersions(versions);
  report("");
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
  $("logout").hidden = false;
  $("new-page").focus();
  report("已登录");
});
on("logout", "click", async () => {
  if (await safeToLeave()) {
    state.file = null;
    location.reload();
  }
});
on("new-page", "click", async () => {
  if (!await safeToLeave()) return;
  const data = await promptDialog("新建页面", "页面 ID 用于访问地址，创建后固定。", [
    { name: "id", label: "页面 ID（小写字母、数字和短横线）", pattern: "[a-z0-9][a-z0-9\\-]{0,62}", maxLength: 63 },
    { name: "title", label: "标题", maxLength: 120 },
  ], "创建");
  if (!data) return;
  await request("/pages", { method: "POST", json: data });
  await refreshPages();
  await openPage(data.id);
  report("页面已创建");
});
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
on("preview", "click", async () => {
  const preview = await request(pagePath() + "/preview", { method: "POST" });
  $("preview-link").href = preview.url;
  $("preview-result").hidden = false;
  $("preview-link").focus();
  report("预览链接已生成，显示已保存的工作文件");
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
  const data = await promptDialog("发布工作版本", "发布后，正在显示页面的浏览器会自动更新。", [
    { name: "description", label: "版本描述（选填）", optional: true, maxLength: 300 },
  ], "发布");
  if (!data) return;
  const published = await request(pagePath() + "/publish", { method: "POST", json: { ...data, expected: listing.revision } });
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
  const text = $("source").value;
  const data = await request(filesPath("") + "/" + encodedPath(file.path), {
    method: "PUT", headers: fileHeaders(file.tag), body: text,
  });
  file.tag = '"' + data.etag + '"';
  file.text = text;
  $("delete-file").hidden = false;
  $("download").hidden = false;
  await refreshFiles();
  report("文件已保存");
});
on("delete-file", "click", async () => {
  const file = state.file;
  if (!await promptDialog("删除工作文件？", file.path, [], "删除文件")) return;
  await request(filesPath("") + "/" + encodedPath(file.path), { method: "DELETE", headers: fileHeaders(file.tag) });
  state.file = null;
  renderFile();
  await refreshFiles();
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
  report("已返回工作版本");
});
on("delete-page", "click", async () => {
  const p = state.page;
  if (!await promptDialog("删除页面及其发布历史？", p.title + " 的工作文件和所有版本将被删除。", [], "删除页面")) return;
  await request(pagePath() + "?confirm=" + encodeURIComponent(p.id), { method: "DELETE" });
  state.page = null;
  state.file = null;
  renderPage();
  await refreshPages();
  report("页面已删除");
});
on("settings-form", "submit", async () => {
  await request("/settings", { method: "PUT", json: { defaultPage: $("default-page").value } });
  await refreshPages();
  report("默认页面设置已保存");
});
on("rotate", "click", async () => {
  if (!await promptDialog("更新访问令牌？", "现有令牌和预览链接会立即失效。新令牌仅在更新后显示，请复制留存。", [], "更新令牌")) return;
  const result = await request("/settings", { method: "PUT", json: { rotateToken: true } });
  state.token = result.token;
  $("dialog-title").textContent = "复制新访问令牌";
  $("dialog-message").textContent = "";
  $("dialog-fields").replaceChildren();
  const secret = element("input");
  secret.setAttribute("aria-label", "新访问令牌");
  secret.readOnly = true;
  secret.value = result.token;
  $("dialog-fields").append(secret);
  $("cancel-dialog").hidden = true;
  $("confirm-dialog").textContent = "完成";
  const dialog = $("dialog");
  dialog.showModal();
  secret.focus();
  secret.select();
  await new Promise(resolve => dialog.addEventListener("close", resolve, { once: true }));
  $("dialog-fields").replaceChildren();
  $("cancel-dialog").hidden = false;
  $("preview-result").hidden = true;
  report("访问令牌已更新");
});
