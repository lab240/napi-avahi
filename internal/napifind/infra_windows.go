package napifind

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The default gateways and the DNS servers of the adapters (GetAdaptersAddresses):
// not NAPI, the raw search skips them.
func infraIPs() map[string]string {
	out := map[string]string{}
	size := uint32(16 << 10)
	var buf []byte
	for i := 0; i < 3; i++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_INET, 0x80, 0, // GAA_FLAG_INCLUDE_GATEWAYS
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return out
		}
		buf = nil
	}
	if buf == nil {
		return out
	}
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			if ip := d.Address.IP().To4(); ip != nil {
				out[ip.String()] = "DNS"
			}
		}
		for g := a.FirstGatewayAddress; g != nil; g = g.Next {
			if ip := g.Address.IP().To4(); ip != nil && !ip.IsUnspecified() {
				out[ip.String()] = "шлюз"
			}
		}
	}
	return out
}
