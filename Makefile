# napi-avahi: the board side is a shell script (Debian package, Architecture: all);
# the finders for PCs are Go (make tools).
VERSION = 0.1.2
DEB_VERSION ?= $(VERSION)
DEB = build/napi-avahi_$(DEB_VERSION)_all.deb
GO ?= go

all: test deb

test:
	sh -n usr/bin/napi-avahi
	for f in packaging/deb/postinst packaging/deb/prerm packaging/deb/postrm install.sh uninstall.sh; do sh -n $$f; done
	sh tests/test.sh

deb:
	rm -rf build/deb build/napi-avahi_*.deb
	install -D -m 755 usr/bin/napi-avahi build/deb/usr/bin/napi-avahi
	install -D -m 644 lib/systemd/system/napi-avahi.service build/deb/usr/lib/systemd/system/napi-avahi.service
	install -D -m 644 etc/napi/avahi.conf build/deb/usr/share/napi-avahi/avahi.conf
	install -D -m 644 README.md build/deb/usr/share/doc/napi-avahi/README.md
	install -D -m 644 LICENSE build/deb/usr/share/doc/napi-avahi/copyright
	install -d build/deb/DEBIAN
	for f in postinst prerm postrm; do install -m 755 packaging/deb/$$f build/deb/DEBIAN/$$f; done
	sed -e 's|@VERSION@|$(DEB_VERSION)|' \
	    -e "s|@SIZE@|$$(du -sk --exclude=DEBIAN build/deb | cut -f1)|" \
	    packaging/deb/control.in > build/deb/DEBIAN/control
	dpkg-deb --root-owner-group --build build/deb $(DEB)

# For NapiLinux without the recipe: build/napi-avahi-<version>.tar.gz with install.sh / uninstall.sh
TAR = napi-avahi-$(DEB_VERSION)
tar:
	rm -rf build/$(TAR) build/$(TAR).tar.gz
	mkdir -p build/$(TAR)
	cp -r --parents usr/bin/napi-avahi lib/systemd/system/napi-avahi.service etc/napi/avahi.conf \
	    install.sh uninstall.sh README.md LICENSE build/$(TAR)/
	tar -C build --owner=0 --group=0 -czf build/$(TAR).tar.gz $(TAR)

# For Yocto and manual installs; the deb target lays out the same files.
DESTDIR ?=
BINDIR ?= /usr/bin
SYSTEMDUNITDIR ?= /usr/lib/systemd/system
SYSCONFDIR ?= /etc
install:
	install -D -m 755 usr/bin/napi-avahi $(DESTDIR)$(BINDIR)/napi-avahi
	install -D -m 644 lib/systemd/system/napi-avahi.service $(DESTDIR)$(SYSTEMDUNITDIR)/napi-avahi.service
	install -D -m 644 etc/napi/avahi.conf $(DESTDIR)$(SYSCONFDIR)/napi/avahi.conf

# napi-finder.exe (Windows: window, --console; the icon, version and manifest from tools/mkres),
# napi-finder for Linux (the page in the browser, --console, --serial), napi-discover (Linux console)
tools:
	mkdir -p build
	$(GO) run ./tools/mkres -ico assets/napi.ico -version $(DEB_VERSION) -o cmd/napi-finder/rsrc_windows_amd64.syso
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "-s -w -X main.version=$(DEB_VERSION)" -o build/napi-finder.exe ./cmd/napi-finder
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "-s -w -X main.version=$(DEB_VERSION)" -o build/napi-finder-linux-amd64 ./cmd/napi-finder
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "-s -w -X main.version=$(DEB_VERSION)" -o build/napi-finder-linux-arm64 ./cmd/napi-finder
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o build/napi-discover-linux-amd64 ./cmd/napi-discover
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o build/napi-discover-linux-arm64 ./cmd/napi-discover

clean:
	rm -rf build

.PHONY: all test deb install tar tools clean
