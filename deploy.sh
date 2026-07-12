#!/bin/sh
# Build inv-tui for ARMv6 and deploy it to the kiosk Pi.
#
# Pushes the binary, the systemd kiosk unit, and (if a local .env file with
# real credentials exists -- gitignored) /etc/inv-tui.env. Assumes key-based
# ssh to $PI_HOST.
set -e
cd "$(dirname "$0")"
PI_HOST=${PI_HOST:-root@192.168.4.193}
# Route through the VPS (which has Tailscale + accept-routes) so deploys work
# from anywhere, not just the local network.
SSH_OPTS="-o StrictHostKeyChecking=accept-new -o ProxyJump=vps"

# push() copies a file over plain ssh stdin -- the Pi's minimal Raspbian has
# neither sftp-server nor a remote scp binary.
push() {
	ssh $SSH_OPTS "$PI_HOST" "cat > '$2'" < "$1"
}

GOOS=linux GOARCH=arm GOARM=6 go build -ldflags="-s -w" -o inv-tui-armv6 .
push inv-tui-armv6 /tmp/inv-tui
push inv-tui.service /etc/systemd/system/inv-tui.service
if [ -f .env ]; then
	push .env /etc/inv-tui.env
fi

ssh $SSH_OPTS "$PI_HOST" '
	set -e
	install -m 755 /tmp/inv-tui /usr/local/bin/inv-tui
	[ -f /etc/inv-tui.env ] && chmod 600 /etc/inv-tui.env
	inv-tui -check
	systemctl daemon-reload
	systemctl disable getty@tty1 2>/dev/null || true
	systemctl enable inv-tui 2>/dev/null
	systemctl restart inv-tui
	sleep 2
	systemctl is-active inv-tui
'
echo "deployed to $PI_HOST"
