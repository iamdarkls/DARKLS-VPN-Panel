#!/bin/sh
set -e
ROOT=/jffs/xray
PANEL="$ROOT/panel"
test -s "$PANEL/watch.sh.before-panel"
test -s "$PANEL/usb-mount-script.before-panel"
if [ -s /tmp/darkls-watch.pid ]; then
  old_pid=$(cat /tmp/darkls-watch.pid)
  case "$old_pid" in ''|*[!0-9]*) exit 1 ;; esac
  if grep -q '/jffs/xray/watch.sh' "/proc/$old_pid/cmdline" 2>/dev/null; then
    kill "$old_pid"
    remaining=25
    while kill -0 "$old_pid" 2>/dev/null && [ "$remaining" -gt 0 ]; do sleep 1; remaining=$((remaining - 1)); done
    kill -0 "$old_pid" 2>/dev/null && { echo 'Watchdog did not stop'; exit 1; }
  fi
fi
sh "$PANEL/service.sh" stop
cp -p "$PANEL/watch.sh.before-panel" "$ROOT/watch.sh"
cp -p "$PANEL/usb-mount-script.before-panel" /jffs/scripts/usb-mount-script
sh /jffs/scripts/usb-mount-script
sync
echo 'Panel disabled. Current domain lists and VPN configuration are preserved.'
