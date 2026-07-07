const state = {
  tabs: [],
  activeTabId: "",
  editingTabId: "",
  views: {},
};

const activeTabStorageKey = "pprof-folder-browser.activeTabId";
const tabsSidebarWidthStorageKey = "pprof-folder-browser.tabsSidebarWidth";
const tabsSidebarLeftExtraStorageKey = "pprof-folder-browser.tabsSidebarLeftExtra";
const sidebarWidthDefault = 220;
const sidebarWidthMin = 160;
const sidebarWidthMax = 420;
const sidebarLeftExtraMax = 260;
const browserDocument = typeof document === "undefined" ? null : document;
const browserWindow = typeof window === "undefined" ? null : window;
const shell = browserDocument?.querySelector(".shell");
const tabsPanel = browserDocument?.querySelector(".tabs-panel");
const sidebarResizeHandleLeft = browserDocument?.querySelector("#sidebarResizeHandleLeft");
const sidebarResizeHandleRight = browserDocument?.querySelector("#sidebarResizeHandleRight");
const tabList = browserDocument?.querySelector("#tabList");
const addTabBtn = browserDocument?.querySelector("#addTabBtn");
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

function loadStoredActiveTabId() {
  try {
    if (typeof localStorage === "undefined") return "";
    return localStorage.getItem(activeTabStorageKey) || "";
  } catch {
    return "";
  }
}

function storeActiveTabId(tabId) {
  try {
    if (typeof localStorage === "undefined") return;
    if (tabId) {
      localStorage.setItem(activeTabStorageKey, tabId);
    } else {
      localStorage.removeItem(activeTabStorageKey);
    }
  } catch {
    // Some browsers disable localStorage in private or restricted contexts.
  }
}

function clampSidebarWidth(width, containerWidth = 0) {
  const numericWidth = Number(width);
  const requestedWidth = Number.isFinite(numericWidth) ? numericWidth : sidebarWidthDefault;
  const containerMax = containerWidth > 0 ? Math.max(sidebarWidthMin, containerWidth - 360) : sidebarWidthMax;
  const maxWidth = Math.min(sidebarWidthMax, containerMax);
  return Math.round(Math.min(Math.max(requestedWidth, sidebarWidthMin), maxWidth));
}

function clampSidebarLeftExtra(extra, availableLeft = sidebarLeftExtraMax) {
  const numericExtra = Number(extra);
  const requestedExtra = Number.isFinite(numericExtra) ? numericExtra : 0;
  const maxExtra = Math.min(sidebarLeftExtraMax, Math.max(0, availableLeft));
  return Math.round(Math.min(Math.max(requestedExtra, 0), maxExtra));
}

function loadStoredSidebarWidth() {
  try {
    if (typeof localStorage === "undefined") return 0;
    return Number.parseInt(localStorage.getItem(tabsSidebarWidthStorageKey) || "", 10) || 0;
  } catch {
    return 0;
  }
}

function loadStoredSidebarLeftExtra() {
  try {
    if (typeof localStorage === "undefined") return 0;
    return Number.parseInt(localStorage.getItem(tabsSidebarLeftExtraStorageKey) || "", 10) || 0;
  } catch {
    return 0;
  }
}

function storeSidebarWidth(width) {
  try {
    if (typeof localStorage === "undefined") return;
    localStorage.setItem(tabsSidebarWidthStorageKey, String(Math.round(width)));
  } catch {
    // Some browsers disable localStorage in private or restricted contexts.
  }
}

function storeSidebarLeftExtra(extra) {
  try {
    if (typeof localStorage === "undefined") return;
    localStorage.setItem(tabsSidebarLeftExtraStorageKey, String(Math.round(extra)));
  } catch {
    // Some browsers disable localStorage in private or restricted contexts.
  }
}

function currentSidebarWidth() {
  if (!tabsPanel) return sidebarWidthDefault;
  const styledWidth = Number.parseInt(shell?.style.getPropertyValue("--tabs-sidebar-width") || "", 10);
  if (styledWidth > 0) return styledWidth;
  const width = tabsPanel.getBoundingClientRect().width - currentSidebarLeftExtra();
  return width > 0 ? width : sidebarWidthDefault;
}

function currentSidebarLeftExtra() {
  if (!shell) return 0;
  return Number.parseInt(shell.style.getPropertyValue("--tabs-sidebar-left-extra") || "", 10) || 0;
}

function availableSidebarLeftExtra() {
  if (!shell) return sidebarLeftExtraMax;
  return Math.max(0, shell.getBoundingClientRect().left - 8);
}

function applySidebarWidth(width) {
  if (!shell) return clampSidebarWidth(width);

  const clamped = clampSidebarWidth(width, shell.clientWidth);
  shell.style.setProperty("--tabs-sidebar-width", `${clamped}px`);
  if (sidebarResizeHandleRight) {
    sidebarResizeHandleRight.setAttribute("aria-valuenow", String(clamped));
  }
  return clamped;
}

function applySidebarLeftExtra(extra) {
  if (!shell) return clampSidebarLeftExtra(extra);

  const availableLeft = availableSidebarLeftExtra();
  const clamped = clampSidebarLeftExtra(extra, availableLeft);
  shell.style.setProperty("--tabs-sidebar-left-extra", `${clamped}px`);
  if (sidebarResizeHandleLeft) {
    sidebarResizeHandleLeft.setAttribute("aria-valuemax", String(clampSidebarLeftExtra(sidebarLeftExtraMax, availableLeft)));
    sidebarResizeHandleLeft.setAttribute("aria-valuenow", String(clamped));
  }
  return clamped;
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
      const editing = tab.id === state.editingTabId;
      const canDelete = state.tabs.length > 1;
      const tabClasses = ["tab-item"];
      if (active) tabClasses.push("active");
      if (editing) tabClasses.push("editing");
      return `
        <div class="${tabClasses.join(" ")}">
          ${
            editing
              ? `
                <form class="tab-edit-form" data-edit-tab-id="${escapeHtml(tab.id)}">
                  <input
                    class="tab-edit-input"
                    type="text"
                    value="${escapeHtml(tab.name)}"
                    aria-label="编辑页签名称"
                    autocomplete="off"
                  />
                </form>
              `
              : `
                <button
                  class="tab-button${active ? " active" : ""}"
                  type="button"
                  role="tab"
                  aria-selected="${active ? "true" : "false"}"
                  data-tab-id="${escapeHtml(tab.id)}"
                  title="${escapeHtml(tab.name)}"
                >${escapeHtml(tab.name)}</button>
                <div class="tab-action-buttons">
                  <button
                    class="tab-action-button tab-edit-button"
                    type="button"
                    data-edit-tab-button-id="${escapeHtml(tab.id)}"
                    aria-label="修改页签 ${escapeHtml(tab.name)}"
                    title="修改页签名称"
                  >&#9998;</button>
                  ${
                    canDelete
                      ? `
                        <button
                          class="tab-action-button tab-close-button"
                          type="button"
                          data-delete-tab-id="${escapeHtml(tab.id)}"
                          aria-label="删除页签 ${escapeHtml(tab.name)}"
                          title="删除页签"
                        >×</button>
                      `
                      : ""
                  }
                </div>
              `
          }
        </div>
      `;
    })
    .join("");

  focusEditingTabInput();
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
      <dd title="${escapeHtml(profile.path)}">
        <button
          class="copy-path-button"
          type="button"
          data-copy-path="${escapeHtml(profile.path)}"
          title="点击复制"
        >${escapeHtml(profile.path)}</button>
      </dd>
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
  const storedActiveTabId = loadStoredActiveTabId();
  state.tabs = data.tabs || [];
  if (!state.tabs.some((tab) => tab.id === state.editingTabId)) {
    state.editingTabId = "";
  }
  for (const tab of state.tabs) {
    ensureTabView(tab.id);
  }
  if (!state.tabs.some((tab) => tab.id === state.activeTabId)) {
    state.activeTabId = state.tabs.some((tab) => tab.id === storedActiveTabId)
      ? storedActiveTabId
      : data.activeTabId || state.tabs[0]?.id || "";
  }
  storeActiveTabId(state.activeTabId);
  renderTabs();
}

async function createTab() {
  addTabBtn.disabled = true;
  try {
    const data = await api("/api/tabs", { method: "POST", body: "{}" });
    if (!data.tab) return;
    state.editingTabId = "";
    state.tabs = [...state.tabs, data.tab];
    state.activeTabId = data.tab.id;
    storeActiveTabId(state.activeTabId);
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

async function renameTab(tabId, name) {
  const trimmed = name.trim();
  if (!trimmed || !tabId) return null;

  try {
    const data = await api("/api/tabs", {
      method: "PATCH",
      body: JSON.stringify({ id: tabId, name: trimmed }),
    });
    if (!data.tab) return null;
    state.tabs = state.tabs.map((tab) => (tab.id === data.tab.id ? data.tab : tab));
    return data.tab;
  } catch (error) {
    log(error.message);
    alert(error.message);
    return null;
  }
}

function startEditTab(tabId) {
  if (!state.tabs.some((tab) => tab.id === tabId)) return;
  state.editingTabId = tabId;
  renderTabs();
}

function cancelEditTab() {
  if (!state.editingTabId) return;
  state.editingTabId = "";
  renderTabs();
}

async function finishEditTab(tabId, name) {
  const tab = state.tabs.find((item) => item.id === tabId);
  if (!tab) return;

  const trimmed = name.trim();
  if (!trimmed || trimmed === tab.name) {
    state.editingTabId = "";
    renderTabs();
    return;
  }

  const renamed = await renameTab(tabId, trimmed);
  if (!renamed) {
    focusEditingTabInput();
    return;
  }
  state.editingTabId = "";
  renderTabs();
  log(`已保存页签名称：${renamed.name}`);
}

function focusEditingTabInput() {
  if (!state.editingTabId || !browserDocument) return;
  const focusInput = () => {
    const form = Array.from(browserDocument.querySelectorAll("[data-edit-tab-id]")).find(
      (item) => item.dataset.editTabId === state.editingTabId,
    );
    const input = form?.querySelector(".tab-edit-input");
    if (!input) return;
    input.focus();
    input.select();
  };

  if (typeof requestAnimationFrame === "function") {
    requestAnimationFrame(focusInput);
    return;
  }
  setTimeout(focusInput, 0);
}

function nextTabIdAfterDelete(tabs, activeTabId, deletedTabId) {
  if (activeTabId !== deletedTabId) return activeTabId;
  const index = tabs.findIndex((tab) => tab.id === deletedTabId);
  if (index < 0) return activeTabId;
  return tabs[index + 1]?.id || tabs[index - 1]?.id || "";
}

function fallbackTabIdAfterDelete(tabId) {
  return nextTabIdAfterDelete(state.tabs, state.activeTabId, tabId);
}

async function deleteTab(tabId) {
  if (!tabId || state.tabs.length <= 1) return;
  const tab = state.tabs.find((item) => item.id === tabId);
  if (!tab) return;

  const confirmed = window.confirm(`删除页签「${tab.name}」？该页签已打开的 pprof 进程会全部结束。`);
  if (!confirmed) return;

  const fallbackTabId = tabId === state.activeTabId ? fallbackTabIdAfterDelete(tabId) : state.activeTabId;
  try {
    const data = await api(pathWithTab("/api/tabs", tabId), { method: "DELETE" });
    state.tabs = data.tabs || state.tabs.filter((item) => item.id !== tabId);
    delete state.views[tabId];
    if (state.editingTabId === tabId) {
      state.editingTabId = "";
    }

    if (!state.tabs.some((item) => item.id === state.activeTabId)) {
      state.activeTabId = state.tabs.some((item) => item.id === fallbackTabId)
        ? fallbackTabId
        : data.activeTabId || state.tabs[0]?.id || "";
    }
    storeActiveTabId(state.activeTabId);
    renderAll();
    await loadActiveTabData();
    log(`已删除页签：${tab.name}，结束 ${data.cleared || 0} 个 pprof 进程`);
  } catch (error) {
    log(error.message);
    alert(error.message);
  }
}

async function copyPathToClipboard(path) {
  if (!path) return;
  try {
    await writeClipboardText(path);
    showCopyPathSuccess();
  } catch (error) {
    log(`复制路径失败：${error.message}`);
    alert(`复制路径失败：${error.message}`);
  }
}

function showCopyPathSuccess() {
  const message = "已复制路径到剪切板";
  log(message);
  if (!browserDocument) return;

  const status = browserDocument.createElement("div");
  status.className = "toast";
  status.textContent = message;
  browserDocument.body.appendChild(status);
  setTimeout(() => status.remove(), 1600);
}

async function writeClipboardText(text) {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      fallbackCopyText(text);
      return;
    }
  }
  fallbackCopyText(text);
}

function fallbackCopyText(text) {
  if (!browserDocument?.body) {
    throw new Error("当前环境不支持剪切板");
  }

  const textarea = browserDocument.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  browserDocument.body.appendChild(textarea);
  textarea.select();
  if (typeof browserDocument.execCommand !== "function") {
    textarea.remove();
    throw new Error("当前环境不支持剪切板");
  }
  const copied = browserDocument.execCommand("copy");
  textarea.remove();
  if (!copied) {
    throw new Error("浏览器拒绝复制");
  }
}

async function switchTab(tabId) {
  if (!tabId || tabId === state.activeTabId) return;
  state.editingTabId = "";
  state.activeTabId = tabId;
  storeActiveTabId(state.activeTabId);
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
  initSidebarResize();

  tabList.addEventListener("click", (event) => {
    const deleteButton = event.target.closest("[data-delete-tab-id]");
    if (deleteButton) {
      deleteTab(deleteButton.dataset.deleteTabId);
      return;
    }

    const editButton = event.target.closest("[data-edit-tab-button-id]");
    if (editButton) {
      startEditTab(editButton.dataset.editTabButtonId);
      return;
    }

    const button = event.target.closest("[data-tab-id]");
    if (!button) return;
    switchTab(button.dataset.tabId);
  });

  tabList.addEventListener("submit", async (event) => {
    const form = event.target.closest("[data-edit-tab-id]");
    if (!form) return;
    event.preventDefault();
    const input = form.querySelector(".tab-edit-input");
    await finishEditTab(form.dataset.editTabId, input?.value || "");
  });

  tabList.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    if (!event.target.closest("[data-edit-tab-id]")) return;
    cancelEditTab();
  });

  addTabBtn.addEventListener("click", createTab);

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
    const copyButton = event.target.closest("[data-copy-path]");
    if (copyButton) {
      copyPathToClipboard(copyButton.dataset.copyPath);
      return;
    }

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

function initSidebarResize() {
  if (!shell || !tabsPanel) return;

  let latestWidth = applySidebarWidth(loadStoredSidebarWidth() || currentSidebarWidth());
  let latestLeftExtra = applySidebarLeftExtra(loadStoredSidebarLeftExtra());

  if (sidebarResizeHandleRight) {
    sidebarResizeHandleRight.setAttribute("aria-valuemin", String(sidebarWidthMin));
    sidebarResizeHandleRight.setAttribute("aria-valuemax", String(sidebarWidthMax));
    let draggingRight = false;
    let startX = 0;
    let startWidth = latestWidth;

    sidebarResizeHandleRight.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      draggingRight = true;
      startX = event.clientX;
      startWidth = currentSidebarWidth();
      latestWidth = startWidth;
      browserDocument.body.classList.add("resizing-sidebar");
      sidebarResizeHandleRight.setPointerCapture?.(event.pointerId);
    });

    sidebarResizeHandleRight.addEventListener("pointermove", (event) => {
      if (!draggingRight) return;
      latestWidth = applySidebarWidth(startWidth + event.clientX - startX);
    });

    const finishRightDrag = (event) => {
      if (!draggingRight) return;
      draggingRight = false;
      browserDocument.body.classList.remove("resizing-sidebar");
      sidebarResizeHandleRight.releasePointerCapture?.(event.pointerId);
      latestWidth = applySidebarWidth(latestWidth);
      storeSidebarWidth(latestWidth);
    };

    sidebarResizeHandleRight.addEventListener("pointerup", finishRightDrag);
    sidebarResizeHandleRight.addEventListener("pointercancel", finishRightDrag);
    sidebarResizeHandleRight.addEventListener("keydown", (event) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const delta = event.shiftKey ? 30 : 10;
      const direction = event.key === "ArrowRight" ? 1 : -1;
      latestWidth = applySidebarWidth(currentSidebarWidth() + direction * delta);
      storeSidebarWidth(latestWidth);
    });
  }

  if (sidebarResizeHandleLeft) {
    sidebarResizeHandleLeft.setAttribute("aria-valuemin", "0");
    sidebarResizeHandleLeft.setAttribute("aria-valuemax", String(sidebarLeftExtraMax));
    let draggingLeft = false;
    let startX = 0;
    let startLeftExtra = latestLeftExtra;

    sidebarResizeHandleLeft.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      draggingLeft = true;
      startX = event.clientX;
      startLeftExtra = currentSidebarLeftExtra();
      latestLeftExtra = startLeftExtra;
      browserDocument.body.classList.add("resizing-sidebar");
      sidebarResizeHandleLeft.setPointerCapture?.(event.pointerId);
    });

    sidebarResizeHandleLeft.addEventListener("pointermove", (event) => {
      if (!draggingLeft) return;
      latestLeftExtra = applySidebarLeftExtra(startLeftExtra + startX - event.clientX);
    });

    const finishLeftDrag = (event) => {
      if (!draggingLeft) return;
      draggingLeft = false;
      browserDocument.body.classList.remove("resizing-sidebar");
      sidebarResizeHandleLeft.releasePointerCapture?.(event.pointerId);
      latestLeftExtra = applySidebarLeftExtra(latestLeftExtra);
      storeSidebarLeftExtra(latestLeftExtra);
    };

    sidebarResizeHandleLeft.addEventListener("pointerup", finishLeftDrag);
    sidebarResizeHandleLeft.addEventListener("pointercancel", finishLeftDrag);
    sidebarResizeHandleLeft.addEventListener("keydown", (event) => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const delta = event.shiftKey ? 30 : 10;
      const direction = event.key === "ArrowLeft" ? 1 : -1;
      latestLeftExtra = applySidebarLeftExtra(currentSidebarLeftExtra() + direction * delta);
      storeSidebarLeftExtra(latestLeftExtra);
    });
  }

  browserWindow?.addEventListener("resize", () => {
    latestWidth = applySidebarWidth(loadStoredSidebarWidth() || currentSidebarWidth());
    latestLeftExtra = applySidebarLeftExtra(loadStoredSidebarLeftExtra());
  });
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
    clampSidebarLeftExtra,
    clampSidebarWidth,
    nextTabIdAfterDelete,
    nextSort,
    pathWithTab,
    profileMatchesFilter,
    sortProfileList,
  };
}
