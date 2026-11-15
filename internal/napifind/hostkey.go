package napifind

import (
	"errors"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// HostKeys asks each address for its SSH host key (the key exchange only, no login) and returns
// the SHA256 fingerprints by IP. The key tells one board from another where the MAC does not:
// Armbian makes the MAC from a part of the chip serial, and two NAPI-C can have the same one.
// A reflash makes a new key; the fixed boards then fall back to the MAC.
func HostKeys(ips []string) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, ip := range ips {
		if net.ParseIP(ip) == nil {
			continue
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if k := hostKey(ip); k != "" {
				mu.Lock()
				out[ip] = k
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	logf("host keys: %d of %d", len(out), len(ips))
	return out
}

var errGotKey = errors.New("got the key")

func hostKey(ip string) string {
	c, err := net.DialTimeout("tcp4", net.JoinHostPort(ip, "22"), 2*time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(4 * time.Second))
	var fp string
	cfg := &ssh.ClientConfig{
		User: "napi-discover",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			fp = ssh.FingerprintSHA256(k)
			return errGotKey // stop here: no login
		},
		Timeout: 4 * time.Second,
	}
	ssh.NewClientConn(c, ip+":22", cfg)
	return fp
}
