# pprof-folder-browser

中文 | [English](README.en.md)

`pprof-folder-browser` 是一个本地 Web 工具，用来集中浏览目录中的 Go pprof profile 文件，并一键启动对应的 `go tool pprof` Web UI。

## 功能

- 按页签管理多组目录、扫描结果和已打开的 pprof 会话。
- 添加一个或多个目录后递归扫描 profile 文件。
- 以目录树展示扫描结果，保留 profile 文件所在的相对目录层级。
- 选择 profile 文件后在详情面板查看文件大小、修改时间、目录和完整路径。
- 支持按文件名、路径或来源目录过滤结果。
- 支持按文件名、文件大小排序；目录始终排在文件前。
- 点击 profile 文件后自动执行 `go tool pprof -http`，并在浏览器新窗口打开 pprof Web UI。
- 对同一个 profile 会复用已启动的 pprof 会话。
- 可清除当前页签下已启动的 pprof 进程。

扫描会识别以下文件：

- 扩展名为 `.pprof`、`.pb.gz`、`.prof` 的文件。
- 文件名包含 `profile`、`heap`、`allocs`、`goroutine`、`threadcreate`、`block`、`mutex`、`trace` 等常见 pprof 关键词的文件。

## 环境要求

- Go 1.24 或更新版本。
- `go` 命令已加入 `PATH`。
- 现代浏览器。
- 可选：Node.js，用于运行前端单元测试。

Windows 上支持“选择目录”系统弹窗。非 Windows 平台目前需要手动输入目录路径。

前端依赖 jQuery 3.7.1 和 jsTree 3.3.17，相关文件已放在 `web/vendor/` 并由 Go 程序本地提供，不需要运行时访问 CDN。

## 运行

```powershell
go run .
```

启动后打开：

```text
http://127.0.0.1:18080
```

默认监听地址是 `0.0.0.0:18080`。可以通过环境变量修改：

```powershell
$env:PPROF_FOLDER_BROWSER_ADDR = "127.0.0.1:18080"
go run .
```

Linux/macOS:

```bash
PPROF_FOLDER_BROWSER_ADDR=127.0.0.1:18080 go run .
```

## 构建

Windows:

```powershell
go build -o pprof-folder-browser.exe .
.\pprof-folder-browser.exe
```

Linux/macOS:

```bash
go build -o pprof-folder-browser .
./pprof-folder-browser
```

## 使用流程

1. 打开 `http://127.0.0.1:18080`。
2. 新建或选择一个页签。
3. 输入目录路径，或在 Windows 上点击“选择目录”。
4. 点击“扫描”。
5. 在目录树中展开目录，或使用过滤框和排序按钮定位 profile 文件。
6. 选择 profile 文件查看详情，点击“打开”；也可以双击树中的 profile 文件。
7. 不再需要时，点击“清除全部”结束当前页签下的 pprof 进程。

## 项目结构

```text
.
├── main.go                 # HTTP API、profile 扫描和 pprof 进程管理
├── dialog_windows.go       # Windows 系统目录选择窗口
├── web/                    # 内嵌前端页面和静态资源
│   ├── app.js              # 页签、目录树、过滤、排序和会话操作
│   ├── index.html
│   ├── style.css
│   └── vendor/             # 本地前端依赖
└── tests/                  # 前端工具函数测试
```

## 测试

后端测试：

```powershell
go test ./...
```

前端工具函数测试：

```powershell
node --test tests/app.test.cjs
```

## 注意事项

- 页签、目录和扫描结果会保存到用户配置目录下的 `pprof-folder-browser/state.json`，重启后会自动恢复；也可以用 `PPROF_FOLDER_BROWSER_STATE` 指定状态文件路径。
- `pprof` 会话记录不会跨重启保留，因为对应的 `go tool pprof` 进程会在应用退出后结束。
- 工具默认绑定 `0.0.0.0`，适合本机或可信网络使用；如果只想本机访问，请设置 `PPROF_FOLDER_BROWSER_ADDR=127.0.0.1:18080`。
- `go tool pprof -http` 启动的 profile 页面也可能暴露性能数据，请不要在不可信网络中公开访问。
- 如果点击“打开”时报找不到 `go` 命令，请确认 Go 已正确安装并加入 `PATH`。
- `web/vendor/` 中的第三方浏览器依赖保留各自的 MIT 许可证文件。

## 许可证

MIT License，详见 [LICENSE](LICENSE)。
