// Package fixed keeps the boards the user marked ("fix") with a name of their own, on this computer:
// <config dir>/napi-discover/boards.json (%APPDATA% on Windows, ~/.config on Linux). The board
// itself is not changed: this works for boards with napi-avahi and for bare avahi alike.
//
// A board is known by its SSH host key and by its MAC. The key first: the MAC alone is not enough
// (Armbian derives it from a part of the chip serial, two NAPI-C can share it). The MAC when the
// key changed (a reflash) and that MAC belongs to one found board only.
package fixed

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Name string `json:"name"` // the user's name: "котельная"
	MAC  string `json:"mac,omitempty"`
	Key  string `json:"key,omitempty"`  // SHA256:... of the SSH host key
	IP   string `json:"ip,omitempty"`   // the last one seen
	Host string `json:"host,omitempty"` // the last network name seen
	// set by the user: shown instead of what the board tells (napi-avahi, the SSH banner)
	Model string    `json:"model,omitempty"`
	SoC   string    `json:"soc,omitempty"`
	Role  string    `json:"role,omitempty"`
	OS    string    `json:"os,omitempty"`
	Seen  time.Time `json:"seen,omitempty"`
	// what the board told of itself when last found (napi-avahi, the SSH banner): the table shows
	// it at the start, before the check
	Last *Info `json:"last,omitempty"`
}

// Found is one row of a search, as far as matching goes.
type Found struct{ IP, MAC, Key, Host string }

// Path: the file; tests set it.
var Path = func() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "napi-discover", "boards.json")
}

func Load() ([]Entry, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var es []Entry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, err
	}
	return es, nil
}

func Save(es []Entry) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	sort.SliceStable(es, func(i, j int) bool { return strings.ToLower(es[i].Name) < strings.ToLower(es[j].Name) })
	b, _ := json.MarshalIndent(es, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func normMAC(m string) string { return strings.ToLower(strings.TrimSpace(m)) }

// Match: for each found row the index of its entry, or -1. The entries get the new IP, name, time
// and, after a MAC match, the new key (so the caller saves them when changed is true).
func Match(es []Entry, rows []Found, now time.Time) (idx []int, changed bool) {
	idx = make([]int, len(rows))
	for i := range idx {
		idx[i] = -1
	}
	used := make([]bool, len(es))
	// the key
	for i, r := range rows {
		if r.Key == "" {
			continue
		}
		for e := range es {
			if !used[e] && es[e].Key == r.Key {
				idx[i], used[e] = e, true
				break
			}
		}
	}
	// the MAC: only if one unmatched row has it
	macRows := map[string][]int{}
	for i, r := range rows {
		if m := normMAC(r.MAC); m != "" {
			macRows[m] = append(macRows[m], i)
		}
	}
	for e := range es {
		m := normMAC(es[e].MAC)
		if used[e] || m == "" || len(macRows[m]) != 1 {
			continue
		}
		if i := macRows[m][0]; idx[i] < 0 {
			idx[i], used[e] = e, true
		}
	}
	// neither key nor MAC (no SSH, another subnet): the IP
	for e := range es {
		if used[e] || es[e].Key != "" || es[e].MAC != "" || es[e].IP == "" {
			continue
		}
		for i, r := range rows {
			if idx[i] < 0 && r.IP == es[e].IP {
				idx[i], used[e] = e, true
				break
			}
		}
	}
	for i, e := range idx {
		if e < 0 {
			continue
		}
		x, r := &es[e], rows[i]
		if r.Key != "" && x.Key != r.Key {
			x.Key, changed = r.Key, true
		}
		if r.MAC != "" && normMAC(x.MAC) != normMAC(r.MAC) {
			x.MAC, changed = r.MAC, true
		}
		if x.IP != r.IP || (r.Host != "" && x.Host != r.Host) {
			x.IP, changed = r.IP, true
			if r.Host != "" {
				x.Host = r.Host
			}
		}
		x.Seen, changed = now, true
	}
	return idx, changed
}

// Fix adds (or renames) the entry of a found row; an empty name removes it.
func Fix(es []Entry, r Found, name string, now time.Time) []Entry {
	idx, _ := Match(es, []Found{r}, now)
	name = strings.TrimSpace(name)
	if e := idx[0]; e >= 0 {
		if name == "" {
			return append(es[:e:e], es[e+1:]...)
		}
		es[e].Name = name
		return es
	}
	if name == "" {
		return es
	}
	return append(es, Entry{Name: name, MAC: r.MAC, Key: r.Key, IP: r.IP, Host: r.Host, Seen: now})
}

// Remove drops an entry by its index (a board not found now has no row to unmark).
func Remove(es []Entry, i int) []Entry {
	if i < 0 || i >= len(es) {
		return es
	}
	return append(es[:i:i], es[i+1:]...)
}
