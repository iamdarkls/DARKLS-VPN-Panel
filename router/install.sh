#!/bin/sh
set -e
umask 077
PKG=$(CDPATH= cd "$(dirname "$0")" && pwd)
ROOT=/jffs/xray
PANEL="$ROOT/panel"

grep -q '^Uid:[[:space:]]*0[[:space:]]' /proc/self/status || { echo 'Run as router administrator'; exit 1; }
[ "$(uname -m)" = aarch64 ] || { echo 'This package requires aarch64'; exit 1; }
for file in vpn.sh watch.sh domains.txt dnsmasq-vpn.conf telegram-networks.txt; do
  [ -f "$ROOT/$file" ] || { echo "Missing $ROOT/$file"; exit 1; }
done
[ -f /jffs/scripts/usb-mount-script ] || { echo 'USB startup handler is missing'; exit 1; }
grep -q 'darkls_yt' "$ROOT/vpn.sh" || { echo 'YouTube fix was not found; installation stopped'; exit 1; }
grep -q '/jffs/xray/watch.sh' /jffs/scripts/usb-mount-script || { echo 'Unknown USB startup handler'; exit 1; }
test -s "$PKG/darkls-panel"
test -s "$PKG/service.sh"

mkdir -p "$PANEL"
chmod 700 "$PANEL"
if [ ! -f "$PANEL/watch.sh.before-panel" ]; then cp -p "$ROOT/watch.sh" "$PANEL/watch.sh.before-panel"; fi
if [ ! -f "$PANEL/usb-mount-script.before-panel" ]; then cp -p /jffs/scripts/usb-mount-script "$PANEL/usb-mount-script.before-panel"; fi

# Do not change the existing VPN routing script or the Xray connection config.
cp "$PKG/darkls-panel" "$PANEL/darkls-panel.new"
chmod 700 "$PANEL/darkls-panel.new"
if [ -f "$PANEL/darkls-panel" ]; then cp -p "$PANEL/darkls-panel" "$PANEL/darkls-panel.previous"; fi
mv "$PANEL/darkls-panel.new" "$PANEL/darkls-panel"
cp "$PKG/service.sh" "$PANEL/service.sh"
chmod 700 "$PANEL/service.sh"
"$PANEL/darkls-panel" --init

if ! grep -q 'DARKLS_PANEL_INTEGRATION' "$ROOT/watch.sh"; then
  grep -q 'sh /jffs/xray/vpn.sh start ||' "$ROOT/watch.sh" || { echo 'Unknown watchdog; installation stopped'; exit 1; }
  awk '
    NR == 1 { print; print "# DARKLS_PANEL_INTEGRATION v1"; next }
    /^while :; do$/ {
      print
      print "  sh /jffs/xray/panel/service.sh ensure"
      next
    }
    /sh \/jffs\/xray\/vpn.sh start \|\|/ {
      sub("sh /jffs/xray/vpn.sh start", "/jffs/xray/panel/darkls-panel --watch-recover")
    }
    { print }
  ' "$ROOT/watch.sh" > "$PANEL/watch.sh.new"
else
  cp -p "$ROOT/watch.sh" "$PANEL/watch.sh.new"
fi

cat > "$PANEL/usb-mount-script.new" <<'USBEOF'
#!/bin/sh
{
  echo 'USB startup triggered:'
  date
} >> /tmp/darkls-usb-start.log
sh /jffs/xray/panel/service.sh ensure
sh /jffs/xray/watch.sh >> /tmp/darkls-watch.log 2>&1 < /dev/null &
USBEOF

sh -n "$PANEL/watch.sh.new"
sh -n "$PANEL/usb-mount-script.new"

# Stop the old watchdog before starting its integrated replacement.
if [ -s /tmp/darkls-watch.pid ]; then
  old_pid=$(cat /tmp/darkls-watch.pid)
  case "$old_pid" in ''|*[!0-9]*) old_pid= ;; esac
  if [ -n "$old_pid" ] && grep -q '/jffs/xray/watch.sh' "/proc/$old_pid/cmdline" 2>/dev/null; then
    kill "$old_pid"
    remaining=25
    while kill -0 "$old_pid" 2>/dev/null && [ "$remaining" -gt 0 ]; do
      sleep 1
      remaining=$((remaining - 1))
    done
    if kill -0 "$old_pid" 2>/dev/null; then
      echo 'Old watchdog did not stop; installation stopped'
      exit 1
    fi
  fi
fi

mv "$PANEL/watch.sh.new" "$ROOT/watch.sh"
mv "$PANEL/usb-mount-script.new" /jffs/scripts/usb-mount-script
chmod 700 "$ROOT/watch.sh" /jffs/scripts/usb-mount-script
sh "$PANEL/service.sh" stop
sleep 1
sh /jffs/scripts/usb-mount-script
sleep 2
sync

if [ -s /tmp/darkls-panel.pid ]; then
  panel_pid=$(cat /tmp/darkls-panel.pid)
  if kill -0 "$panel_pid" 2>/dev/null && grep -q '/jffs/xray/panel/darkls-panel' "/proc/$panel_pid/cmdline" 2>/dev/null; then
    echo 'Panel installed: http://192.168.50.1:8088'
    echo 'VPN routing was preserved. Keep the USB drive connected.'
    exit 0
  fi
fi
echo 'Panel did not start. Details:'
tail -n 30 /tmp/darkls-panel.log
exit 1
