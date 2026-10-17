#!/bin/sh
BASE=/jffs/xray/panel
PID=/tmp/darkls-panel.pid

running() {
  [ -s "$PID" ] || return 1
  panel_pid=$(cat "$PID")
  case "$panel_pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$panel_pid" 2>/dev/null || return 1
  grep -q '/jffs/xray/panel/darkls-panel' "/proc/$panel_pid/cmdline" 2>/dev/null
}

case "${1:-ensure}" in
  ensure|start)
    running && exit 0
    [ -x "$BASE/darkls-panel" ] && [ -s "$BASE/auth.json" ] || exit 1
    # Inherit HUP-ignore across exec; independent of SSH and USB installer lifetime.
    (trap '' HUP; exec "$BASE/darkls-panel") >> /tmp/darkls-panel.log 2>&1 < /dev/null &
    ;;
  stop)
    if running; then kill "$panel_pid"; fi
    ;;
  *) echo "Usage: $0 ensure|stop"; exit 1 ;;
esac
