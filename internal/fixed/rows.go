package fixed

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lab240/napi-avahi/internal/napifind"
)

// Row is one line of the table of any napi-discover (the window, the page, the console).
type Row struct {
	Fixed                                      string // the user's name; "" when not marked
	Entry                                      int    // the index of the entry in boards.json, -1 when not marked
	Name, IP, Model, SoC, OS, Role, MAC, Signs string
	Key                                        string // SSH host key, for "fix"
	Kind                                       string // "avahi", "raw" (NAPI? of the raw search), "gone" (marked, not found now), ""
}

// FirstIP: the address for the buttons ("a, b" with two interfaces).
func (r Row) FirstIP() string { return strings.TrimSpace(strings.Split(r.IP, ",")[0]) }

// Search runs the ordinary search (napi-avahi) or the raw one, and asks the host keys.
// The rows are as found: Annotate puts the marked boards in.
func Search(raw bool, progress func(done, total int)) ([]Row, error) {
	var rows []Row
	var err error
	if raw {
		var hosts []napifind.Host
		hosts, err = napifind.Scan(progress)
		for _, h := range hosts {
			r := Row{Entry: -1, Name: h.Name, IP: h.IP, OS: h.SSH, MAC: h.MAC, Signs: h.Verdict}
			if b := h.Board; b != nil {
				r.Model, r.SoC, r.OS, r.Role = b.TXT["model"], b.TXT["soc"], b.TXT["os"], b.TXT["role"]
				r.Kind = "avahi"
			} else if h.Verdict == "NAPI?" {
				r.Kind = "raw"
			}
			if len(h.Signs) > 0 {
				r.Signs += ": " + strings.Join(h.Signs, " ")
			}
			rows = append(rows, r)
		}
	} else {
		var list []napifind.Board
		list, err = napifind.Find(3 * time.Second)
		for _, b := range list {
			t := b.TXT
			rows = append(rows, Row{Entry: -1, Name: b.Host, IP: strings.Join(b.IP, ", "), Model: t["model"], SoC: t["soc"],
				OS: t["os"], Role: t["role"], MAC: t["mac"], Signs: "napi-avahi", Kind: "avahi"})
		}
		// the ordinary search does not see the boards without napi-avahi; the marked ones are
		// looked up at their last address, so they still show up at once
		if es, _ := Load(); len(es) > 0 {
			rows = append(rows, lastSeen(rows, es)...)
		}
	}
	var ips []string
	for _, r := range rows {
		ips = append(ips, r.FirstIP())
	}
	keys := napifind.HostKeys(ips)
	for i := range rows {
		rows[i].Key = keys[rows[i].FirstIP()]
	}
	return rows, err
}

// The marked boards not answered by napi-avahi: TCP 22 at the last IP, with the mDNS name by it.
func lastSeen(rows []Row, es []Entry) []Row {
	have := map[string]bool{}
	for _, r := range rows {
		for _, ip := range strings.Split(r.IP, ",") {
			have[strings.TrimSpace(ip)] = true
		}
	}
	var ips []string
	for _, e := range es {
		if e.IP != "" && !have[e.IP] {
			have[e.IP] = true
			ips = append(ips, e.IP)
		}
	}
	var out []Row
	for _, h := range napifind.Probe(ips) {
		r := Row{Entry: -1, Name: h.Name, IP: h.IP, OS: h.SSH, MAC: h.MAC, Signs: h.Verdict, Kind: "raw"}
		if len(h.Signs) > 0 {
			r.Signs += ": " + strings.Join(h.Signs, " ")
		}
		out = append(out, r)
	}
	return out
}

// Annotate: the found rows with the user's names; the marked boards first (by name), then the
// rest as found, then the marked boards not found now ("gone": the last IP and when). The
// entries get the new IP/key and are saved.
func Annotate(found []Row) []Row {
	es, err := Load()
	if err != nil || len(es) == 0 {
		out := append([]Row{}, found...)
		for i := range out {
			out[i].Fixed, out[i].Entry = "", -1
		}
		return out
	}
	fs := make([]Found, len(found))
	for i, r := range found {
		fs[i] = Found{IP: r.FirstIP(), MAC: r.MAC, Key: r.Key, Host: r.Name}
	}
	idx, changed := Match(es, fs, time.Now())
	if changed {
		Save(es)
		es, _ = Load() // Save sorts
		idx, _ = Match(es, fs, time.Now())
	}
	var marked, rest []Row
	seen := make([]bool, len(es))
	for i, r := range found {
		r.Fixed, r.Entry = "", -1
		if e := idx[i]; e >= 0 {
			r.Fixed, r.Entry, seen[e] = es[e].Name, e, true
			marked = append(marked, r)
			continue
		}
		rest = append(rest, r)
	}
	sort.SliceStable(marked, func(i, j int) bool { return marked[i].Entry < marked[j].Entry })
	out := append(marked, rest...)
	for e, x := range es {
		if seen[e] {
			continue
		}
		when := ""
		if !x.Seen.IsZero() {
			when = " " + x.Seen.Local().Format("02.01.2006 15:04")
		}
		out = append(out, Row{Fixed: x.Name, Entry: e, Name: x.Host, IP: x.IP, MAC: x.MAC, Key: x.Key, Kind: "gone",
			Signs: fmt.Sprintf("не найдена (последний раз%s)", when)})
	}
	return out
}

// SetName marks a row with a name, renames it, or ("") unmarks it.
func SetName(r Row, name string) error {
	es, err := Load()
	if err != nil {
		return err
	}
	if r.Entry >= 0 && r.Entry < len(es) && r.Kind == "gone" {
		if strings.TrimSpace(name) == "" {
			es = Remove(es, r.Entry)
		} else {
			es[r.Entry].Name = strings.TrimSpace(name)
		}
		return Save(es)
	}
	return Save(Fix(es, Found{IP: r.FirstIP(), MAC: r.MAC, Key: r.Key, Host: r.Name}, name, time.Now()))
}
