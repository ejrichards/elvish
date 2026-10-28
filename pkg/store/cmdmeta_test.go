package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/store"
	"src.elv.sh/pkg/testutil"
)

func TestHostID_PersistsInFileNextToDB(t *testing.T) {
	dir := testutil.InTempDir(t)
	dbPath := filepath.Join(dir, "db.bolt")
	idPath := filepath.Join(dir, "host-id")

	st := must.OK1(store.NewStore(dbPath))
	id := must.OK1(st.HostID())
	must.OK(st.Close())

	content := must.OK1(os.ReadFile(idPath))
	if string(content) != id+"\n" {
		t.Errorf("host-id file contains %q, want %q", content, id+"\n")
	}

	// Deleting the database keeps the host ID.
	must.OK(os.Remove(dbPath))
	st = must.OK1(store.NewStore(dbPath))
	if id2 := must.OK1(st.HostID()); id2 != id {
		t.Errorf("host ID after deleting db is %q, want %q", id2, id)
	}
	must.OK(st.Close())

	// Deleting the file generates a new host ID.
	must.OK(os.Remove(idPath))
	st = must.OK1(store.NewStore(dbPath))
	if id3 := must.OK1(st.HostID()); id3 == id || id3 == "" {
		t.Errorf("host ID after deleting file is %q, want a new ID", id3)
	}
	must.OK(st.Close())
}

func TestHostID_IgnoresInvalidFile(t *testing.T) {
	dir := testutil.InTempDir(t)
	idPath := filepath.Join(dir, "host-id")
	must.OK(os.WriteFile(idPath, []byte("not a uuid\n"), 0600))

	st := must.OK1(store.NewStore(filepath.Join(dir, "db.bolt")))
	defer st.Close()
	id := must.OK1(st.HostID())
	if id == "" || id == "not a uuid" {
		t.Errorf("host ID is %q, want a new ID", id)
	}
	if content := must.OK1(os.ReadFile(idPath)); string(content) != id+"\n" {
		t.Errorf("host-id file contains %q, want %q", content, id+"\n")
	}
}
