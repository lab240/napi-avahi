package fixed

import (
	"path/filepath"
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
