package napifind

import (
	"os"
	"strings"
)

// IPv4 -> MAC from the kernel neighbour table.
func arpTable() map[string]string {
	out := map[string]string{}
	b, _ := os.ReadFile("/proc/net/arp")
	for _, l := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) >= 4 && f[3] != "00:00:00:00:00:00" {
			out[f[0]] = f[3]
		}
	}
	return out
}
