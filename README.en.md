# pprof-folder-browser

[中文](README.md) | English

`pprof-folder-browser` is a local web tool for browsing Go pprof profile files in folders and opening each profile with the `go tool pprof` Web UI.

## Features

- Manage separate directory sets, scan results, and pprof sessions with tabs.
- Add one or more folders and scan them recursively for profile files.
- Display scan results as a directory tree while preserving each profile's relative folder hierarchy.
- Select a profile to inspect its size, modification time, source folder, and full path in a detail panel.
- Filter results by file name, path, or source folder.
- Sort results by file name or file size, with folders kept before files.
- Open a profile by starting `go tool pprof -http` and launching the pprof Web UI in a new browser window.
- Reuse an existing pprof session for the same profile file.
- Clear the pprof processes started from the active tab.

The scanner recognizes:

- Files ending with `.pprof`, `.pb.gz`, or `.prof`.
- Files whose names contain common pprof keywords such as `profile`, `heap`, `allocs`, `goroutine`, `threadcreate`, `block`, `mutex`, or `trace`.

## Requirements

- Go 1.24 or newer.
- The `go` command available on `PATH`.
- A modern web browser.
- Optional: Node.js for frontend unit tests.

The system folder picker is supported on Windows. On non-Windows platforms, enter folder paths manually.

The frontend uses jQuery 3.7.1 and jsTree 3.3.17. Their files are vendored under `web/vendor/` and served locally by the Go app, so runtime CDN access is not required.

## Run

```powershell
go run .
```

Then open:

```text
http://127.0.0.1:18080
```

The default listen address is `0.0.0.0:18080`. Override it with an environment variable:

```powershell
$env:PPROF_FOLDER_BROWSER_ADDR = "127.0.0.1:18080"
go run .
```

Linux/macOS:

```bash
PPROF_FOLDER_BROWSER_ADDR=127.0.0.1:18080 go run .
```

## Build

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

## Usage

1. Open `http://127.0.0.1:18080`.
2. Create or select a tab.
3. Enter a folder path, or use the folder picker on Windows.
4. Click "Scan".
5. Expand folders in the tree, or use the filter box and sort buttons to find a profile.
6. Select a profile to inspect its details, then click "Open". You can also double-click a profile in the tree.
7. Click "Clear all" when you want to stop the pprof processes for the active tab.

## Project Layout

```text
.
├── main.go                 # HTTP API, profile scanning, and pprof process management
├── dialog_windows.go       # Windows system folder picker
├── web/                    # Embedded frontend page and static assets
│   ├── app.js              # Tabs, directory tree, filtering, sorting, and session actions
│   ├── index.html
│   ├── style.css
│   └── vendor/             # Local browser dependencies
└── tests/                  # Frontend utility tests
```

## Tests

Backend tests:

```powershell
go test ./...
```

Frontend utility tests:

```powershell
node --test tests/app.test.cjs
```

## Notes

- Application state is kept in memory. Tabs, folders, scan results, and session records are cleared when the process exits.
- The app binds to `0.0.0.0` by default. Use `PPROF_FOLDER_BROWSER_ADDR=127.0.0.1:18080` if you only want local access.
- Profile data can be sensitive. Do not expose the app or the started `go tool pprof -http` pages on untrusted networks.
- If opening a profile fails because the `go` command cannot be found, verify that Go is installed and available on `PATH`.
- Third-party browser dependencies in `web/vendor/` keep their own MIT license files.

## License

MIT License. See [LICENSE](LICENSE).
