#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
app_name="pprof-folder-browser"
stdout_log="$script_dir/build-run.out.log"
stderr_log="$script_dir/build-run.err.log"

is_windows() {
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
    *) return 1 ;;
  esac
}

stop_running_service() {
  if is_windows; then
    MSYS_NO_PATHCONV=1 taskkill.exe /F /T /IM "$app_name.exe" >/dev/null 2>&1 || true
  elif command -v pkill >/dev/null 2>&1; then
    pkill -x "$app_name" >/dev/null 2>&1 || true
  fi

  # 给 Windows 留出释放旧可执行文件的时间，再开始重新构建。
  sleep 1
}

if ! command -v go >/dev/null 2>&1; then
  echo "未在 PATH 中找到 go 命令" >&2
  exit 1
fi

if is_windows; then
  binary="$script_dir/$app_name.exe"
else
  binary="$script_dir/$app_name"
fi

stop_running_service

echo "正在构建 $app_name..."
(
  cd "$script_dir"
  go build -o "$binary" .
)

: > "$stdout_log"
: > "$stderr_log"
echo "正在启动 $app_name..."
nohup "$binary" >"$stdout_log" 2>"$stderr_log" < /dev/null &
server_pid=$!
disown "$server_pid" 2>/dev/null || true

listen_addr="${PPROF_FOLDER_BROWSER_ADDR:-0.0.0.0:18080}"
port="${listen_addr##*:}"
url="http://127.0.0.1:$port"

if command -v curl >/dev/null 2>&1; then
  for _ in {1..20}; do
    if curl --noproxy "*" --silent --fail --max-time 1 "$url/api/tabs" >/dev/null 2>&1; then
      echo "启动成功：$url"
      exit 0
    fi

    if ! kill -0 "$server_pid" 2>/dev/null; then
      echo "服务进程在响应 $url 前已经退出，请查看：$stderr_log" >&2
      tail -n 20 "$stderr_log" >&2 || true
      exit 1
    fi

    sleep 0.25
  done

  echo "服务进程已经启动，但未响应 $url，请查看：$stderr_log" >&2
  exit 1
fi

echo "启动成功：$url"
