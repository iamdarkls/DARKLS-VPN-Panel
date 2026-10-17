#!/bin/sh
set -e
BASE=/jffs/xray
PID=/tmp/dnsmasq-vpn.pid
# Stop only our helper; never touch the stock DHCP/DNS service.
if [ -s "$PID" ]; then
  dns_pid=$(cat "$PID")
  case "$dns_pid" in ''|*[!0-9]*) echo 'Invalid DNS helper PID'; exit 1 ;; esac
  if kill -0 "$dns_pid" 2>/dev/null; then
    grep -q 'dnsmasq-vpn.conf' "/proc/$dns_pid/cmdline" || { echo 'Unexpected DNS process'; exit 1; }
    kill "$dns_pid"
    attempts=0
    while kill -0 "$dns_pid" 2>/dev/null; do
      attempts=$((attempts + 1))
      [ "$attempts" -le 5 ] || { echo 'DNS helper did not stop'; exit 1; }
      sleep 1
    done
  fi
fi
rm -f "$PID"
dnsmasq --conf-file="$BASE/dnsmasq-vpn.conf"
test -s "$PID"
dns_pid=$(cat "$PID")
kill -0 "$dns_pid"
grep -q 'dnsmasq-vpn.conf' "/proc/$dns_pid/cmdline"
echo 'DNS lists reloaded; Xray and cached VPN routes preserved'
