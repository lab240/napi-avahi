#!/bin/sh
# napi-avahi: install on a running system without the package manager (NapiLinux images without
# the napi-avahi recipe). Run from the unpacked archive as root: sh install.sh
# Needs avahi-daemon in the image; avahi-utils (avahi-set-host-name, avahi-browse) are optional.
# An existing /etc/napi/avahi.conf (role, name) is kept. Remove: sh uninstall.sh
# NAPI_AVAHI_ROOT=<dir> installs into a copy of / and skips systemctl (tests).
set -eu
B="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
R=${NAPI_AVAHI_ROOT:-}

if [ -z "$R" ] && [ "$(id -u)" != 0 ]; then
	echo "napi-avahi: run as root" >&2
	exit 1
fi
if [ -z "$R" ] && ! command -v avahi-daemon >/dev/null 2>&1 && [ ! -x /usr/sbin/avahi-daemon ]; then
	echo "napi-avahi: avahi-daemon is not in this system; add it to the image (avahi-daemon, avahi-utils)" >&2
	exit 1
fi
if [ -z "$R" ] && ! command -v avahi-set-host-name >/dev/null 2>&1; then
	echo "napi-avahi: no avahi-utils: a new name applies by restarting avahi, 'napi-avahi list' does not work"
fi

# The unit directory of this system.
U=/usr/lib/systemd/system
[ -d "$R$U" ] || [ ! -d "$R/lib/systemd/system" ] || U=/lib/systemd/system

mkdir -p "$R/usr/bin" "$R$U" "$R/etc/napi" "$R/etc/avahi/services"
cp "$B/usr/bin/napi-avahi" "$R/usr/bin/napi-avahi"
chmod 755 "$R/usr/bin/napi-avahi"
cp "$B/lib/systemd/system/napi-avahi.service" "$R$U/napi-avahi.service"
chmod 644 "$R$U/napi-avahi.service"
if [ -e "$R/etc/napi/avahi.conf" ]; then
	echo "Existing /etc/napi/avahi.conf kept"
else
	cp "$B/etc/napi/avahi.conf" "$R/etc/napi/avahi.conf"
	chmod 644 "$R/etc/napi/avahi.conf"
fi

if [ -n "$R" ]; then
	NAPI_AVAHI_ROOT="$R" sh "$R/usr/bin/napi-avahi" update >/dev/null
	exit 0
fi
systemctl daemon-reload
systemctl enable napi-avahi.service
systemctl enable avahi-daemon.service 2>/dev/null || true
systemctl restart napi-avahi.service
systemctl start avahi-daemon.service 2>/dev/null || true
echo "napi-avahi installed:"
napi-avahi show
