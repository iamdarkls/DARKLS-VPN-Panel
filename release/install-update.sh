#!/bin/sh
set -e
umask 077
PKG=$(CDPATH= cd "$(dirname "$0")" && pwd)
BASE=/jffs/xray
PANEL=$BASE/panel
ROLL=$PANEL/update-rollback
CHANGED=0
LOG_RESTART=0
grep -q '^Uid:[[:space:]]*0[[:space:]]' /proc/self/status || exit 1
grep -q 'DARKLS_PANEL_INTEGRATION' "$BASE/watch.sh" || { echo 'Install panel first'; exit 1; }
test -s "$PANEL/auth.json"
[ ! -f "$PANEL/apply-journal.json" ] || { echo "Finish or recover pending settings operation first"; exit 1; }
chmod 700 "$PKG/darkls-panel"
TARGET=$("$PKG/darkls-panel" --version)
"$PKG/darkls-panel" --verify-package "$PKG"
for name in install-update.sh reload-dns.sh devices-sync.sh watch.sh; do sh -n "$PKG/$name"; done
mkdir -p "$ROLL"
chmod 700 "$ROLL"
for name in vpn.sh watch.sh domains.txt dnsmasq-vpn.conf telegram-networks.txt; do cp -p "$BASE/$name" "$ROLL/$name"; done
cp -p "$PANEL/state.json" "$ROLL/state.json"
rm -f "$ROLL/instagram-fbsbx-migrated" "$ROLL/instagram-fbsbx-migrated.absent"
if [ -f "$PANEL/instagram-fbsbx-migrated" ]; then cp -p "$PANEL/instagram-fbsbx-migrated" "$ROLL/instagram-fbsbx-migrated"; else touch "$ROLL/instagram-fbsbx-migrated.absent"; fi
for name in reload-dns.sh devices-sync.sh; do
  rm -f "$ROLL/$name" "$ROLL/$name.absent"
  if [ -f "$PANEL/$name" ]; then cp -p "$PANEL/$name" "$ROLL/$name"; else touch "$ROLL/$name.absent"; fi
done
cp "$PKG/darkls-panel" "$PANEL/darkls-panel.new"
chmod 700 "$PANEL/darkls-panel.new"
for name in reload-dns.sh devices-sync.sh; do cp "$PKG/$name" "$PANEL/$name.new"; chmod 700 "$PANEL/$name.new"; done
cp "$PKG/watch.sh" "$BASE/watch.sh.new"
if grep -q 'DARKLS_DEVICE_POLICY_HOOK' "$BASE/vpn.sh"; then
  cp -p "$BASE/vpn.sh" "$BASE/vpn.sh.new"
else
  grep -q '^iptables -t filter -I INPUT' "$BASE/vpn.sh" || { echo 'Unsupported VPN script'; exit 1; }
  awk '/^iptables -t filter -I INPUT/ { print "# DARKLS_DEVICE_POLICY_HOOK"; print "sh \"$BASE/panel/devices-sync.sh\"" } {print}' "$BASE/vpn.sh" > "$BASE/vpn.sh.new"
fi
if grep -q ' > /tmp/xray.log' "$BASE/vpn.sh.new"; then LOG_RESTART=1; fi
# Use append descriptors so in-place log trimming cannot create sparse holes.
sed 's/ > \/tmp\/xray.log/ >> \/tmp\/xray.log/' "$BASE/vpn.sh.new" > "$BASE/vpn.sh.logfix"
mv "$BASE/vpn.sh.logfix" "$BASE/vpn.sh.new"
sh -n "$BASE/vpn.sh.new"
sh -n "$BASE/watch.sh.new"

stop_panel() {
  sh "$PANEL/service.sh" stop
  if [ -s /tmp/darkls-panel.pid ]; then
    panel_pid=$(cat /tmp/darkls-panel.pid)
    case "$panel_pid" in ''|*[!0-9]*) return 1 ;; esac
    for attempt in 1 2 3 4 5 6 7 8 9 10; do
      if ! kill -0 "$panel_pid" 2>/dev/null; then return 0; fi
      sleep 1
    done
    return 1
  fi
}
ready() {
  [ -s /tmp/darkls-panel.pid ] || return 1
  current_pid=$(cat /tmp/darkls-panel.pid)
  case "$current_pid" in ''|*[!0-9]*) return 1 ;; esac
  kill -0 "$current_pid" 2>/dev/null && grep -q '/jffs/xray/panel/darkls-panel' "/proc/$current_pid/cmdline" 2>/dev/null
}
start_and_check() {
  sh /jffs/scripts/usb-mount-script || return 1
  for attempt in 1 2 3 4 5 6 7 8 9 10; do ready && return 0; sleep 1; done
  return 1
}
rollback() {
  stop_panel || true
  if [ -f "$PANEL/darkls-panel.previous" ]; then mv "$PANEL/darkls-panel.previous" "$PANEL/darkls-panel"; fi
  for name in vpn.sh watch.sh domains.txt dnsmasq-vpn.conf telegram-networks.txt; do cp -p "$ROLL/$name" "$BASE/$name"; done
  cp -p "$ROLL/state.json" "$PANEL/state.json"
  for name in reload-dns.sh devices-sync.sh; do
    if [ -f "$ROLL/$name.absent" ]; then rm -f "$PANEL/$name"; else cp -p "$ROLL/$name" "$PANEL/$name"; fi
  done
  rm -f /tmp/darkls-device-digest
  if [ -f "$ROLL/instagram-fbsbx-migrated.absent" ]; then rm -f "$PANEL/instagram-fbsbx-migrated"; else cp -p "$ROLL/instagram-fbsbx-migrated" "$PANEL/instagram-fbsbx-migrated"; fi
  if [ ! -f "$BASE/autostart.disabled" ]; then sh "$PANEL/reload-dns.sh" || true; fi
  if start_and_check; then touch "$PKG/rollback.ok"; echo 'Previous panel restored'; else echo 'Rollback startup failed; see /tmp/darkls-panel.log'; fi
}
finish() {
  result=$?
  trap - 0
  set +e
  if [ "$result" -eq 0 ]; then start_and_check || result=1; fi
  if [ "$result" -ne 0 ]; then
    if [ "$CHANGED" -eq 1 ]; then rollback; else start_and_check; fi
    echo 'Update failed; package retained for diagnosis'
    tail -n 20 /tmp/darkls-panel.log
    exit "$result"
  fi
  echo "Panel $TARGET installed. Password and device/site selection preserved."
  echo 'Refresh http://192.168.50.1:8088 with Ctrl+F5.'
  if [ ! -f "$BASE/autostart.disabled" ]; then
    (trap '' HUP; "$PANEL/darkls-panel" --check-vpn > /tmp/darkls-vpn-probe.log 2>&1) < /dev/null &
  fi
  rm -rf /tmp/darkls-panel-install /tmp/darkls-monitor-update /tmp/darkls-panel-update-v1.0.3
  rm -f /tmp/xray-arm64.zip /tmp/asusware-usbmount.zip
  if [ "$PKG" = /tmp/darkls-panel-update ]; then rm -rf "$PKG"; fi
  exit 0
}
trap finish 0
if [ -s /tmp/darkls-watch.pid ]; then
  watch_pid=$(cat /tmp/darkls-watch.pid)
  case "$watch_pid" in ''|*[!0-9]*) exit 1 ;; esac
  if grep -q '/jffs/xray/watch.sh' "/proc/$watch_pid/cmdline" 2>/dev/null; then
    kill "$watch_pid" 2>/dev/null || true
    remaining=60
    while kill -0 "$watch_pid" 2>/dev/null; do
      [ "$remaining" -gt 0 ] || exit 1
      sleep 1
      remaining=$((remaining - 1))
    done
  fi
fi
stop_panel
# Keep the immediately preceding installed build, including for repair installs.
cp -p "$PANEL/darkls-panel" "$PANEL/darkls-panel.previous"
mv "$PANEL/darkls-panel.new" "$PANEL/darkls-panel"
CHANGED=1
for name in reload-dns.sh devices-sync.sh; do mv "$PANEL/$name.new" "$PANEL/$name"; done
mv "$BASE/watch.sh.new" "$BASE/watch.sh"
mv "$BASE/vpn.sh.new" "$BASE/vpn.sh"
chmod 700 "$BASE/watch.sh" "$BASE/vpn.sh"
"$PANEL/darkls-panel" --update-catalog
"$PANEL/darkls-panel" --repair-instagram
"$PANEL/darkls-panel" --sync-devices --force-devices
if [ "$LOG_RESTART" -eq 1 ] && [ ! -f "$BASE/autostart.disabled" ]; then
  echo 'Reopening Xray log in append mode; VPN connections will reconnect once'
  sh "$BASE/vpn.sh" start
fi
sync
