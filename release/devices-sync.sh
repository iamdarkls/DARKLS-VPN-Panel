#!/bin/sh
# Called by vpn.sh after chains are created and before LAN hooks attach.
exec /jffs/xray/panel/darkls-panel --sync-devices --force-devices
