const test = require("node:test");
const assert = require("node:assert/strict");

const { nextSort, sortProfileList } = require("../web/app.js");

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
