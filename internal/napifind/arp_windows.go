package napifind

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

var pGetIpNetTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetIpNetTable")

// IPv4 -> MAC from GetIpNetTable (MIB_IPNETROW: index, len, addr[8], ip, type; 24 bytes).
func arpTable() map[string]string {
	out := map[string]string{}
	var size uint32
	pGetIpNetTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return out
	}
	buf := make([]byte, size)
	if r, _, _ := pGetIpNetTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0); r != 0 {
		return out
	}
	n := int(binary.LittleEndian.Uint32(buf))
	for i := 0; i < n && 4+(i+1)*24 <= len(buf); i++ {
		row := buf[4+i*24 : 4+(i+1)*24]
		l := binary.LittleEndian.Uint32(row[4:])
		typ := binary.LittleEndian.Uint32(row[20:])
		if l != 6 || typ == 2 { // 2: invalid
			continue
		}
		ip := net.IP(row[16:20]).String()
		m := row[8:14]
		out[ip] = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
	}
	return out
}
