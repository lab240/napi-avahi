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
	if err := SetInfo(out[0], "model", " Napi C (Classic) "); err != nil {
		t.Fatal(err)
	}
	if err := SetInfo(out[0], "soc", "rk3308"); err != nil {
		t.Fatal(err)
	}
	if err := SetInfo(out[1], "soc", "x"); err == nil {
		t.Fatal("SetInfo on an unmarked row")
	}
	out = Annotate(found)
	if out[0].Model != "Napi C (Classic)" || out[0].SoC != "rk3308" {
		t.Fatalf("%+v", out[0])
	}
	// renamed: the model stays; not found: the gone row has it too
	if err := SetName(out[0], "щит"); err != nil {
		t.Fatal(err)
	}
	out = Annotate(found[2:])
	last := out[len(out)-1]
	if last.Kind != "gone" || last.Fixed != "щит" || last.Model != "Napi C (Classic)" {
		t.Fatalf("%+v", last)
	}
}
