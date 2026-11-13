package napifind

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"
)

// The default gateways (/proc/net/route) and the DNS servers (resolv.conf; behind systemd-resolved
// the real ones are in /run/systemd/resolve/resolv.conf): not NAPI, the raw search skips them.
func infraIPs() map[string]string {
	out := map[string]string{}
	b, _ := os.ReadFile("/proc/net/route")
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		g, err := hex.DecodeString(f[2])
		if err != nil || len(g) != 4 {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, binary.LittleEndian.Uint32(g))
		if !ip.IsUnspecified() {
			out[ip.String()] = "шлюз"
		}
	}
	for _, p := range []string{"/etc/resolv.conf", "/run/systemd/resolve/resolv.conf"} {
		b, _ := os.ReadFile(p)
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && f[0] == "nameserver" {
				if ip := net.ParseIP(f[1]).To4(); ip != nil && !ip.IsLoopback() {
					if out[ip.String()] == "" {
						out[ip.String()] = "DNS"
					}
				}
			}
		}
	}
	return out
}
