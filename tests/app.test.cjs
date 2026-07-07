const test = require("node:test");
const assert = require("node:assert/strict");

const {
  buildProfileTree,
  clampSidebarLeftExtra,
  clampSidebarWidth,
  nextSort,
  nextTabIdAfterDelete,
  pathWithTab,
  profileMatchesFilter,
  sortProfileList,
} = require("../web/app.js");

test("文件名排序按名称升序并使用路径兜底", () => {
  const profiles = [
    { name: "heap.pprof", path: "D:/b/heap.pprof", size: 30 },
    { name: "allocs.pprof", path: "D:/a/allocs.pprof", size: 10 },
    { name: "heap.pprof", path: "D:/a/heap.pprof", size: 20 },
  ];

  const sorted = sortProfileList(profiles, { field: "name", direction: "asc" });

  assert.deepEqual(
    sorted.map((profile) => profile.path),
    ["D:/a/allocs.pprof", "D:/a/heap.pprof", "D:/b/heap.pprof"],
  );
});

test("大小排序支持降序", () => {
  const profiles = [
    { name: "small.pprof", path: "D:/small.pprof", size: 10 },
    { name: "large.pprof", path: "D:/large.pprof", size: 100 },
    { name: "middle.pprof", path: "D:/middle.pprof", size: 50 },
  ];

  const sorted = sortProfileList(profiles, { field: "size", direction: "desc" });

  assert.deepEqual(
    sorted.map((profile) => profile.name),
    ["large.pprof", "middle.pprof", "small.pprof"],
  );
});

test("重复点击同一排序字段会切换方向", () => {
  assert.deepEqual(nextSort({ field: "", direction: "asc" }, "name"), {
    field: "name",
    direction: "asc",
  });
  assert.deepEqual(nextSort({ field: "name", direction: "asc" }, "name"), {
    field: "name",
    direction: "desc",
  });
  assert.deepEqual(nextSort({ field: "name", direction: "desc" }, "size"), {
    field: "size",
    direction: "asc",
  });
});

test("接口路径会追加当前页签 ID", () => {
  assert.equal(pathWithTab("/api/dirs", "tab-2"), "/api/dirs?tabId=tab-2");
  assert.equal(pathWithTab("/api/dirs?path=D%3A%2Fprofiles", "tab-2"), "/api/dirs?path=D%3A%2Fprofiles&tabId=tab-2");
});

test("删除页签后选择合理的活动页签", () => {
  const tabs = [{ id: "tab-1" }, { id: "tab-2" }, { id: "tab-3" }];

  assert.equal(nextTabIdAfterDelete(tabs, "tab-2", "tab-2"), "tab-3");
  assert.equal(nextTabIdAfterDelete(tabs, "tab-3", "tab-3"), "tab-2");
  assert.equal(nextTabIdAfterDelete(tabs, "tab-1", "tab-2"), "tab-1");
});

test("页签栏宽度会被限制在可用范围内", () => {
  assert.equal(clampSidebarWidth(80, 1000), 160);
  assert.equal(clampSidebarWidth(260, 1000), 260);
  assert.equal(clampSidebarWidth(500, 1000), 420);
  assert.equal(clampSidebarWidth(500, 700), 340);
  assert.equal(clampSidebarLeftExtra(-20, 200), 0);
  assert.equal(clampSidebarLeftExtra(120, 200), 120);
  assert.equal(clampSidebarLeftExtra(300, 200), 200);
  assert.equal(clampSidebarLeftExtra(300, 400), 260);
});

test("扫描结果会按目录层级构造成树", () => {
  const profiles = [
    {
      id: "cpu",
      name: "cpu.pprof",
      path: "D:\\profiles\\service-a\\cpu.pprof",
      dir: "D:\\profiles",
      size: 10,
    },
    {
      id: "heap",
      name: "heap.pprof",
      path: "D:\\profiles\\service-a\\nested\\heap.pprof",
      dir: "D:\\profiles",
      size: 20,
    },
  ];

  const tree = buildProfileTree(profiles, ["D:\\profiles"], { field: "name", direction: "asc" });

  assert.equal(tree.length, 1);
  assert.equal(tree[0].text, "profiles");
  assert.equal(tree[0].children[0].text, "service-a");
  assert.equal(tree[0].children[0].children[0].text, "nested");
  assert.equal(tree[0].children[0].children[1].text, "cpu.pprof");
  assert.equal(tree[0].children[0].children[1].data.profile.id, "cpu");
});

test("过滤会匹配文件名和路径", () => {
  const profile = {
    name: "heap.pprof",
    path: "D:/profiles/service-a/heap.pprof",
    dir: "D:/profiles",
  };

  assert.equal(profileMatchesFilter(profile, "service-a"), true);
  assert.equal(profileMatchesFilter(profile, "mutex"), false);
});
