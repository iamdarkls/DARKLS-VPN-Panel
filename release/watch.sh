#!/bin/sh
# DARKLS_PANEL_INTEGRATION v2
# DARKLS_VPN_HTTPS_MONITOR v2 (probes run inside the panel)
trap '' HUP
umask 077
BASE=/jffs/xray
LOCK=/tmp/darkls-watch.lock
PID=/tmp/darkls-watch.pid
HEARTBEAT=/tmp/darkls-monitor-heartbeat

owner_running() {
  [ -s "$PID" ] || return 1
  owner=$(cat "$PID")
  case "$owner" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$owner" 2>/dev/null || return 1
  grep -q '/jffs/xray/watch.sh' "/proc/$owner/cmdline" 2>/dev/null
}
# An unclean shutdown can leave an empty directory, but no kernel-owned PID.
# Recheck after a short grace to avoid stealing a newly starting watcher's lock.
if ! mkdir "$LOCK" 2>/dev/null; then
  owner_running && exit 0
  sleep 1
  owner_running && exit 0
  rmdir "$LOCK" 2>/dev/null || exit 1
  mkdir "$LOCK" 2>/dev/null || exit 0
fi
echo $$ > "$PID"
cleanup() {
  if [ "$(cat "$PID" 2>/dev/null)" = "$$" ]; then
    rm -f "$PID"
    rmdir "$LOCK" 2>/dev/null || true
  fi
}
trap cleanup 0
trap 'exit 0' TERM INT
seen_pid=
seen_at=0
while :; do
  sh "$BASE/panel/service.sh" ensure
  now=$(awk '{print int($1)}' /proc/uptime)
  panel_pid=$(cat /tmp/darkls-panel.pid 2>/dev/null)
  case "$panel_pid" in ''|*[!0-9]*) panel_pid= ;; esac
  if [ "$panel_pid" != "$seen_pid" ]; then seen_pid=$panel_pid; seen_at=$now; fi
  completed=$seen_at
  if [ -n "$panel_pid" ] && [ -s "$HEARTBEAT" ]; then
    read -r heartbeat_pid heartbeat_at < "$HEARTBEAT"
    case "$heartbeat_at" in ''|*[!0-9]*) heartbeat_at=0 ;; esac
    if [ "$heartbeat_pid" = "$panel_pid" ] && [ "$heartbeat_at" -le "$now" ]; then completed=$heartbeat_at; fi
  fi
  # A check plus bounded recovery may take up to 195 seconds. Never confuse
  # a slow network check with a hung panel; allow a full four-minute grace.
  if [ -n "$panel_pid" ] && [ $((now - completed)) -gt 240 ]; then
    if grep -q '/jffs/xray/panel/darkls-panel' "/proc/$panel_pid/cmdline" 2>/dev/null; then
      date
      echo 'Panel monitor heartbeat expired; restarting panel only'
      kill "$panel_pid" 2>/dev/null || true
      seen_at=$now
    fi
  fi
  sleep 15
done
