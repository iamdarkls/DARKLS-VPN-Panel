#!/bin/sh
# The Ubuntu runner normally already has these tools. Avoid its mirror list
# when a package is missing: DNS failures there can stall the entire job.
set -eu
if command -v ip >/dev/null && command -v iptables >/dev/null; then
  exit 0
fi

sources=$(mktemp)
trap 'rm -f "$sources"' EXIT
chmod 0644 "$sources"
cat > "$sources" <<'SOURCES'
deb [signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] https://archive.ubuntu.com/ubuntu noble main
deb [signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] https://archive.ubuntu.com/ubuntu noble-updates main
deb [signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] https://security.ubuntu.com/ubuntu noble-security main
SOURCES

apt() {
  sudo apt-get -o "Dir::Etc::sourcelist=$sources" -o Dir::Etc::sourceparts=- \
    -o Acquire::Retries=0 -o Acquire::https::Timeout=10 "$@"
}
apt -o APT::Update::Error-Mode=any update
apt install -y --no-install-recommends iproute2 iptables
command -v ip >/dev/null && command -v iptables >/dev/null
