package state

import (
	"context"
	"herdr-workspace-enhancer/model"
	"reflect"
	"testing"
)

func TestOrderingPersistsAndSyncPreserves(t *testing.T) {
	s := testStore(t, t.TempDir())
	ctx := context.Background()
	rows := []Record{{ID: "a", Status: model.WorkspaceActive}, {ID: "hidden", Status: model.WorkspaceArchived}, {ID: "b", Status: model.WorkspaceActive}}
	if err := s.ApplySync(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveRecord(ctx, "b", "a", true); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySync(ctx, rows); err != nil {
		t.Fatal(err)
	}
	put(t, s, Record{ID: "new", Status: model.WorkspaceActive})
	got, err := s.ListSortedRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "a", "hidden", "new"}) {
		t.Fatal(ids)
	}
	if _, err := s.MoveRecord(ctx, "a", "missing", true); err == nil {
		t.Fatal("missing target accepted")
	}
	if _, err := s.MoveRecord(ctx, "a", "a", true); err != nil {
		t.Fatal(err)
	}
	var n int64
	s.db.Model(&workspaceRow{}).Where("sort_order IS NULL").Count(&n)
	if n != 0 {
		t.Fatal("uninitialized order")
	}
}

func TestOrderingRollbackAndReopen(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		put(t, s, Record{ID: id, Status: model.WorkspaceActive})
	}
	if err := s.db.Exec(`CREATE TRIGGER fail_order BEFORE UPDATE OF sort_order ON workspaces WHEN NEW.id = 'b' BEGIN SELECT RAISE(ABORT, 'test write failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveRecord(ctx, "c", "a", true); err == nil {
		t.Fatal("expected failure")
	}
	rows, _ := s.ListNonClosedRecords(context.Background())
	if rows[0].ID != "a" || rows[2].ID != "c" {
		t.Fatal("partial transaction", rows)
	}
	s.db.Exec("DROP TRIGGER fail_order")
	if _, err := s.MoveRecord(ctx, "c", "a", true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, _ = s.ListSortedRecords(context.Background())
	if rows[0].ID != "c" {
		t.Fatal("order not persisted", rows)
	}
}

func TestStableNormalizationAndQueryScopes(t *testing.T) {
	s := testStore(t, t.TempDir())
	ctx := context.Background()
	for _, r := range []Record{
		{ID: "null1", Status: model.WorkspaceActive},
		{ID: "tie1", Status: model.WorkspaceActive},
		{ID: "closed", HostID: "closed-host", Status: model.WorkspaceClosed},
		{ID: "manager", Status: model.WorkspaceActive, ManagerState: model.ManagerDone},
		{ID: "first", Status: model.WorkspaceArchived},
		{ID: "tie2", Status: model.WorkspaceActive},
		{ID: "null2", Status: model.WorkspaceActive},
	} {
		put(t, s, r)
	}
	if err := s.db.Exec(`UPDATE workspaces SET sort_order = CASE WHEN id IN ('tie1','tie2') THEN 8 WHEN id = 'first' THEN 3 ELSE NULL END`).Error; err != nil {
		t.Fatal(err)
	}
	nonClosed, err := s.ListNonClosedRecords(ctx)
	if err != nil || len(nonClosed) != 6 || nonClosed[2].ID != "manager" {
		t.Fatal(nonClosed, err)
	}
	var assigned int64
	s.db.Model(&workspaceRow{}).Where("sort_order IS NOT NULL").Count(&assigned)
	if assigned != 3 {
		t.Fatal("read query assigned indexes")
	}
	rows, err := s.ListSortedRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "tie1", "tie2", "null1", "null2"}
	for i, r := range rows {
		if r.ID != want[i] {
			t.Fatal(rows)
		}
	}
	var excluded []workspaceRow
	s.db.Where("id IN ?", []string{"closed", "manager"}).Find(&excluded)
	for _, r := range excluded {
		if r.SortOrder != nil {
			t.Fatal(r)
		}
	}
	if _, err := s.Get("closed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindHost(ctx, "closed-host"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec(`CREATE TRIGGER reject_update BEFORE UPDATE ON workspaces BEGIN SELECT RAISE(ABORT, 'unexpected update'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListSortedRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveRecord(ctx, "first", "first", true); err != nil {
		t.Fatal(err)
	}
}

func TestMoveDirectionsAndInvalidIdentity(t *testing.T) {
	for _, tc := range []struct {
		source, target string
		before         bool
		want           string
	}{
		{"a", "c", true, "bac"}, {"a", "c", false, "bca"},
		{"c", "a", true, "cab"}, {"c", "a", false, "acb"},
		{"b", "b", false, "abc"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			s := testStore(t, t.TempDir())
			ctx := context.Background()
			for _, id := range []string{"a", "b", "c"} {
				put(t, s, Record{ID: id, Status: model.WorkspaceActive})
			}
			if _, err := s.MoveRecord(ctx, "missing", "a", true); err == nil {
				t.Fatal("accepted missing source")
			}
			if _, err := s.MoveRecord(ctx, "a", "missing", true); err == nil {
				t.Fatal("accepted missing target")
			}
			var n int64
			s.db.Model(&workspaceRow{}).Where("sort_order IS NOT NULL").Count(&n)
			if n != 0 {
				t.Fatal("invalid move changed indexes")
			}
			rows, err := s.MoveRecord(ctx, tc.source, tc.target, tc.before)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			for _, r := range rows {
				got += r.ID
			}
			if got != tc.want {
				t.Fatal(got)
			}
		})
	}
}

func TestConcurrentRefreshAndMove(t *testing.T) {
	root := t.TempDir()
	s := testStore(t, root)
	other, err := Open(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		put(t, s, Record{ID: id, Status: model.WorkspaceActive})
	}
	for i := 0; i < 20; i++ {
		done := make(chan error, 1)
		go func() { _, e := other.ListSortedRecords(ctx); done <- e }()
		if _, err := s.MoveRecord(ctx, "c", "a", true); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		rows, err := s.ListSortedRecords(ctx)
		if err != nil || rows[0].ID != "c" {
			t.Fatal(rows, err)
		}
	}
}

func TestReactivatedRecordRetainsIndexAndRowOrder(t *testing.T) {
	s := testStore(t, t.TempDir())
	ctx := context.Background()
	a := Record{ID: "a", Status: model.WorkspaceActive}
	b := Record{ID: "b", Status: model.WorkspaceActive}
	if err := s.ApplySync(ctx, []Record{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListSortedRecords(ctx); err != nil {
		t.Fatal(err)
	}
	a.Status = model.WorkspaceClosed
	if err := s.ApplySync(ctx, []Record{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListSortedRecords(ctx); err != nil {
		t.Fatal(err)
	}
	a.Status = model.WorkspaceActive
	if err := s.ApplySync(ctx, []Record{a}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListSortedRecords(ctx)
	if err != nil || rows[0].ID != "a" {
		t.Fatal(rows, err)
	}
}
