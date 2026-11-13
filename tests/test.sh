#!/bin/sh
# napi-avahi on a fake board: name, TXT records, role and name settings, the avahi files.
set -e
cd "$(dirname "$0")/.."
rm -rf build/test-root
mkdir -p build/test-root
T=$(pwd)/build/test-root
fail=0
check() { # what expected actual
	if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: want [$2] got [$3]"; fail=1; fi
}

mkdir -p "$T/proc/device-tree" "$T/proc/sys/kernel" "$T/sys/class/net/end0" "$T/sys/class/net/lo" "$T/etc/avahi/services"
printf 'radxa,rockpis\0rockchip,rk3308\0' >"$T/proc/device-tree/compatible"
printf 'NAPI-C \0' >"$T/proc/device-tree/model"
printf '9e098bbcae8b6599\0' >"$T/proc/device-tree/serial-number"
echo 6.18.62-current-rockchip64 >"$T/proc/sys/kernel/osrelease"
echo d2:a9:fd:81:b9:df >"$T/sys/class/net/end0/address"
echo 00:00:00:00:00:00 >"$T/sys/class/net/lo/address"
echo 'PRETTY_NAME="Armbian 26.11.0-trunk.117 noble"' >"$T/etc/os-release"
printf '[server]\n#host-name=foo\nuse-ipv4=yes\n' >"$T/etc/avahi/avahi-daemon.conf"
export NAPI_AVAHI_ROOT="$T"
A=usr/bin/napi-avahi

check name "name=napi3308-$(printf 9e098bbcae8b6599 | md5sum | cut -c1-4).local" "$(sh $A show | grep '^name=')"
check model "model=NAPI-C" "$(sh $A show | grep '^model=')"
check os "os=armbian-26.11.0-trunk.117" "$(sh $A show | grep '^os=')"
check serial "serial=9e098bbcae8b6599" "$(sh $A show | grep '^serial=')"
check kernel "kernel=6.18.62-current-rockchip64" "$(sh $A show | grep '^kernel=')"

sh $A update >/dev/null
check host-name "host-name=napi3308-$(printf 9e098bbcae8b6599 | md5sum | cut -c1-4)" "$(grep '^host-name=' "$T/etc/avahi/avahi-daemon.conf")"
check service-type 1 "$(grep -c '<type>_napi._tcp</type>' "$T/etc/avahi/services/napi.service")"
check txt-soc 1 "$(grep -c '<txt-record>soc=rk3308</txt-record>' "$T/etc/avahi/services/napi.service")"

sh $A role 'donoff,mgate' >/dev/null
check role "ROLE=donoff,mgate" "$(grep '^ROLE=' "$T/etc/napi/avahi.conf")"
check txt-role 1 "$(grep -c '<txt-record>role=donoff,mgate</txt-record>' "$T/etc/avahi/services/napi.service")"

sh $A name kotelnaya >/dev/null
check own-name "host-name=kotelnaya" "$(grep '^host-name=' "$T/etc/avahi/avahi-daemon.conf")"
check one-host-name 1 "$(grep -c '^host-name=' "$T/etc/avahi/avahi-daemon.conf")"
if sh $A name 'Bad_Name' 2>/dev/null; then check bad-name refused accepted; else check bad-name refused refused; fi
sh $A name '' >/dev/null
check name-back "host-name=napi3308-$(printf 9e098bbcae8b6599 | md5sum | cut -c1-4)" "$(grep '^host-name=' "$T/etc/avahi/avahi-daemon.conf")"

# A board with the same MAC and serial tail (seen on the bench): another name
printf 'b8d0bb10ae8b6599\0' >"$T/proc/device-tree/serial-number"
check same-mac-other-name "name=napi3308-$(printf b8d0bb10ae8b6599 | md5sum | cut -c1-4).local" "$(sh $A show | grep '^name=')"

# RK3568 NAPI2 with eth0 only, no serial (the MAC end then), NapiLinux
rm "$T/proc/device-tree/serial-number"
rm -r "$T/sys/class/net/end0"
mkdir -p "$T/sys/class/net/eth0"
echo 5a:11:22:33:1c:2e >"$T/sys/class/net/eth0/address"
printf 'napilab,napi2\0rockchip,rk3568\0' >"$T/proc/device-tree/compatible"
echo 1.4 >"$T/etc/napilinux-release"
check napi2 "name=napi3568-1c2e.local" "$(sh $A show | grep '^name=')"
check napilinux "os=napilinux-1.4" "$(sh $A show | grep '^os=')"

exit $fail
