package fixed

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	now := time.Now()
	// .52 and .69 share a MAC (Armbian): told apart by the key
	es := []Entry{
		{Name: "котельная", MAC: "d2:a9:fd:81:b9:df", Key: "K52"},
		{Name: "склад", MAC: "D2:A9:FD:81:B9:DF", Key: "K69"},
		{Name: "старая", MAC: "06:39:d7:ba:f3:4e", Key: "OLD"},
		{Name: "нет", MAC: "aa:aa:aa:aa:aa:aa"},
	}
	rows := []Found{
		{IP: "192.168.30.69", MAC: "d2:a9:fd:81:b9:df", Key: "K69", Host: "napic-2.local"},
		{IP: "192.168.30.52", MAC: "d2:a9:fd:81:b9:df", Key: "K52", Host: "napic.local"},
		{IP: "192.168.30.55", MAC: "06:39:d7:ba:f3:4e", Key: "NEW"}, // reflashed: the MAC
		{IP: "192.168.30.99", MAC: "11:11:11:11:11:11", Key: "X"},
	}
	idx, ch := Match(es, rows, now)
	if idx[0] != 1 || idx[1] != 0 || idx[2] != 2 || idx[3] != -1 || !ch {
		t.Fatalf("idx %v", idx)
	}
	if es[2].Key != "NEW" || es[2].IP != "192.168.30.55" || es[1].Host != "napic-2.local" || !es[3].Seen.IsZero() {
		t.Fatalf("entries %+v", es)
	}
	// a shared MAC without a matching key: not guessed
	es2 := []Entry{{Name: "x", MAC: "d2:a9:fd:81:b9:df", Key: "GONE"}}
	if idx, _ := Match(es2, rows[:2], now); idx[0] != -1 || idx[1] != -1 {
		t.Fatalf("shared MAC matched: %v", idx)
	}
}

func TestFixSaveLoad(t *testing.T) {
	dir := t.TempDir()
	Path = func() string { return filepath.Join(dir, "a", "boards.json") }
	now := time.Now()
	r := Found{IP: "10.0.0.5", MAC: "06:eb:6b:07:55:9f", Key: "K"}
	es := Fix(nil, r, " щит ", now)
	es = Fix(es, r, "щит 2", now)
	if len(es) != 1 || es[0].Name != "щит 2" {
		t.Fatalf("%+v", es)
	}
	if err := Save(es); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || len(got) != 1 || got[0].Key != "K" {
		t.Fatalf("%+v %v", got, err)
	}
	if es = Fix(got, r, "", now); len(es) != 0 {
		t.Fatalf("not removed: %+v", es)
	}
}

func TestAnnotateOrderAndInfo(t *testing.T) {
	dir := t.TempDir()
	Path = func() string { return filepath.Join(dir, "boards.json") }
	found := []Row{
		{Name: "pc.local", IP: "10.0.0.9", Key: "PC", Signs: "Linux: linux"},
		{Name: "napic.local", IP: "10.0.0.7", Key: "K7", Kind: "raw", Signs: "NAPI?: avahi"},
		{Name: "napi3308.local", IP: "10.0.0.20", Key: "K20", Kind: "avahi", Model: "Napi C"},
		{Name: "napic-2.local", IP: "10.0.0.5", Key: "K5", Kind: "raw", Signs: "NAPI?: avahi"},
	}
	if err := SetName(found[1], "котельная"); err != nil {
		t.Fatal(err)
	}
	out := Annotate(found)
	var ips []string
	for _, r := range out {
		ips = append(ips, r.IP)
	}
	want := "10.0.0.7 10.0.0.20 10.0.0.5 10.0.0.9"
	if got := strings.Join(ips, " "); got != want {
		t.Fatalf("order %s, want %s", got, want)
	}
	if err := Edit(out[0], Info{Name: "котельная", Model: " Napi C (Classic) ", SoC: "rk3308", Role: "термостат", OS: "NapiLinux"}); err != nil {
		t.Fatal(err)
	}
	if err := Edit(out[2], Info{Name: " "}); err == nil {
		t.Fatal("Edit without a name")
	}
	// an unmarked napi-avahi row: marked by Edit; the model left as shown stays the board's
	if err := Edit(out[1], Info{Name: "щит 20", Model: out[1].Model, Role: "щит"}); err != nil {
		t.Fatal(err)
	}
	out = Annotate(found)
	if r := out[0]; r.Fixed != "котельная" || r.Model != "Napi C (Classic)" || r.SoC != "rk3308" || r.Role != "термостат" || r.OS != "NapiLinux" {
		t.Fatalf("%+v", r)
	}
	if r := out[1]; r.Fixed != "щит 20" || r.Model != "Napi C" || r.Role != "щит" {
		t.Fatalf("%+v", r)
	}
	// the user's model wins over the napi-avahi one; cleared, the board's is back
	if err := Edit(out[1], Info{Name: "щит 20", Model: "мой RK3568", Role: "щит"}); err != nil {
		t.Fatal(err)
	}
	if r := Annotate(found)[1]; r.Model != "мой RK3568" {
		t.Fatalf("%+v", r)
	}
	if err := Edit(Annotate(found)[1], Info{Name: "щит 20", Role: "щит"}); err != nil {
		t.Fatal(err)
	}
	if r := Annotate(found)[1]; r.Model != "Napi C" {
		t.Fatalf("%+v", r)
	}
	// "Изменить" sends the fields as shown: the board's own values (the napi-avahi model, the SSH
	// banner as the OS) do not become the user's
	found[0].OS = "OpenSSH_9.6"
	pc := Annotate(found)[3]
	if err := Edit(pc, Info{Name: "пк", OS: pc.OS}); err != nil {
		t.Fatal(err)
	}
	es, _ := Load()
	for _, e := range es {
		if (e.Name == "щит 20" && e.Model != "") || (e.Name == "пк" && e.OS != "") {
			t.Fatalf("the board's own value saved: %+v", e)
		}
	}
	// renamed: the model stays; not found: the gone row has it too
	if err := SetName(out[0], "щит"); err != nil {
		t.Fatal(err)
	}
	out = Annotate(found[3:])
	var gone *Row
	for i := range out {
		if out[i].Fixed == "щит" {
			gone = &out[i]
		}
	}
	if gone == nil || gone.Kind != "gone" || gone.Model != "Napi C (Classic)" || gone.OS != "NapiLinux" {
		t.Fatalf("%+v", out)
	}
}

func TestSavedShowsLastSeen(t *testing.T) {
	dir := t.TempDir()
	Path = func() string { return filepath.Join(dir, "boards.json") }
	found := []Row{{Name: "napi3568.local", IP: "10.0.0.20", Key: "K20", Kind: "avahi", Model: "Napi 2", SoC: "rk3568", OS: "armbian"}}
	if err := SetName(found[0], "щит"); err != nil {
		t.Fatal(err)
	}
	out := Annotate(found) // remembers what the board told
	if err := Edit(out[0], Info{Name: "щит", Model: out[0].Model, SoC: out[0].SoC, OS: out[0].OS, Role: "котёл"}); err != nil {
		t.Fatal(err)
	}
	s := Saved()
	if len(s) != 1 || s[0].Kind != "wait" || s[0].IP != "10.0.0.20" || s[0].Model != "Napi 2" || s[0].SoC != "rk3568" || s[0].Role != "котёл" {
		t.Fatalf("%+v", s)
	}
}
