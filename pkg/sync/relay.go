package sync

import (
	"fmt"
	"io"
	"net"
	"os"

	"src.elv.sh/pkg/daemon"
	"src.elv.sh/pkg/daemon/daemondefs"
)

// Relay activates the local daemon and copies bytes between the standard
// streams and the daemon's socket until the input closes or the daemon hangs
// up. It is what "elvish -sync" runs on the remote over ssh; the daemon
// client on the other end speaks its usual RPC through the pipe without
// knowing it is remote. The connections are held for the whole run, which
// keeps the daemon alive for as long as the relay lives.
func Relay(fds [3]*os.File, spawnCfg *daemondefs.SpawnConfig) error {
	cl, err := daemon.Activate(fds[2], spawnCfg)
	if err != nil {
		return fmt.Errorf("activate daemon: %w", err)
	}
	// The activation client stays connected until the relay is done. The
	// daemon exits when its last client disconnects, and it registers new
	// connections asynchronously, so closing the activation client right
	// after dialing the relay's connection can leave the daemon counting
	// zero clients and exiting with the relay's connection still in flight.
	defer cl.Close()
	conn, err := net.Dial("unix", spawnCfg.SockPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, fds[0])
		// Tell the daemon there is no more input; it then finishes any
		// in-flight request, closes, and the copy below sees EOF.
		if uc, ok := conn.(*net.UnixConn); ok {
			uc.CloseWrite()
		}
		done <- err
	}()
	go func() {
		_, err := io.Copy(fds[1], conn)
		done <- err
	}()
	// Either direction ending ends the relay. If the daemon hung up, the
	// input copy is still blocked on the standard input; the process exit
	// takes care of it.
	if err := <-done; err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	return nil
}
