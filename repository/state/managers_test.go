package state

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentManagerRegistration(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, "session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save("manager-workspace-id", 0, "legacy"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			other, e := Open(root, "session")
			if e == nil {
				e = other.RegisterManager(fmt.Sprint(i))
			}
			if e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	ids, err := s.ManagerIDs()
	if err != nil || len(ids) != 21 || !ids["legacy"] {
		t.Fatal(ids, err)
	}
	other, err := Open(root, "other-session")
	if err != nil {
		t.Fatal(err)
	}
	ids, err = other.ManagerIDs()
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
}
