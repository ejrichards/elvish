package histutil

import (
	"errors"
	"reflect"
	"testing"

	"src.elv.sh/pkg/store/storedefs"
)

var errMock = errors.New("mock error")

func TestNewHybridStore_ReturnsMemStoreIfDBIsNil(t *testing.T) {
	store, err := NewHybridStore(nil)
	if _, ok := store.(*memStore); !ok {
		t.Errorf("NewHybridStore -> %v, want memStore", store)
	}
	if err != nil {
		t.Errorf("NewHybridStore -> error %v, want nil", err)
	}
}

func TestNewHybridStore_ReturnsMemStoreOnDBError(t *testing.T) {
	db := NewFaultyInMemoryDB()
	db.SetOneOffError(errMock)
	store, err := NewHybridStore(db)
	if _, ok := store.(*memStore); !ok {
		t.Errorf("NewHybridStore -> %v, want memStore", store)
	}
	if err != errMock {
		t.Errorf("NewHybridStore -> error %v, want %v", err, errMock)
	}
}

func TestFusuer_AddCmd_AddsBothToDBAndSession(t *testing.T) {
	db := NewFaultyInMemoryDB("shared 1")
	f := mustNewHybridStore(db)

	f.AddCmd(storedefs.Cmd{Text: "session 1"})

	wantDBCmds := []storedefs.Cmd{
		{Text: "shared 1", Seq: 0}, {Text: "session 1", Seq: 1}}
	if dbCmds, _ := db.CmdsWithSeq(-1, -1); !reflect.DeepEqual(dbCmds, wantDBCmds) {
		t.Errorf("DB commands = %v, want %v", dbCmds, wantDBCmds)
	}

	allCmds, err := f.AllCmds()
	if err != nil {
		panic(err)
	}
	wantAllCmds := []storedefs.Cmd{
		{Text: "shared 1", Seq: 0},
		{Text: "session 1", Seq: 1}}
	if !reflect.DeepEqual(allCmds, wantAllCmds) {
		t.Errorf("AllCmd -> %v, want %v", allCmds, wantAllCmds)
	}
}

func TestHybridStore_AddCmd_AddsToSessionEvenIfDBErrors(t *testing.T) {
	db := NewFaultyInMemoryDB()
	f := mustNewHybridStore(db)
	db.SetOneOffError(errMock)

	_, err := f.AddCmd(storedefs.Cmd{Text: "haha"})
	if err != errMock {
		t.Errorf("AddCmd -> error %v, want %v", err, errMock)
	}

	allCmds, err := f.AllCmds()
	if err != nil {
		panic(err)
	}
	wantAllCmds := []storedefs.Cmd{{Text: "haha", Seq: 1}}
	if !reflect.DeepEqual(allCmds, wantAllCmds) {
		t.Errorf("AllCmd -> %v, want %v", allCmds, wantAllCmds)
	}
}

func TestHybridStore_AddSessionCmd_AddsToSessionOnly(t *testing.T) {
	db := NewFaultyInMemoryDB("shared 1")
	f := mustNewHybridStore(db)

	// Session-only commands get negative sequence numbers, and don't take
	// up numbers in the DB.
	if seq := f.AddSessionCmd(storedefs.Cmd{Text: "session only 1"}); seq != -1 {
		t.Errorf("AddSessionCmd -> %v, want -1", seq)
	}
	f.AddCmd(storedefs.Cmd{Text: "session 1"})
	if seq := f.AddSessionCmd(storedefs.Cmd{Text: "session only 2"}); seq != -2 {
		t.Errorf("AddSessionCmd -> %v, want -2", seq)
	}

	wantDBCmds := []storedefs.Cmd{
		{Text: "shared 1", Seq: 0}, {Text: "session 1", Seq: 1}}
	if dbCmds, _ := db.CmdsWithSeq(-1, -1); !reflect.DeepEqual(dbCmds, wantDBCmds) {
		t.Errorf("DB commands = %v, want %v", dbCmds, wantDBCmds)
	}

	allCmds, _ := f.AllCmds()
	wantAllCmds := []storedefs.Cmd{
		{Text: "shared 1", Seq: 0},
		{Text: "session only 1", Seq: -1},
		{Text: "session 1", Seq: 1},
		{Text: "session only 2", Seq: -2}}
	if !reflect.DeepEqual(allCmds, wantAllCmds) {
		t.Errorf("AllCmds -> %v, want %v", allCmds, wantAllCmds)
	}
	// The cursor walks session-only commands like any other.
	testCursorIteration(t, f.Cursor("session only"), []storedefs.Cmd{
		{Text: "session only 1", Seq: -1}, {Text: "session only 2", Seq: -2}})
}

func TestMemStore_AddSessionCmd_IsAddCmd(t *testing.T) {
	s := NewMemStore("a")
	if seq := s.AddSessionCmd(storedefs.Cmd{Text: "b", Seq: -1}); seq != 2 {
		t.Errorf("AddSessionCmd -> %v, want 2", seq)
	}
	allCmds, _ := s.AllCmds()
	wantAllCmds := []storedefs.Cmd{{Text: "a", Seq: 0}, {Text: "b", Seq: 2}}
	if !reflect.DeepEqual(allCmds, wantAllCmds) {
		t.Errorf("AllCmds -> %v, want %v", allCmds, wantAllCmds)
	}
}

func TestHybridStore_AllCmds_IncludesFrozenSharedAndNewlyAdded(t *testing.T) {
	db := NewFaultyInMemoryDB("shared 1")
	f := mustNewHybridStore(db)

	// Simulate adding commands from both the current session and other sessions.
	f.AddCmd(storedefs.Cmd{Text: "session 1"})
	db.AddCmd("other session 1")
	db.AddCmd("other session 2")
	f.AddCmd(storedefs.Cmd{Text: "session 2"})
	db.AddCmd("other session 3")

	// AllCmds should return all commands from the storage when the HybridStore
	// was created, plus session commands. The session commands should have
	// sequence numbers consistent with the DB.
	allCmds, err := f.AllCmds()
	if err != nil {
		t.Errorf("AllCmds -> error %v, want nil", err)
	}
	wantAllCmds := []storedefs.Cmd{
		{Text: "shared 1", Seq: 0},
		{Text: "session 1", Seq: 1},
		{Text: "session 2", Seq: 4}}
	if !reflect.DeepEqual(allCmds, wantAllCmds) {
		t.Errorf("AllCmds -> %v, want %v", allCmds, wantAllCmds)
	}
}

func TestHybridStore_AllCmds_ReturnsSessionIfDBErrors(t *testing.T) {
	db := NewFaultyInMemoryDB("shared 1")
	f := mustNewHybridStore(db)
	f.AddCmd(storedefs.Cmd{Text: "session 1"})
	db.SetOneOffError(errMock)

	allCmds, err := f.AllCmds()
	if err != errMock {
		t.Errorf("AllCmds -> error %v, want %v", err, errMock)
	}
	wantAllCmds := []storedefs.Cmd{{Text: "session 1", Seq: 1}}
	if !reflect.DeepEqual(allCmds, wantAllCmds) {
		t.Errorf("AllCmd -> %v, want %v", allCmds, wantAllCmds)
	}
}

func TestHybridStore_Cursor_OnlySession(t *testing.T) {
	db := NewFaultyInMemoryDB()
	f := mustNewHybridStore(db)
	db.AddCmd("+ other session")
	f.AddCmd(storedefs.Cmd{Text: "+ session 1"})
	f.AddCmd(storedefs.Cmd{Text: "- no match"})

	testCursorIteration(t, f.Cursor("+"), []storedefs.Cmd{{Text: "+ session 1", Seq: 1}})
}

func TestHybridStore_Cursor_OnlyShared(t *testing.T) {
	db := NewFaultyInMemoryDB("- no match", "+ shared 1")
	f := mustNewHybridStore(db)
	db.AddCmd("+ other session")
	f.AddCmd(storedefs.Cmd{Text: "- no match"})

	testCursorIteration(t, f.Cursor("+"), []storedefs.Cmd{{Text: "+ shared 1", Seq: 1}})
}

func TestHybridStore_Cursor_SharedAndSession(t *testing.T) {
	db := NewFaultyInMemoryDB("- no match", "+ shared 1")
	f := mustNewHybridStore(db)
	db.AddCmd("+ other session")
	db.AddCmd("- no match")
	f.AddCmd(storedefs.Cmd{Text: "+ session 1"})
	f.AddCmd(storedefs.Cmd{Text: "- no match"})

	testCursorIteration(t, f.Cursor("+"), []storedefs.Cmd{
		{Text: "+ shared 1", Seq: 1},
		{Text: "+ session 1", Seq: 4}})
}

func testCursorIteration(t *testing.T, cursor Cursor, wantCmds []storedefs.Cmd) {
	expectEndOfHistory := func() {
		t.Helper()
		if _, err := cursor.Get(); err != ErrEndOfHistory {
			t.Errorf("Get -> error %v, want ErrEndOfHistory", err)
		}
	}
	expectCmd := func(i int) {
		t.Helper()
		wantCmd := wantCmds[i]
		cmd, err := cursor.Get()
		if cmd != wantCmd {
			t.Errorf("Get -> %v, want %v", cmd, wantCmd)
		}
		if err != nil {
			t.Errorf("Get -> error %v, want nil", err)
		}
	}

	expectEndOfHistory()

	for i := len(wantCmds) - 1; i >= 0; i-- {
		cursor.Prev()
		expectCmd(i)
	}

	cursor.Prev()
	expectEndOfHistory()
	cursor.Prev()
	expectEndOfHistory()

	for i := range wantCmds {
		cursor.Next()
		expectCmd(i)
	}

	cursor.Next()
	expectEndOfHistory()
	cursor.Next()
	expectEndOfHistory()
}

func mustNewHybridStore(db DB) SessionStore {
	f, err := NewHybridStore(db)
	if err != nil {
		panic(err)
	}
	return f
}
