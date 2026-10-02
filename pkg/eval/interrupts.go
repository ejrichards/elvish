package eval

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// ErrInterrupted is thrown when the execution is interrupted by a signal.
var ErrInterrupted = errors.New("interrupted")

var interruptListeners = struct {
	sync.Mutex
	cancels map[context.Context]context.CancelFunc
}{cancels: make(map[context.Context]context.CancelFunc)}

// Interrupt cancels current signal-listening contexts when an interrupt key
// is delivered by a keyboard protocol rather than the terminal driver.
func Interrupt() {
	interruptListeners.Lock()
	defer interruptListeners.Unlock()
	for _, cancel := range interruptListeners.cancels {
		cancel()
	}
}

// ListenInterrupts returns a Context that is canceled when SIGINT or SIGQUIT
// has been received by the process. It also returns a function to cancel the
// Context, which should be called when it is no longer needed.
// An interrupt key handled by the editor may also cancel it via Interrupt.
func ListenInterrupts() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	interruptListeners.Lock()
	interruptListeners.cancels[ctx] = cancel
	interruptListeners.Unlock()
	done := func() {
		cancel()
		interruptListeners.Lock()
		delete(interruptListeners.cancels, ctx)
		interruptListeners.Unlock()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGQUIT)

	go func() {
		defer done()
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	return ctx, done
}
