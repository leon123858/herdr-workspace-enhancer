package service

import "fmt"

type identityEntry[T any] struct {
	recordID, hostID string
	value            T
	removed          bool
}

// identitySet indexes nonempty keys independently and preserves insertion order.
// Failed operations leave both indexes untouched.
type identitySet[T any] struct {
	records, hosts map[string]*identityEntry[T]
	order          []*identityEntry[T]
}

func newIdentitySet[T any]() *identitySet[T] {
	return &identitySet[T]{records: make(map[string]*identityEntry[T]), hosts: make(map[string]*identityEntry[T])}
}
func (s *identitySet[T]) add(recordID, hostID string, value T) error {
	if recordID == "" && hostID == "" {
		return fmt.Errorf("both identity keys are empty")
	}
	if e := s.records[recordID]; e != nil && e.hostID == hostID {
		return nil
	}
	if s.records[recordID] != nil || s.hosts[hostID] != nil {
		return fmt.Errorf("duplicate identity record=%q host=%q", recordID, hostID)
	}
	e := &identityEntry[T]{recordID: recordID, hostID: hostID, value: value}
	if recordID != "" {
		s.records[recordID] = e
	}
	if hostID != "" {
		s.hosts[hostID] = e
	}
	s.order = append(s.order, e)
	return nil
}
func (s *identitySet[T]) bindRecord(e *identityEntry[T], id string) error {
	if id == "" || e.removed {
		return fmt.Errorf("invalid record binding %q", id)
	}
	if other := s.records[id]; other != nil && other != e {
		return fmt.Errorf("duplicate record identity %q", id)
	}
	if e.recordID != "" {
		delete(s.records, e.recordID)
	}
	e.recordID = id
	s.records[id] = e
	return nil
}
func (s *identitySet[T]) remove(e *identityEntry[T]) {
	delete(s.records, e.recordID)
	delete(s.hosts, e.hostID)
	e.removed = true
}
func (s *identitySet[T]) entries() []*identityEntry[T] {
	result := make([]*identityEntry[T], 0, len(s.order))
	for _, e := range s.order {
		if !e.removed {
			result = append(result, e)
		}
	}
	return result
}
