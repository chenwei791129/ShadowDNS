#!/bin/sh
# Create shadowdns system user and group if they don't exist.
if ! getent group shadowdns >/dev/null 2>&1; then
    groupadd --system shadowdns
fi
if ! getent passwd shadowdns >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin --gid shadowdns shadowdns
fi

# Create log directory with correct ownership.
install -d -o shadowdns -g shadowdns -m 0750 /var/log/shadowdns

# Reload systemd unit files after install or upgrade.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload ||:
fi
