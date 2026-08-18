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

  # Give Windows time to release the previous executable before rebuilding it.
  sleep 1
}

if ! command -v go >/dev/null 2>&1; then
  echo "go command was not found in PATH" >&2
  exit 1
fi

if is_windows; then
  binary="$script_dir/$app_name.exe"
else
  binary="$script_dir/$app_name"
fi

stop_running_service

echo "Building $app_name..."
(
  cd "$script_dir"
  go build -o "$binary" .
)

: > "$stdout_log"
: > "$stderr_log"
echo "Starting $app_name..."
nohup "$binary" >"$stdout_log" 2>"$stderr_log" < /dev/null &
server_pid=$!
disown "$server_pid" 2>/dev/null || true

listen_addr="${PPROF_FOLDER_BROWSER_ADDR:-0.0.0.0:18080}"
port="${listen_addr##*:}"
url="http://127.0.0.1:$port"

if command -v curl >/dev/null 2>&1; then
  for _ in {1..20}; do
    if curl --silent --fail --max-time 1 "$url/api/tabs" >/dev/null 2>&1; then
      echo "Started: $url"
      exit 0
    fi
    sleep 0.25
  done

  echo "The process started but did not respond at $url. See $stderr_log" >&2
  exit 1
fi

echo "Started: $url"
