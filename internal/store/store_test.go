package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func row(id string, updatedAt, ttl int64) Row {
	return Row{
		ID:         id,
		ReadonlyID: "ro-" + id,
		Text:       "text of " + id,
		Language:   "go",
		TTLSeconds: ttl,
		CreatedAt:  updatedAt - 10,
		UpdatedAt:  updatedAt,
		ExpiresAt:  updatedAt + ttl,
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	s := openTemp(t)
	now := time.Now().Unix()
	want := row("doc1", now, 3600)
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("doc1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// Upsert updates in place.
	want.Text = "updated"
	want.UpdatedAt = now + 5
	want.ExpiresAt = now + 5 + 3600
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Load("doc1")
	if got.Text != "updated" {
		t.Errorf("upsert did not update text: %+v", got)
	}
	// Readonly lookup finds the same row.
	byRo, err := s.LoadByReadonlyID("ro-doc1")
	if err != nil {
		t.Fatal(err)
	}
	if byRo == nil || byRo.ID != "doc1" {
		t.Errorf("LoadByReadonlyID got %+v", byRo)
	}
}

func TestLoadMissingIsNil(t *testing.T) {
	s := openTemp(t)
	got, err := s.Load("nope")
	if err != nil || got != nil {
		t.Errorf("got %+v, %v; want nil, nil", got, err)
	}
}

func TestDeleteExpired(t *testing.T) {
	s := openTemp(t)
	now := time.Now().Unix()
	s.Save(row("old", now-7200, 3600)) // expired an hour ago
	s.Save(row("live", now-60, 86400))
	n, err := s.DeleteExpired(now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("deleted %d rows, want 1", n)
	}
	if got, _ := s.Load("old"); got != nil {
		t.Error("expired row still present")
	}
	if got, _ := s.Load("live"); got == nil {
		t.Error("live row was deleted")
	}
}

func TestListAndCount(t *testing.T) {
	s := openTemp(t)
	now := time.Now().Unix()
	for i, id := range []string{"a", "b", "c"} {
		s.Save(row(id, now+int64(i), 3600)) // c is most recent
	}
	if n, _ := s.Count(); n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	rows, err := s.List(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "c" || rows[1].ID != "b" {
		t.Errorf("page 1 = %v", rowIDs(rows))
	}
	rows, _ = s.List(2, 2)
	if len(rows) != 1 || rows[0].ID != "a" {
		t.Errorf("page 2 = %v", rowIDs(rows))
	}
}

func rowIDs(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
