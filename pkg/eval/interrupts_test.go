package eval

import "testing"

func TestInterrupt(t *testing.T) {
	first, doneFirst := ListenInterrupts()
	defer doneFirst()
	second, doneSecond := ListenInterrupts()
	defer doneSecond()
	Interrupt()
	if first.Err() == nil || second.Err() == nil {
		t.Fatal("interrupt did not cancel active contexts")
	}
	next, doneNext := ListenInterrupts()
	defer doneNext()
	if next.Err() != nil {
		t.Fatal("interrupt cancelled a later context")
	}
}
