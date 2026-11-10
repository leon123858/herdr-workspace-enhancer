package state

import "testing"

func TestDatabaseAndLock(t *testing.T) {
	s, e := Open(t.TempDir(), "s")
	if e != nil {
		t.Fatal(e)
	}
	unlock, e := s.Lock("session")
	if e != nil {
		t.Fatal(e)
	}
	if other, e := s.Lock("session"); e == nil {
		other()
		t.Fatal("concurrent lock acquired")
	}
	unlock()
	unlock, e = s.Lock("session")
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	db := Database{Version: 1, Records: []Record{{ID: "uuid", Status: "archived", Label: "中文"}}}
	if e = s.WriteDatabase(db); e != nil {
		t.Fatal(e)
	}
	db.Records[0].Label = "updated"
	if e = s.WriteDatabase(db); e != nil {
		t.Fatal(e)
	}
	got, e := s.ReadDatabase()
	if e != nil || got.Records[0].Label != "updated" {
		t.Fatal(got, e)
	}
}
