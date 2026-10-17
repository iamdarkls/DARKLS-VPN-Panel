#!/bin/sh
# Called before legacy hooks attach; group policy runs at the end of vpn.sh.
set -e
exec /jffs/xray/panel/darkls-panel --sync-devices --force-devices
