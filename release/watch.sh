#!/bin/sh
# DARKLS_PANEL_INTEGRATION v1
# DARKLS_VPN_HTTPS_MONITOR v1
trap '' HUP
LOCK=/tmp/darkls-watch.lock
mkdir "$LOCK" 2>/dev/null || exit 0
echo $$ > /tmp/darkls-watch.pid
trap 'rm -f /tmp/darkls-watch.pid; rmdir "$LOCK"' 0
trap 'exit 0' TERM INT
BASE=/jffs/xray
BIN=$BASE/panel/darkls-panel
failures=0
last_probe=0
last_recovery=-180
last_deep=0
last_dns_failure=0
last_maintenance=-60

healthy() {
  REASON='Xray process missing'
  pidof xray >/dev/null || return 1
  REASON='VPN DNS process missing'
  [ -s /tmp/dnsmasq-vpn.pid ] || return 1
  dns_pid=$(cat /tmp/dnsmasq-vpn.pid)
  kill -0 "$dns_pid" 2>/dev/null || return 1
  grep -q 'dnsmasq-vpn.conf' "/proc/$dns_pid/cmdline" || return 1
  REASON='NAT hook missing'
  iptables -t nat -C PREROUTING -i br0 -s 192.168.50.0/24 -j DV_NAT 2>/dev/null || return 1
  REASON='UDP hook missing'
  iptables -t mangle -C PREROUTING -i br0 -s 192.168.50.0/24 -j DV_UDP 2>/dev/null || return 1
  REASON='INPUT hook missing'
  iptables -t filter -C INPUT -i br0 -s 192.168.50.0/24 -j DV_IN 2>/dev/null || return 1
  REASON='Policy rule missing'
  ip rule show | grep -q 'fwmark 0x1000000/0x1000000.*lookup 100' || return 1
  REASON='Local route missing'
  ip route show table 100 | grep -q '^local default.*dev lo' || return 1
}

recover() {
  date
  echo "Checking failed VPN components: $REASON"
  "$BIN" --watch-recover --reason "$REASON" || echo 'Recovery failed; will retry'
  last_recovery=$(awk '{print int($1)}' /proc/uptime)
  last_probe=$last_recovery
  failures=0
}

while :; do
  sh "$BASE/panel/service.sh" ensure
  now=$(awk '{print int($1)}' /proc/uptime)
  # Log limits need one pass per minute, not another Go process every tick.
  if [ $((now - last_maintenance)) -ge 60 ]; then
    "$BIN" --maintain-logs
    last_maintenance=$now
  fi
  if [ -f "$BASE/autostart.disabled" ]; then
    failures=0
  elif ! healthy; then
    # Boot-time firewall rebuilds must be repaired promptly.
    if [ $((now - last_recovery)) -ge 15 ]; then recover; fi
  elif [ $((now - last_deep)) -ge 60 ]; then
    last_deep=$now
    if "$BIN" --check-router > /tmp/darkls-router-probe.log 2>&1; then
      last_dns_failure=0
    else
      date; cat /tmp/darkls-router-probe.log
      # Require two failed deep checks; a temporary upstream DNS failure is not
      # enough to interrupt working connections. Structural repairs stay prompt.
      if [ "$last_dns_failure" -eq 1 ] && [ $((now - last_recovery)) -ge 180 ]; then
        REASON=$(head -n 1 /tmp/darkls-router-probe.log)
        recover
        last_dns_failure=0
      else last_dns_failure=1; fi
    fi
  elif [ $((now - last_probe)) -ge 30 ] && [ "$(date +%Y)" -ge 2025 ]; then
    last_probe=$now
    if "$BIN" --check-vpn > /tmp/darkls-vpn-probe.log 2>&1; then
      if [ "$failures" -gt 0 ]; then date; echo 'VPN HTTPS recovered'; fi
      failures=0
    else
      failures=$((failures + 1))
      date
      echo "VPN HTTPS failed, consecutive checks: $failures"
      cat /tmp/darkls-vpn-probe.log
      if [ "$failures" -eq 1 ]; then "$BIN" --check-components > /tmp/darkls-components.log 2>&1; fi
      if [ "$failures" -ge 2 ] && [ $((now - last_recovery)) -ge 180 ]; then
        REASON='HTTPS traffic failed on both probe destinations twice'
        recover
      fi
    fi
  fi
  sleep 15
done
