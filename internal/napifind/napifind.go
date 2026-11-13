// Package napifind finds the NAPI boards on the LAN: the mDNS service _napi._tcp that
// the napi-avahi package announces, with its TXT fields (model, soc, os, role, mac).
//
// It asks with "legacy unicast" mDNS queries (from an ordinary UDP port, not 5353), so it does not
// fight the mDNS service of the system for the port: the answers come straight back to it.
package napifind

import (
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
	Host string            // napi3308-b9df.local
	IP   []string          // IPv4
	TXT  map[string]string // model, soc, os, role, mac, kernel
}

type found struct {
	instance, host string
	txt            map[string]string
}

// Find listens for the given time and returns the boards sorted by name.
func Find(wait time.Duration) ([]Board, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	pc := ipv4.NewPacketConn(conn)
	ifs := interfaces()
	if len(ifs) == 0 {
		return nil, errors.New("нет сетевых интерфейсов с IPv4")
	}

	boards := map[string]*found{}  // by instance name
	hosts := map[string][]string{} // host name -> IPv4
	send := func(qs []dnsmessage.Question) {
		b, err := (&dnsmessage.Message{Questions: qs}).Pack()
		if err != nil {
			return
		}
		for i := range ifs {
			if pc.SetMulticastInterface(&ifs[i]) == nil {
				pc.WriteTo(b, nil, mdnsAddr)
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
	buf := make([]byte, 9000)
	for time.Now().Before(end) {
		if !time.Now().Before(next) {
			send(questions())
			next = time.Now().Add(700 * time.Millisecond)
		}
		conn.SetReadDeadline(next)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var m dnsmessage.Message
		if m.Unpack(buf[:n]) != nil || !m.Header.Response {
			continue
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
