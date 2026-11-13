SUMMARY = "Find NAPI boards on the LAN without knowing their IP (mDNS)"
DESCRIPTION = "napi-avahi gives the board the avahi (mDNS) name \
napi<SoC>-<id>.local (id: 4 hex of md5 of the chip serial) and announces \
the _napi._tcp service with TXT fields model, soc, os, role, mac, serial, \
kernel. napi-finder.exe (Windows) and napi-discover (Linux) list the boards. \
The system hostname is not changed."
HOMEPAGE = "https://github.com/lab240/napi-avahi"
SECTION = "net"

LICENSE = "MIT"
LIC_FILES_CHKSUM = "file://LICENSE;md5=6c03ec114e3986e4e8e203bc875e42cd"

SRC_URI = "git://github.com/lab240/napi-avahi.git;protocol=https;branch=main"
# Development: follow the branch. For a release, pin a commit instead:
#   SRCREV = "<commit>"
SRCREV = "${AUTOREV}"
PV = "0.1.2+git"

S = "${WORKDIR}/git"

inherit systemd allarch

# A shell script: nothing to configure or compile.
do_configure[noexec] = "1"
do_compile[noexec] = "1"

do_install() {
    oe_runmake install DESTDIR=${D} \
        BINDIR=${bindir} \
        SYSTEMDUNITDIR=${systemd_system_unitdir} \
        SYSCONFDIR=${sysconfdir}
}

SYSTEMD_SERVICE:${PN} = "napi-avahi.service"
SYSTEMD_AUTO_ENABLE = "enable"

# "napi-avahi role/name" edit it; keep the user's copy on upgrades.
CONFFILES:${PN} = "${sysconfdir}/napi/avahi.conf"

# avahi-daemon: the name and the service; avahi-utils: avahi-set-host-name,
# avahi-resolve-address (applying without a restart) and avahi-browse (list).
RDEPENDS:${PN} = "avahi-daemon avahi-utils"

# Stop announcing on removal: the service file and our host-name line.
pkg_postrm:${PN}() {
    rm -f $D${sysconfdir}/avahi/services/napi.service
    if [ -f $D${sysconfdir}/avahi/avahi-daemon.conf ]; then
        sed -i '/^host-name=/d' $D${sysconfdir}/avahi/avahi-daemon.conf
    fi
    if [ -z "$D" ] && [ -d /run/systemd/system ]; then
        systemctl try-restart avahi-daemon.service || true
    fi
}
