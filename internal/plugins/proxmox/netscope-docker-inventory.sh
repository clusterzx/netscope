#!/bin/sh
# netscope-docker-inventory – read-only Docker inventory of the LXC containers on a
# Proxmox VE node for NetScope (plugin "Proxmox VE", option "Docker in LXC-Containern").
#
# Lists the running containers with `pct list` and runs `docker version`, `docker ps`
# and `docker images` inside each one via `pct exec`. Nothing is changed.
#
# Install on every node as root:
#   install -m 0755 netscope-docker-inventory /usr/local/sbin/netscope-docker-inventory
# and restrict NetScope's key to this script in /root/.ssh/authorized_keys (one line):
#   command="/usr/local/sbin/netscope-docker-inventory",from="<NetScope-IP>",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA… netscope
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
T=20

run() {
	timeout "$T" pct exec "$ID" -- "$@" </dev/null 2>/dev/null
}

echo "#netscope-docker-inventory 1"
echo "#node $(hostname)"
if ! command -v pct >/dev/null 2>&1; then
	echo "#error pct nicht gefunden – kein Proxmox-VE-Host"
	exit 1
fi
for ID in $(pct list 2>/dev/null | awk 'NR > 1 && $2 == "running" { print $1 }'); do
	echo "#lxc $ID"
	if ! run sh -c 'command -v docker' >/dev/null; then
		echo "#nodocker"
		continue
	fi
	echo "#version"
	run docker version --format '{{.Server.Version}}' || echo "#error version $?"
	echo "#ps"
	run docker ps -a --no-trunc --format '{{json .}}' || echo "#error ps $?"
	echo "#images"
	run docker images --no-trunc --format '{{json .}}' || echo "#error images $?"
done
echo "#end"
