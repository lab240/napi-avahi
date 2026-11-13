package napifind

// The raw search, for boards without napi-avahi: every address of the own subnets on TCP 22
// (but the default gateway and the DNS servers), and for those that answer the signs of a NAPI.
//
//   ssh   the banner: OpenSSH on Ubuntu/Debian (Armbian), bare OpenSSH_x.y or dropbear (Yocto) is Linux;
//         ROSSSH (MikroTik), OpenSSH_for_Windows are not NAPI
//   mac   locally administered (Armbian derives it from the chip serial: d2:..., 06:..., 6a:...)
//   name  the mDNS name by the IP (any avahi answers it, even without napi-avahi)
//   ports the NAPI web pages and MQTT: 8080 napiconfig (shown by name), 8090 donoff, 8086 influxdb,
//         3000 grafana, 1883
//   napi-avahi: the ordinary search runs alongside and marks the boards it found

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Host is one address that answered on port 22.
type Host struct {
	IP      string
	MAC     string
	SSH     string // the banner without "SSH-2.0-"
	Name    string // mDNS name by the IP, or from napi-avahi
	Ports   []int  // open ports of the NAPI services
	Board   *Board // found by napi-avahi
	Verdict string // "NAPI", "NAPI?", "Linux", "не NAPI"
	Signs   []string
}

var napiPorts = []int{8080, 8090, 8086, 3000, 1883}

// Scan checks the own subnets; progress, when set, is called as the addresses are done
// (from other goroutines).
func Scan(progress func(done, total int)) ([]Host, error) {
	ifs := interfaces()
	if len(ifs) == 0 {
		return nil, fmt.Errorf("нет сетевых интерфейсов с IPv4")
	}
	// The gateway and the DNS servers are not NAPI.
	infra := infraIPs()
	var addrs []string
	for _, a := range subnetHosts(ifs) {
		if what := infra[a.IP.String()]; what != "" {
			logf("skip %s: %s", a.IP, what)
			continue
		}
		addrs = append(addrs, a.IP.String())
	}
	logf("raw search: %d addresses, TCP 22", len(addrs))

	var boards []Board
	avahi := make(chan struct{})
	go func() { boards, _ = Find(3 * time.Second); close(avahi) }()

	var (
		mu    sync.Mutex
		hosts []*Host
		done  atomic.Int32
		wg    sync.WaitGroup
		jobs  = make(chan string)
	)
	for w := 0; w < 128; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				if h := probe(ip); h != nil {
					mu.Lock()
					hosts = append(hosts, h)
					mu.Unlock()
				}
				if n := done.Add(1); progress != nil {
					progress(int(n), len(addrs))
				}
			}
		}()
	}
	for _, a := range addrs {
		jobs <- a
	}
	close(jobs)
	wg.Wait()
	logf("port 22 open: %d", len(hosts))

	names := reverseNames(hosts)
	arp := arpTable()
	<-avahi
	byIP := map[string]*Board{}
	for i := range boards {
		for _, ip := range boards[i].IP {
			byIP[ip] = &boards[i]
		}
	}
	out := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		h.MAC = arp[h.IP]
		h.Name = names[h.IP]
		h.Board = byIP[h.IP]
		if h.Board != nil {
			h.Name = h.Board.Host
			if h.MAC == "" {
				h.MAC = h.Board.TXT["mac"]
			}
		}
		judge(h)
		logf("%s %s ssh=%q mac=%s ports=%v: %s %v", h.IP, h.Name, h.SSH, h.MAC, h.Ports, h.Verdict, h.Signs)
		out = append(out, *h)
	}
	rank := map[string]int{"NAPI": 0, "NAPI?": 1, "Linux": 2}
	sort.Slice(out, func(i, j int) bool {
		ri, ok := rank[out[i].Verdict]
		if !ok {
			ri = 3
		}
		rj, ok := rank[out[j].Verdict]
		if !ok {
			rj = 3
		}
		if ri != rj {
			return ri < rj
		}
		return ipLess(out[i].IP, out[j].IP)
	})
	return out, nil
}

// TCP 22, the SSH banner, then the NAPI ports.
func probe(ip string) *Host {
	c, err := net.DialTimeout("tcp4", net.JoinHostPort(ip, "22"), 800*time.Millisecond)
	if err != nil {
		return nil
	}
	h := &Host{IP: ip}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := c.Read(buf)
	c.Close()
	line := strings.TrimSpace(strings.SplitN(string(buf[:n]), "\n", 2)[0])
	h.SSH = strings.TrimPrefix(line, "SSH-2.0-")

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range napiPorts {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			if c, err := net.DialTimeout("tcp4", net.JoinHostPort(ip, fmt.Sprint(p)), 600*time.Millisecond); err == nil {
				c.Close()
				mu.Lock()
				h.Ports = append(h.Ports, p)
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	sort.Ints(h.Ports)
	return h
}

func judge(h *Host) {
	s := strings.ToLower(h.SSH)
	linux := strings.Contains(s, "ubuntu") || strings.Contains(s, "debian") || strings.Contains(s, "dropbear") ||
		strings.Contains(s, "armbian") || strings.Contains(s, "raspbian")
	notNapi := strings.Contains(s, "rosssh") || strings.Contains(s, "windows") || strings.Contains(s, "cisco")
	// Yocto (NapiLinux) builds OpenSSH without a distribution suffix: "OpenSSH_9.6".
	linux = linux || (!notNapi && strings.HasPrefix(s, "openssh_") && !strings.Contains(s, " "))
	napiName := strings.Contains(strings.ToLower(h.Name), "napi")
	if h.Board != nil {
		h.Signs = append(h.Signs, "napi-avahi")
	}
	if linux {
		h.Signs = append(h.Signs, "linux")
	}
	if la := localMAC(h.MAC); la {
		h.Signs = append(h.Signs, "mac")
	}
	if napiName {
		h.Signs = append(h.Signs, "имя")
	}
	for _, p := range h.Ports {
		if p == 8080 { // 22 and 8080: napiconfig, the web setup of NAPI
			h.Signs = append(h.Signs, "napiconfig")
		} else {
			h.Signs = append(h.Signs, fmt.Sprint(":", p))
		}
	}
	switch {
	case h.Board != nil:
		h.Verdict = "NAPI"
	case notNapi:
		h.Verdict = "не NAPI"
	case napiName, linux && len(h.Signs) > 1:
		h.Verdict = "NAPI?"
	case linux:
		h.Verdict = "Linux"
	default:
		h.Verdict = "?"
	}
}

// The locally administered bit: Armbian makes the NAPI MAC from the chip serial with it set.
func localMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	return err == nil && len(hw) > 0 && hw[0]&0x02 != 0 && hw[0]&0x01 == 0
}

// The mDNS name by the IP: a PTR question for d.c.b.a.in-addr.arpa straight to each host's port 5353.
func reverseNames(hosts []*Host) map[string]string {
	names := map[string]string{}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil || len(hosts) == 0 {
		return names
	}
	defer conn.Close()
	for _, h := range hosts {
		p := net.ParseIP(h.IP).To4()
		rev := fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", p[3], p[2], p[1], p[0])
		b, err := (&dnsmessage.Message{Questions: []dnsmessage.Question{q(rev, dnsmessage.TypePTR)}}).Pack()
		if err == nil {
			conn.WriteToUDP(b, &net.UDPAddr{IP: p, Port: 5353})
		}
	}
	buf := make([]byte, 9000)
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		conn.SetReadDeadline(end)
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		var m dnsmessage.Message
		if m.Unpack(buf[:n]) != nil {
			continue
		}
		for _, rr := range m.Answers {
			if ptr, ok := rr.Body.(*dnsmessage.PTRResource); ok {
				names[from.IP.String()] = strings.TrimSuffix(ptr.PTR.String(), ".")
			}
		}
	}
	return names
}

func ipLess(a, b string) bool {
	x, y := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if x == nil || y == nil {
		return a < b
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}
