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

fail() {
  echo "[错误] $*" >&2
  exit 1
}

stop_running_service() {
  if is_windows; then
    MSYS_NO_PATHCONV=1 taskkill.exe /F /T /IM "$app_name.exe" >/dev/null 2>&1 || true
  elif command -v pkill >/dev/null 2>&1; then
    pkill -x "$app_name" >/dev/null 2>&1 || true
  fi

  # 给 Windows 留出释放旧可执行文件的时间，再替换可执行文件。
  sleep 1
}

if ! command -v go >/dev/null 2>&1; then
  fail "未在 PATH 中找到 go 命令"
fi

if is_windows; then
  binary="$script_dir/$app_name.exe"
  new_binary="$script_dir/$app_name.new.exe"
else
  binary="$script_dir/$app_name"
  new_binary="$script_dir/$app_name.new"
fi
trap 'rm -f "$new_binary"' EXIT

# 源码版本以 version.go 为准，后面的编译产物和运行实例都必须与它一致。
source_version="$(sed -n 's/^const appVersion = "\(.*\)"[[:space:]]*$/\1/p' "$script_dir/version.go" | tr -d '\r')"
if [ -z "$source_version" ]; then
  fail "无法从 version.go 解析源码版本号（格式应为 const appVersion = \"x.y.z\"）"
fi
echo "[版本] 源码当前版本: $source_version"

# 先编译到临时文件：编译失败时旧服务保持运行，不会出现服务停了却没有新程序的情况。
echo "[构建] 正在编译 $app_name $source_version..."
build_status=0
build_output="$(cd "$script_dir" && go build -o "$new_binary" . 2>&1)" || build_status=$?
if [ "$build_status" -ne 0 ]; then
  echo "$build_output" >&2
  fail "编译失败（go build 退出码 $build_status），已中止；原服务未停止，可执行文件未被替换"
fi

built_version="$("$new_binary" --version 2>&1 | tr -d '\r' || true)"
echo "[版本] 编译产物版本: $built_version"
if [ "$built_version" != "$source_version" ]; then
  fail "编译产物版本($built_version)与源码版本($source_version)不一致，已中止"
fi

echo "[部署] 停止旧服务并替换可执行文件..."
stop_running_service
mv -f "$new_binary" "$binary" || fail "替换可执行文件失败，旧进程可能仍占用 $binary"

: > "$stdout_log"
: > "$stderr_log"
echo "[启动] 正在启动 $app_name $source_version..."
nohup "$binary" >"$stdout_log" 2>"$stderr_log" < /dev/null &
server_pid=$!
disown "$server_pid" 2>/dev/null || true

listen_addr="${PPROF_FOLDER_BROWSER_ADDR:-0.0.0.0:18080}"
port="${listen_addr##*:}"
url="http://127.0.0.1:$port"

if command -v curl >/dev/null 2>&1; then
  for _ in {1..20}; do
    if curl --noproxy "*" --silent --fail --max-time 1 "$url/api/tabs" >/dev/null 2>&1; then
      # 以运行中实例自己报告的版本为准，防止端口上仍是旧进程。
      running_version="$(curl --noproxy "*" --silent --max-time 2 "$url/api/version" 2>/dev/null \
        | sed -n 's/.*"version" *: *"\([^"]*\)".*/\1/p' || true)"
      echo "[版本] 运行中实例版本: ${running_version:-未知（实例没有 /api/version 接口，疑似旧版本）}"
      if [ "$running_version" != "$source_version" ]; then
        fail "运行中实例版本(${running_version:-未知})与源码版本($source_version)不一致，请检查是否有旧进程占用端口 $port"
      fi
      echo "启动成功：$url（版本 $running_version）"
      exit 0
    fi

    if ! kill -0 "$server_pid" 2>/dev/null; then
      echo "[错误] 服务进程在响应 $url 前已经退出，请查看：$stderr_log" >&2
      tail -n 20 "$stderr_log" >&2 || true
      exit 1
    fi

    sleep 0.25
  done

  fail "服务进程已经启动，但未响应 $url，请查看：$stderr_log"
fi

echo "启动成功：$url（未安装 curl，未能校验运行中实例版本）"
