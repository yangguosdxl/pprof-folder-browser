const state = {
  dirs: [],
  profiles: [],
  sessions: [],
  filter: "",
};

const dirForm = document.querySelector("#dirForm");
const dirInput = document.querySelector("#dirInput");
const chooseDirBtn = document.querySelector("#chooseDirBtn");
const dirList = document.querySelector("#dirList");
const scanBtn = document.querySelector("#scanBtn");
const filterInput = document.querySelector("#filterInput");
const profileRows = document.querySelector("#profileRows");
const summary = document.querySelector("#summary");
const sessionList = document.querySelector("#sessionList");
const logBox = document.querySelector("#logBox");

function log(message) {
  const line = `[${new Date().toLocaleTimeString()}] ${message}`;
  logBox.textContent = `${line}\n${logBox.textContent}`.slice(0, 4000);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `请求失败：${response.status}`);
  }
  return data;
}

function renderDirs() {
  if (state.dirs.length === 0) {
    dirList.innerHTML = `<div class="empty">还没有添加目录</div>`;
    return;
  }
  dirList.innerHTML = state.dirs
    .map(
      (dir) => `
        <div class="dir-item">
          <span title="${escapeHtml(dir)}">${escapeHtml(dir)}</span>
          <button data-remove-dir="${escapeHtml(dir)}">移除</button>
        </div>
      `,
    )
    .join("");
}

function renderProfiles() {
  const keyword = state.filter.trim().toLowerCase();
  const profiles = state.profiles.filter((profile) => {
    if (!keyword) return true;
    return `${profile.name} ${profile.path}`.toLowerCase().includes(keyword);
  });

  summary.textContent = `共 ${state.profiles.length} 个文件，当前显示 ${profiles.length} 个`;

  if (profiles.length === 0) {
    profileRows.innerHTML = `<tr><td colspan="5" class="empty-cell">没有匹配的 profile 文件</td></tr>`;
    return;
  }

  profileRows.innerHTML = profiles
    .map(
      (profile) => `
        <tr>
          <td>
            <div class="file-name">${escapeHtml(profile.name)}</div>
            <div class="file-path" title="${escapeHtml(profile.path)}">${escapeHtml(profile.path)}</div>
          </td>
          <td>${formatSize(profile.size)}</td>
          <td>${formatTime(profile.modified)}</td>
          <td class="dir-cell" title="${escapeHtml(profile.dir)}">${escapeHtml(profile.dir)}</td>
          <td><button class="open-btn" data-open-id="${profile.id}">打开</button></td>
        </tr>
      `,
    )
    .join("");
}

function renderSessions() {
  if (state.sessions.length === 0) {
    sessionList.innerHTML = `<div class="empty">尚未打开任何 pprof 页面</div>`;
    return;
  }

  sessionList.innerHTML = state.sessions
    .map(
      (session) => `
        <a class="session-item" href="${escapeHtml(session.url)}" target="_blank" rel="noreferrer">
          <span>${escapeHtml(session.url)}</span>
          <small>${escapeHtml(session.path)}</small>
        </a>
      `,
    )
    .join("");
}

async function loadDirs() {
  const data = await api("/api/dirs");
  state.dirs = data.dirs || [];
  renderDirs();
}

async function scanProfiles() {
  scanBtn.disabled = true;
  scanBtn.textContent = "扫描中";
  try {
    const data = await api("/api/scan", { method: "POST", body: "{}" });
    state.profiles = data.profiles || [];
    renderProfiles();
    log(`扫描完成：找到 ${state.profiles.length} 个 profile 文件`);
  } catch (error) {
    log(error.message);
    alert(error.message);
  } finally {
    scanBtn.disabled = false;
    scanBtn.textContent = "扫描";
  }
}

async function loadSessions() {
  const data = await api("/api/sessions");
  state.sessions = data.sessions || [];
  renderSessions();
}

async function openProfile(id) {
  try {
    const data = await api("/api/open", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    log(`${data.reused ? "复用" : "启动"} pprof 页面：${data.url}`);
    window.open(data.url, "_blank", "noopener,noreferrer");
    await loadSessions();
  } catch (error) {
    log(error.message);
    alert(error.message);
  }
}

async function addDir(path) {
  const data = await api("/api/dirs", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
  state.dirs = data.dirs || [];
  dirInput.value = "";
  renderDirs();
  log(`已添加目录：${path}`);
}

async function chooseDir() {
  chooseDirBtn.disabled = true;
  chooseDirBtn.textContent = "选择中";
  log("正在打开目录选择窗口");
  try {
    const data = await api("/api/select-dir", { method: "POST", body: "{}" });
    if (data.canceled) {
      log("已取消选择目录");
      return;
    }
    if (!data.path) {
      log("目录选择窗口没有返回路径");
      return;
    }
    dirInput.value = data.path;
    // 系统弹窗只负责选路径，添加目录仍复用手动输入的校验和刷新逻辑。
    await addDir(data.path);
  } catch (error) {
    log(error.message);
    alert(error.message);
  } finally {
    chooseDirBtn.disabled = false;
    chooseDirBtn.textContent = "选择目录";
  }
}

dirForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const path = dirInput.value.trim();
  if (!path) return;
  try {
    await addDir(path);
  } catch (error) {
    log(error.message);
    alert(error.message);
  }
});

dirList.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-remove-dir]");
  if (!button) return;
  const path = button.dataset.removeDir;
  try {
    const data = await api(`/api/dirs?path=${encodeURIComponent(path)}`, { method: "DELETE" });
    state.dirs = data.dirs || [];
    state.profiles = state.profiles.filter((profile) => profile.dir !== path);
    renderDirs();
    renderProfiles();
    log(`已移除目录：${path}`);
  } catch (error) {
    log(error.message);
    alert(error.message);
  }
});

profileRows.addEventListener("click", (event) => {
  const button = event.target.closest("[data-open-id]");
  if (!button) return;
  openProfile(button.dataset.openId);
});

scanBtn.addEventListener("click", scanProfiles);
chooseDirBtn.addEventListener("click", chooseDir);
filterInput.addEventListener("input", () => {
  state.filter = filterInput.value;
  renderProfiles();
});

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1024;
  for (const unit of units) {
    if (value < 1024) return `${value.toFixed(1)} ${unit}`;
    value /= 1024;
  }
  return `${value.toFixed(1)} TB`;
}

function formatTime(value) {
  if (!value) return "-";
  return new Date(value).toLocaleString();
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

loadDirs().catch((error) => log(error.message));
loadSessions().catch((error) => log(error.message));
renderProfiles();
renderSessions();
