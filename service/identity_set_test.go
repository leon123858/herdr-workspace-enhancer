package service

import "testing"

func TestIdentitySetConflictPreservesEntry(t *testing.T) {
	s := newIdentitySet[int]()
	if s.add("", "", 0) == nil {
		t.Fatal("accepted empty identity")
	}
	if err := s.add("r", "h1", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.add("", "h2", 2); err != nil {
		t.Fatal(err)
	}
	second := s.hosts["h2"]
	if s.bindRecord(second, "r") == nil {
		t.Fatal("accepted duplicate record")
	}
	if second.recordID != "" || len(s.entries()) != 2 {
		t.Fatal("conflict changed entries")
	}
	if s.add("other", "h1", 3) == nil {
		t.Fatal("accepted duplicate host")
	}
	s.remove(s.records["r"])
	if s.records["r"] != nil || s.hosts["h1"] != nil {
		t.Fatal("stale index")
	}
	if err := s.bindRecord(second, "r"); err != nil {
		t.Fatal(err)
	}
	if err := s.add("r", "h2", 2); err != nil {
		t.Fatal(err)
	}
	if len(s.entries()) != 1 {
		t.Fatal("duplicate entry")
	}
}
