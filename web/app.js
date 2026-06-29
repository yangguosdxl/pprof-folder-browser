const state = {
  tabs: [],
  activeTabId: "",
  views: {},
};

const browserDocument = typeof document === "undefined" ? null : document;
const tabList = browserDocument?.querySelector("#tabList");
const addTabBtn = browserDocument?.querySelector("#addTabBtn");
const tabNameForm = browserDocument?.querySelector("#tabNameForm");
const tabNameInput = browserDocument?.querySelector("#tabNameInput");
const dirForm = browserDocument?.querySelector("#dirForm");
const dirInput = browserDocument?.querySelector("#dirInput");
const chooseDirBtn = browserDocument?.querySelector("#chooseDirBtn");
const dirList = browserDocument?.querySelector("#dirList");
const scanBtn = browserDocument?.querySelector("#scanBtn");
const filterInput = browserDocument?.querySelector("#filterInput");
const profileTree = browserDocument?.querySelector("#profileTree");
const profileDetail = browserDocument?.querySelector("#profileDetail");
const summary = browserDocument?.querySelector("#summary");
const sortNameBtn = browserDocument?.querySelector("#sortNameBtn");
const sortSizeBtn = browserDocument?.querySelector("#sortSizeBtn");
const sessionList = browserDocument?.querySelector("#sessionList");
const clearSessionsBtn = browserDocument?.querySelector("#clearSessionsBtn");
const logBox = browserDocument?.querySelector("#logBox");

function createView() {
  return {
    dirs: [],
    profiles: [],
    sessions: [],
    selectedProfileId: "",
    filter: "",
    sort: {
      field: "",
      direction: "asc",
    },
  };
}

function ensureTabView(tabId) {
  if (!state.views[tabId]) {
    state.views[tabId] = createView();
  }
  return state.views[tabId];
}

function currentView() {
  if (!state.activeTabId) {
    return createView();
  }
  return ensureTabView(state.activeTabId);
}

function activeTab() {
  return state.tabs.find((tab) => tab.id === state.activeTabId) || null;
}

function log(message) {
  if (!logBox) return;
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

function pathWithTab(path, tabId = state.activeTabId) {
  if (!tabId) {
    return path;
  }
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}tabId=${encodeURIComponent(tabId)}`;
}

function renderTabs() {
  if (!tabList) return;

  tabList.innerHTML = state.tabs
    .map((tab) => {
      const active = tab.id === state.activeTabId;
      return `
        <button
          class="tab-button${active ? " active" : ""}"
          type="button"
          role="tab"
          aria-selected="${active ? "true" : "false"}"
          data-tab-id="${escapeHtml(tab.id)}"
          title="${escapeHtml(tab.name)}"
        >${escapeHtml(tab.name)}</button>
      `;
    })
    .join("");

  syncActiveTabForm();
}

function syncActiveTabForm() {
  if (!tabNameInput) return;
  const tab = activeTab();
  tabNameInput.value = tab ? tab.name : "";
  tabNameInput.disabled = !tab;
  if (tabNameForm) {
    const button = tabNameForm.querySelector("button");
    if (button) {
      button.disabled = !tab;
    }
  }
}

function renderDirs() {
  const view = currentView();
  if (view.dirs.length === 0) {
    dirList.innerHTML = `<div class="empty">还没有添加目录</div>`;
    return;
  }
  dirList.innerHTML = view.dirs
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
  const view = currentView();
  syncSelectedProfile(view);
  updateSummary();
  renderSortControls();
  renderProfileTree(buildProfileTree(view.profiles, view.dirs, view.sort));
  renderProfileDetail();
  applyProfileSearch();
}

function renderProfileTree(treeData) {
  if (!profileTree) return;

  destroyProfileTree();
  if (currentView().profiles.length === 0) {
    profileTree.innerHTML = `<div class="empty-cell">没有匹配的 profile 文件</div>`;
    return;
  }

  if (!hasJsTree()) {
    renderFallbackTree(treeData);
    return;
  }

  const tree = window.jQuery(profileTree);
  tree
    .jstree({
      core: {
        data: treeData,
        force_text: true,
        multiple: false,
        themes: {
          dots: true,
          icons: true,
        },
      },
      plugins: ["search", "types", "wholerow"],
      search: {
        case_sensitive: false,
        show_only_matches: true,
        show_only_matches_children: true,
        search_callback(search, node) {
          const value = search.trim().toLowerCase();
          if (!value) return true;
          return String(node.data?.searchText || node.text).toLowerCase().includes(value);
        },
      },
      types: {
        folder: { icon: "jstree-folder" },
        profile: { icon: "jstree-file" },
      },
    })
    .off(".profileTree")
    .on("ready.jstree.profileTree", () => {
      const selected = currentView().selectedProfileId;
      if (selected) {
        tree.jstree(true).select_node(profileNodeId(selected), true, true);
      }
      applyProfileSearch();
    })
    .on("select_node.jstree.profileTree", (_event, data) => {
      if (data.node.data?.kind === "profile") {
        currentView().selectedProfileId = data.node.data.profile.id;
        renderProfileDetail();
        return;
      }
      tree.jstree(true).toggle_node(data.node);
    })
    .on("dblclick.jstree.profileTree", ".jstree-anchor", (event) => {
      const node = tree.jstree(true).get_node(event.currentTarget);
      if (node?.data?.kind === "profile") {
        openProfile(node.data.profile.id);
      }
    });
}

function destroyProfileTree() {
  if (!hasJsTree() || !profileTree) return;
  const tree = window.jQuery(profileTree);
  if (tree.data("jstree")) {
    tree.off(".profileTree");
    tree.jstree("destroy");
  }
}

function hasJsTree() {
  return Boolean(window.jQuery?.fn?.jstree);
}

function renderFallbackTree(treeData) {
  profileTree.innerHTML = `<div class="tree-fallback">${renderFallbackNodes(treeData)}</div>`;
}

function renderFallbackNodes(nodes) {
  if (nodes.length === 0) {
    return `<div class="empty-cell">没有匹配的 profile 文件</div>`;
  }

  return `<ul>${nodes
    .map((node) => {
      const profile = node.data?.profile;
      if (profile) {
        return `
          <li>
            <button class="fallback-profile" type="button" data-select-id="${escapeHtml(profile.id)}">
              ${escapeHtml(profile.name)}
            </button>
          </li>
        `;
      }
      return `
        <li>
          <span class="fallback-folder">${escapeHtml(node.text)}</span>
          ${renderFallbackNodes(node.children || [])}
        </li>
      `;
    })
    .join("")}</ul>`;
}

function applyProfileSearch() {
  if (!profileTree) return;
  updateSummary();
  if (hasJsTree() && window.jQuery(profileTree).data("jstree")) {
    const tree = window.jQuery(profileTree).jstree(true);
    const keyword = currentView().filter.trim();
    if (keyword) {
      tree.search(keyword);
    } else {
      tree.clear_search();
    }
  } else if (currentView().profiles.length > 0) {
    renderFallbackTree(buildProfileTree(filteredProfiles(), currentView().dirs, currentView().sort));
  }
}

function updateSummary() {
  const view = currentView();
  summary.textContent = `共 ${view.profiles.length} 个文件，当前显示 ${filteredProfiles().length} 个`;
}

function filteredProfiles() {
  const view = currentView();
  const keyword = view.filter.trim().toLowerCase();
  if (!keyword) {
    return view.profiles;
  }
  return view.profiles.filter((profile) => profileMatchesFilter(profile, keyword));
}

function profileMatchesFilter(profile, keyword) {
  return `${profile.name} ${profile.path} ${profile.dir}`.toLowerCase().includes(keyword);
}

function renderProfileDetail() {
  if (!profileDetail) return;
  const profile = selectedProfile();
  if (!profile) {
    profileDetail.innerHTML = `<div class="empty">请选择一个 profile 文件</div>`;
    return;
  }

  profileDetail.innerHTML = `
    <div class="detail-name">${escapeHtml(profile.name)}</div>
    <dl>
      <dt>大小</dt>
      <dd>${formatSize(profile.size)}</dd>
      <dt>修改时间</dt>
      <dd>${formatTime(profile.modified)}</dd>
      <dt>目录</dt>
      <dd title="${escapeHtml(profile.dir)}">${escapeHtml(profile.dir)}</dd>
      <dt>路径</dt>
      <dd title="${escapeHtml(profile.path)}">${escapeHtml(profile.path)}</dd>
    </dl>
    <button class="open-btn detail-open-btn" type="button" data-open-id="${escapeHtml(profile.id)}">打开</button>
  `;
}

function selectedProfile() {
  const view = currentView();
  return view.profiles.find((profile) => profile.id === view.selectedProfileId) || null;
}

function syncSelectedProfile(view) {
  if (!view.profiles.some((profile) => profile.id === view.selectedProfileId)) {
    view.selectedProfileId = view.profiles[0]?.id || "";
  }
}

function renderSessions() {
  const view = currentView();
  clearSessionsBtn.disabled = view.sessions.length === 0;

  if (view.sessions.length === 0) {
    sessionList.innerHTML = `<div class="empty">尚未打开任何 pprof 页面</div>`;
    return;
  }

  sessionList.innerHTML = view.sessions
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

function renderSortControls() {
  const view = currentView();
  sortNameBtn.textContent = sortButtonText(view.sort, "name", "文件名");
  sortSizeBtn.textContent = sortButtonText(view.sort, "size", "大小");
}

function renderAll() {
  if (filterInput) {
    filterInput.value = currentView().filter;
  }
  renderTabs();
  renderDirs();
  renderProfiles();
  renderSessions();
}

function sortButtonText(sort, field, label) {
  if (sort.field !== field) {
    return label;
  }
  return `${label} ${sort.direction === "asc" ? "↑" : "↓"}`;
}

function sortProfiles(profiles) {
  return sortProfileList(profiles, currentView().sort);
}

function sortProfileList(profiles, sort) {
  if (!sort.field) {
    return profiles;
  }

  const direction = sort.direction === "asc" ? 1 : -1;
  return [...profiles].sort((left, right) => {
    let result = 0;
    if (sort.field === "name") {
      result = left.name.localeCompare(right.name, "zh-CN", { sensitivity: "base" });
    } else if (sort.field === "size") {
      result = left.size - right.size;
    }

    if (result === 0) {
      result = left.path.localeCompare(right.path, "zh-CN", { sensitivity: "base" });
    }
    return result * direction;
  });
}

function buildProfileTree(profiles, dirs, sort = { field: "", direction: "asc" }) {
  const roots = [];
  const rootMap = new Map();
  const rootDirs = dirs.length > 0 ? dirs : uniqueProfileDirs(profiles);

  for (const dir of rootDirs) {
    const key = normalizePath(dir).toLowerCase();
    if (!rootMap.has(key)) {
      const root = directoryNode(baseName(dir) || dir, dir, true);
      root.data.searchText = dir;
      rootMap.set(key, root);
      roots.push(root);
    }
  }

  for (const profile of profiles) {
    const root = findProfileRoot(profile, rootMap, roots);
    addProfileToTree(root, profile);
  }

  for (const root of roots) {
    sortTreeChildren(root.children, sort);
  }
  return roots.filter((root) => root.children.length > 0 || rootDirs.includes(root.data.path));
}

function uniqueProfileDirs(profiles) {
  const seen = new Set();
  const dirs = [];
  for (const profile of profiles) {
    const key = normalizePath(profile.dir).toLowerCase();
    if (!seen.has(key)) {
      seen.add(key);
      dirs.push(profile.dir);
    }
  }
  return dirs;
}

function findProfileRoot(profile, rootMap, roots) {
  const key = normalizePath(profile.dir).toLowerCase();
  if (rootMap.has(key)) {
    return rootMap.get(key);
  }

  const root = directoryNode(baseName(profile.dir) || profile.dir, profile.dir, true);
  root.data.searchText = profile.dir;
  rootMap.set(key, root);
  roots.push(root);
  return root;
}

function addProfileToTree(root, profile) {
  const parts = relativePathParts(profile);
  let parent = root;
  for (const part of parts.slice(0, -1)) {
    parent = ensureChildDirectory(parent, part);
  }
  parent.children.push(profileNode(profile));
}

function relativePathParts(profile) {
  const fullPath = normalizePath(profile.path);
  const rootPath = normalizePath(profile.dir);
  let relative = fullPath;
  if (fullPath.toLowerCase().startsWith(rootPath.toLowerCase())) {
    relative = fullPath.slice(rootPath.length).replace(/^\/+/, "");
  }
  const parts = relative.split("/").filter(Boolean);
  return parts.length > 0 ? parts : [profile.name];
}

function ensureChildDirectory(parent, name) {
  const key = name.toLowerCase();
  let child = parent.children.find((node) => node.data?.kind === "folder" && node.data.key === key);
  if (!child) {
    child = directoryNode(name, `${parent.data.path}/${name}`, false);
    parent.children.push(child);
  }
  return child;
}

function directoryNode(name, path, opened) {
  return {
    text: name,
    type: "folder",
    state: { opened },
    children: [],
    data: {
      kind: "folder",
      key: name.toLowerCase(),
      path,
      searchText: `${name} ${path}`,
    },
  };
}

function profileNode(profile) {
  return {
    id: profileNodeId(profile.id),
    text: profile.name,
    type: "profile",
    data: {
      kind: "profile",
      profile,
      searchText: `${profile.name} ${profile.path} ${profile.dir}`,
    },
  };
}

function profileNodeId(id) {
  return `profile-${id}`;
}

function sortTreeChildren(children, sort) {
  for (const child of children) {
    if (child.children) {
      sortTreeChildren(child.children, sort);
    }
  }

  children.sort((left, right) => {
    const leftIsFolder = left.data?.kind === "folder";
    const rightIsFolder = right.data?.kind === "folder";
    if (leftIsFolder !== rightIsFolder) {
      return leftIsFolder ? -1 : 1;
    }
    if (!leftIsFolder && !rightIsFolder) {
      return compareProfiles(left.data.profile, right.data.profile, sort);
    }
    return left.text.localeCompare(right.text, "zh-CN", { sensitivity: "base" });
  });
}

function compareProfiles(left, right, sort) {
  if (!sort.field) {
    return left.path.localeCompare(right.path, "zh-CN", { sensitivity: "base" });
  }
  return sortProfileList([left, right], sort)[0] === left ? -1 : 1;
}

function normalizePath(path) {
  return String(path || "").replaceAll("\\", "/").replace(/\/+$/, "");
}

function baseName(path) {
  const parts = normalizePath(path).split("/").filter(Boolean);
  return parts.at(-1) || path;
}

function toggleSort(field) {
  const view = currentView();
  view.sort = nextSort(view.sort, field);
  renderProfiles();
}

function nextSort(currentSort, field) {
  if (currentSort.field === field) {
    return {
      field,
      direction: currentSort.direction === "asc" ? "desc" : "asc",
    };
  }
  return { field, direction: "asc" };
}

async function loadTabs() {
  const data = await api("/api/tabs");
  state.tabs = data.tabs || [];
  for (const tab of state.tabs) {
    ensureTabView(tab.id);
  }
  if (!state.tabs.some((tab) => tab.id === state.activeTabId)) {
    state.activeTabId = data.activeTabId || state.tabs[0]?.id || "";
  }
  renderTabs();
}

async function createTab() {
  addTabBtn.disabled = true;
  try {
    const data = await api("/api/tabs", { method: "POST", body: "{}" });
    if (!data.tab) return;
    state.tabs = [...state.tabs, data.tab];
    state.activeTabId = data.tab.id;
    ensureTabView(data.tab.id);
    renderAll();
    await loadActiveTabData();
    log(`已新增页签：${data.tab.name}`);
  } catch (error) {
    log(error.message);
    alert(error.message);
  } finally {
    addTabBtn.disabled = false;
  }
}

async function renameActiveTab(name) {
  const trimmed = name.trim();
  if (!trimmed || !state.activeTabId) return;

  try {
    const data = await api("/api/tabs", {
      method: "PATCH",
      body: JSON.stringify({ id: state.activeTabId, name: trimmed }),
    });
    if (!data.tab) return;
    state.tabs = state.tabs.map((tab) => (tab.id === data.tab.id ? data.tab : tab));
    renderTabs();
    log(`已保存页签名称：${data.tab.name}`);
  } catch (error) {
    log(error.message);
    alert(error.message);
    syncActiveTabForm();
  }
}

async function switchTab(tabId) {
  if (!tabId || tabId === state.activeTabId) return;
  state.activeTabId = tabId;
  renderAll();
  try {
    await loadActiveTabData();
  } catch (error) {
    log(error.message);
    alert(error.message);
  }
}

async function loadActiveTabData() {
  if (!state.activeTabId) return;
  await Promise.all([loadDirs(), loadProfiles(), loadSessions()]);
}

async function loadDirs() {
  const tabId = state.activeTabId;
  const data = await api(pathWithTab("/api/dirs", tabId));
  ensureTabView(tabId).dirs = data.dirs || [];
  if (tabId === state.activeTabId) {
    renderDirs();
  }
}

async function loadProfiles() {
  const tabId = state.activeTabId;
  const data = await api(pathWithTab("/api/profiles", tabId));
  ensureTabView(tabId).profiles = data.profiles || [];
  if (tabId === state.activeTabId) {
    renderProfiles();
  }
}

async function scanProfiles() {
  const tabId = state.activeTabId;
  scanBtn.disabled = true;
  scanBtn.textContent = "扫描中";
  try {
    const data = await api(pathWithTab("/api/scan", tabId), { method: "POST", body: "{}" });
    const view = ensureTabView(tabId);
    view.profiles = data.profiles || [];
    if (tabId === state.activeTabId) {
      renderProfiles();
    }
    log(`扫描完成：找到 ${view.profiles.length} 个 profile 文件`);
  } catch (error) {
    log(error.message);
    alert(error.message);
  } finally {
    scanBtn.disabled = false;
    scanBtn.textContent = "扫描";
  }
}

async function loadSessions() {
  const tabId = state.activeTabId;
  const data = await api(pathWithTab("/api/sessions", tabId));
  ensureTabView(tabId).sessions = data.sessions || [];
  if (tabId === state.activeTabId) {
    renderSessions();
  }
}

async function clearSessions() {
  const tabId = state.activeTabId;
  clearSessionsBtn.disabled = true;
  clearSessionsBtn.textContent = "清除中";
  try {
    const data = await api(pathWithTab("/api/sessions", tabId), { method: "DELETE" });
    log(`已清除 ${data.cleared || 0} 个 pprof 进程`);
    await loadSessions();
  } catch (error) {
    log(error.message);
    alert(error.message);
  } finally {
    clearSessionsBtn.textContent = "清除全部";
    clearSessionsBtn.disabled = currentView().sessions.length === 0;
  }
}

async function openProfile(id) {
  const tabId = state.activeTabId;
  try {
    const data = await api(pathWithTab("/api/open", tabId), {
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
  const tabId = state.activeTabId;
  const data = await api(pathWithTab("/api/dirs", tabId), {
    method: "POST",
    body: JSON.stringify({ path }),
  });
  ensureTabView(tabId).dirs = data.dirs || [];
  dirInput.value = "";
  if (tabId === state.activeTabId) {
    renderDirs();
  }
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

async function removeDir(path) {
  const tabId = state.activeTabId;
  const data = await api(pathWithTab(`/api/dirs?path=${encodeURIComponent(path)}`, tabId), {
    method: "DELETE",
  });
  const view = ensureTabView(tabId);
  view.dirs = data.dirs || [];
  view.profiles = view.profiles.filter((profile) => profile.dir !== path);
  if (tabId === state.activeTabId) {
    renderDirs();
    renderProfiles();
  }
  log(`已移除目录：${path}`);
}

function boot() {
  tabList.addEventListener("click", (event) => {
    const button = event.target.closest("[data-tab-id]");
    if (!button) return;
    switchTab(button.dataset.tabId);
  });

  addTabBtn.addEventListener("click", createTab);

  tabNameForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    await renameActiveTab(tabNameInput.value);
  });

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
    try {
      await removeDir(button.dataset.removeDir);
    } catch (error) {
      log(error.message);
      alert(error.message);
    }
  });

  profileTree.addEventListener("click", (event) => {
    const button = event.target.closest("[data-select-id]");
    if (!button) return;
    currentView().selectedProfileId = button.dataset.selectId;
    renderProfileDetail();
  });

  profileDetail.addEventListener("click", (event) => {
    const button = event.target.closest("[data-open-id]");
    if (!button) return;
    openProfile(button.dataset.openId);
  });

  scanBtn.addEventListener("click", scanProfiles);
  chooseDirBtn.addEventListener("click", chooseDir);
  clearSessionsBtn.addEventListener("click", clearSessions);
  sortNameBtn.addEventListener("click", () => toggleSort("name"));
  sortSizeBtn.addEventListener("click", () => toggleSort("size"));
  filterInput.addEventListener("input", () => {
    currentView().filter = filterInput.value;
    applyProfileSearch();
  });

  loadTabs()
    .then(loadActiveTabData)
    .catch((error) => log(error.message));
  renderProfiles();
  renderSessions();
}

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

if (browserDocument) {
  boot();
}

if (typeof module !== "undefined") {
  module.exports = {
    buildProfileTree,
    nextSort,
    pathWithTab,
    profileMatchesFilter,
    sortProfileList,
  };
}
