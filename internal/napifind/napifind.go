// Package napifind finds the NAPI boards on the LAN: the mDNS service _napi._tcp that
// the napi-avahi package announces, with its TXT fields (model, soc, os, role, mac).
//
// It asks with "legacy unicast" mDNS queries (from an ordinary UDP port, not 5353), so it does not
// fight the mDNS service of the system for the port: the answers come straight back to it.
// Besides the multicast query it asks every address of its own subnets directly (port 5353):
// some Wi-Fi access points do not pass multicast from the wired side to their clients, so a NAPI
// on Wi-Fi never hears the multicast query, but avahi answers a unicast one.
package napifind

import (
	"encoding/binary"
	"errors"
	"net"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

const service = "_napi._tcp.local."

var mdnsAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Board is one NAPI that answered.
type Board struct {
	Host string            // napi3308-79df.local
	IP   []string          // IPv4
	TXT  map[string]string // model, soc, os, role, mac, kernel
}

type found struct {
	instance, host string
	txt            map[string]string
}

// Find listens for the given time and returns the boards sorted by name.
// Log, when set, gets what the search does (napi-discover -v).
var Log func(format string, a ...any)

func logf(format string, a ...any) {
	if Log != nil {
		Log(format, a...)
	}
}

func Find(wait time.Duration) ([]Board, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// The sweep sends ~254 packets at once to addresses without an ARP entry yet; they wait in the
	// send buffer, and with the default one the queries after them stall.
	conn.SetWriteBuffer(1 << 20)
	pc := ipv4.NewPacketConn(conn)
	ifs := interfaces()
	if len(ifs) == 0 {
		return nil, errors.New("нет сетевых интерфейсов с IPv4")
	}

	boards := map[string]*found{}  // by instance name
	hosts := map[string][]string{} // host name -> IPv4
	sweep := subnetHosts(ifs)
	for _, i := range ifs {
		addrs, _ := i.Addrs()
		logf("interface %q: %v", i.Name, addrs)
	}
	logf("direct sweep: %d addresses", len(sweep))
	responders := map[string]*net.UDPAddr{} // who answered: the follow-up questions go to them too
	pack := func(qs []dnsmessage.Question) []byte {
		b, _ := (&dnsmessage.Message{Questions: qs}).Pack()
		return b
	}
	send := func(qs []dnsmessage.Question, round int) {
		b := pack(qs)
		if b == nil {
			return
		}
		for i := range ifs {
			if err := pc.SetMulticastInterface(&ifs[i]); err != nil {
				logf("multicast interface %q: %v", ifs[i].Name, err)
			} else if _, err := pc.WriteTo(b, nil, mdnsAddr); err != nil {
				logf("multicast send on %q: %v", ifs[i].Name, err)
			}
		}
		for _, a := range responders {
			conn.WriteToUDP(b, a)
		}
		if round < 2 { // the direct sweep: the first question only, twice
			ptr := pack(qs[:1])
			fails := 0
			var last error
			for _, a := range sweep {
				if responders[a.IP.String()] == nil {
					if _, err := conn.WriteToUDP(ptr, a); err != nil {
						fails++
						last = err
					}
				}
			}
			if fails > 0 {
				logf("direct sweep round %d: %d sends failed, last: %v", round, fails, last)
			}
		}
	}
	// What is still missing: SRV/TXT of the boards, A of their hosts.
	questions := func() []dnsmessage.Question {
		qs := []dnsmessage.Question{q(service, dnsmessage.TypePTR)}
		for _, b := range boards {
			if b.host == "" {
				qs = append(qs, q(b.instance, dnsmessage.TypeSRV))
			}
			if b.txt == nil {
				qs = append(qs, q(b.instance, dnsmessage.TypeTXT))
			}
			if b.host != "" && len(hosts[b.host]) == 0 {
				qs = append(qs, q(b.host, dnsmessage.TypeA))
			}
		}
		return qs
	}

	end := time.Now().Add(wait)
	next := time.Now()
	round := 0
	buf := make([]byte, 9000)
	for time.Now().Before(end) {
		if !time.Now().Before(next) {
			send(questions(), round)
			round++
			next = time.Now().Add(700 * time.Millisecond)
		}
		conn.SetReadDeadline(next)
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var m dnsmessage.Message
		if m.Unpack(buf[:n]) != nil || !m.Header.Response {
			continue
		}
		logf("%.1fs answer from %s: %d records", time.Since(end.Add(-wait)).Seconds(), from, len(m.Answers)+len(m.Additionals))
		if from.Port == 5353 {
			responders[from.IP.String()] = &net.UDPAddr{IP: from.IP, Port: 5353}
		}
		for _, rr := range append(append(m.Answers, m.Authorities...), m.Additionals...) {
			take(rr, boards, hosts)
		}
	}

	list := make([]Board, 0, len(boards))
	for _, b := range boards {
		host := strings.TrimSuffix(b.host, ".")
		if host == "" {
			host = strings.SplitN(b.instance, ".", 2)[0]
		}
		txt := b.txt
		if txt == nil {
			txt = map[string]string{}
		}
		list = append(list, Board{Host: host, IP: hosts[b.host], TXT: txt})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Host < list[j].Host })
	return list, nil
}

func take(rr dnsmessage.Resource, boards map[string]*found, hosts map[string][]string) {
	name := strings.ToLower(rr.Header.Name.String())
	get := func(inst string) *found {
		inst = strings.ToLower(inst)
		if !strings.HasSuffix(inst, "."+service) {
			return nil
		}
		b := boards[inst]
		if b == nil {
			b = &found{instance: inst}
			boards[inst] = b
		}
		return b
	}
	switch body := rr.Body.(type) {
	case *dnsmessage.PTRResource:
		if name == service {
			get(body.PTR.String())
		}
	case *dnsmessage.SRVResource:
		if b := get(name); b != nil {
			b.host = strings.ToLower(body.Target.String())
		}
	case *dnsmessage.TXTResource:
		if b := get(name); b != nil {
			b.txt = map[string]string{}
			for _, s := range body.TXT {
				k, v, _ := strings.Cut(s, "=")
				b.txt[strings.ToLower(k)] = v
			}
		}
	case *dnsmessage.AResource:
		ip := net.IP(body.A[:]).String()
		for _, have := range hosts[name] {
			if have == ip {
				return
			}
		}
		hosts[name] = append(hosts[name], ip)
	}
}

func q(name string, t dnsmessage.Type) dnsmessage.Question {
	return dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: t, Class: dnsmessage.ClassINET}
}

// All host addresses of the IPv4 subnets of the interfaces (/22 and smaller), without our own.
func subnetHosts(ifs []net.Interface) []*net.UDPAddr {
	var out []*net.UDPAddr
	seen := map[string]bool{}
	for _, i := range ifs {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ones, bits := n.Mask.Size()
			if bits != 32 || ones < 22 || ones > 30 {
				continue
			}
			own := n.IP.To4()
			seen[own.String()] = true
			base := binary.BigEndian.Uint32(own.Mask(n.Mask))
			for h := uint32(1); h < 1<<(32-ones)-1; h++ {
				ip := make(net.IP, 4)
				binary.BigEndian.PutUint32(ip, base+h)
				if !seen[ip.String()] {
					seen[ip.String()] = true
					out = append(out, &net.UDPAddr{IP: ip, Port: 5353})
				}
			}
		}
	}
	return out
}

// Up, multicast, not loopback, with an IPv4 address.
func interfaces() []net.Interface {
	all, _ := net.Interfaces()
	var out []net.Interface
	for _, i := range all {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagMulticast == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, i)
				break
			}
		}
	}
	return out
}
