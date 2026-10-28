package store_test

import (
	"testing"

	"src.elv.sh/pkg/store"
	"src.elv.sh/pkg/store/storetest"
)

func TestRecords(t *testing.T) {
	storetest.TestRecords(t, store.MustTempStore(t))
}

func TestRecords_AfterCmdMeta(t *testing.T) {
	st := store.MustTempStore(t)
	storetest.TestCmdMeta(t, st)
	storetest.TestRecords(t, st)
}
