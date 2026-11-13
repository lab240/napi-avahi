#!/bin/sh
# napi-avahi: remove what install.sh put in: the service, the script, the avahi service file and
# our host-name in avahi-daemon.conf. The settings /etc/napi/avahi.conf stay; --purge removes them.
# NAPI_AVAHI_ROOT=<dir> works on a copy of / and skips systemctl (tests).
set -eu
R=${NAPI_AVAHI_ROOT:-}

if [ -z "$R" ] && [ "$(id -u)" != 0 ]; then
	echo "napi-avahi: run as root" >&2
	exit 1
fi
if [ -z "$R" ]; then
	systemctl disable --now napi-avahi.service 2>/dev/null || true
fi
rm -f "$R/usr/bin/napi-avahi" "$R/usr/lib/systemd/system/napi-avahi.service" "$R/lib/systemd/system/napi-avahi.service"
rm -f "$R/etc/avahi/services/napi.service"
if [ -f "$R/etc/avahi/avahi-daemon.conf" ]; then
	sed -i '/^host-name=/d' "$R/etc/avahi/avahi-daemon.conf"
fi
if [ "${1:-}" = "--purge" ]; then
	rm -f "$R/etc/napi/avahi.conf"
	rmdir "$R/etc/napi" 2>/dev/null || true
fi
if [ -z "$R" ]; then
	systemctl daemon-reload
	systemctl try-restart avahi-daemon.service 2>/dev/null || true
	echo "napi-avahi removed"
fi
