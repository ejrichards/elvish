package sync

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"src.elv.sh/pkg/daemon"
	"src.elv.sh/pkg/daemon/daemondefs"
	"src.elv.sh/pkg/prog"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/testutil"
)

func TestRelay_ExposesDaemonOnStdio(t *testing.T) {
	setup(t)
	startDaemon(t, "sock", "db")
	remote, done := startRelay(t, "sock", "db")

	version, err := remote.Version()
	if err != nil || version != daemon.APIVersion {
		t.Fatalf("Version over relay => (%v, %v), want (%v, nil)", version, err, daemon.APIVersion)
	}
	host, err := remote.HostID()
	if err != nil {
		t.Fatal(err)
	}
	record := storedefs.Record{
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: host,
		Idx: 1, Op: storedefs.OpAdd, ID: storedefs.NewID(), Text: "over relay",
	}
	if err := remote.Ingest([]storedefs.Record{record}); err != nil {
		t.Fatal(err)
	}
	if st, err := remote.Status(); err != nil || st[host][storedefs.KindCmd] != 1 {
		t.Errorf("Status over relay => (%v, %v), want tail 1", st, err)
	}

	// Closing the client closes the relay's input; the relay then exits
	// cleanly and the daemon, having no clients left, exits too.
	remote.Close()
	if exit := waitExit(t, done); exit != 0 {
		t.Errorf("relay exited with %d, want 0", exit)
	}
}

func TestSyncClients_ThroughRelay(t *testing.T) {
	setup(t)
	// The host ID lives beside the database, so each daemon needs its own
	// directory to be its own host.
	os.Mkdir("a", 0700)
	os.Mkdir("b", 0700)
	startDaemon(t, "sock-a", "a/db")
	startDaemon(t, "sock-b", "b/db")
	local := daemon.NewClient("sock-a")
	t.Cleanup(func() { local.Close() })
	remote, done := startRelay(t, "sock-b", "b/db")
	defer waitExit(t, done)
	defer remote.Close()

	addCmds(t, local, "local cmd")
	addCmds(t, remote, "remote cmd")
	res, err := SyncClients(local, remote)
	if err != nil {
		t.Fatalf("SyncClients => %v", err)
	}
	if (res != Result{Pulled: 1, Pushed: 1}) {
		t.Errorf("SyncClients => %+v, want 1 pulled, 1 pushed", res)
	}
	if got := texts(t, local); !sameSet(got, []string{"local cmd", "remote cmd"}) {
		t.Errorf("local has %v", got)
	}
	if got := texts(t, remote); !sameSet(got, []string{"local cmd", "remote cmd"}) {
		t.Errorf("remote has %v", got)
	}
}

func TestSyncClients_RefusesOldRemote(t *testing.T) {
	// Not tested against a real outdated daemon: activating one kills it,
	// and in a test the daemon is this process.
	remote := &fakeClient{version: daemon.APIVersion - 1}
	_, err := SyncClients(&fakeClient{}, remote)
	if err == nil || !strings.Contains(err.Error(), "older") {
		t.Errorf("SyncClients with old remote => %v, want version error", err)
	}
	remote.err = errInjected
	if _, err := SyncClients(&fakeClient{}, remote); !errors.Is(err, errInjected) {
		t.Errorf("SyncClients with unreachable remote => %v, want injected", err)
	}
}

func TestProgram_BadUsage(t *testing.T) {
	setup(t)
	for _, args := range [][]string{
		{"elvish", "-sync"},
		{"elvish", "-sync", "a", "b"},
		{"elvish", "-relay", "arg"},
		{"elvish", "-sync", "-relay", "host"},
		{"elvish", "-reset"},
		{"elvish", "-reset", "-relay"},
	} {
		if exit := run(t, args, nil, io.Discard); exit != 2 {
			t.Errorf("%v => exit %d, want 2", args, exit)
		}
	}
}

func TestProgram_NextProgramWithoutFlags(t *testing.T) {
	setup(t)
	p := &Program{}
	if err := p.Run([3]*os.File{}, nil); err == nil {
		t.Errorf("Run without flags => nil, want NextProgram")
	}
}

func TestStamp(t *testing.T) {
	setup(t)
	if err := stamp("stamp"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile("stamp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(first))); err != nil {
		t.Errorf("stamp content %q is not a time: %v", first, err)
	}
	if err := stamp("stamp"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile("stamp")
	if string(second) == string(first) {
		t.Errorf("stamp content did not change: %q", second)
	}
}

// Helpers.

func setup(t *testing.T) {
	testutil.Umask(t, 0)
	dir := testutil.InTempDir(t)
	// The run directory is derived from XDG_RUNTIME_DIR; keep it in the test
	// directory rather than the user's.
	testutil.Setenv(t, "XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
}

// Serves a daemon in a goroutine until all its clients have disconnected.
func startDaemon(t *testing.T, sock, db string) {
	t.Helper()
	sigCh := make(chan os.Signal)
	readyCh := make(chan struct{})
	opts := daemon.ServeOpts{Signals: sigCh, Ready: readyCh}
	doneCh := make(chan struct{})
	go func() {
		daemon.Serve(sock, db, opts)
		close(doneCh)
	}()
	select {
	case <-readyCh:
	case <-time.After(testutil.Scaled(2 * time.Second)):
		t.Fatal("timed out waiting for daemon to start")
	}
	t.Cleanup(func() {
		close(sigCh)
		select {
		case <-doneCh:
		case <-time.After(testutil.Scaled(2 * time.Second)):
			t.Error("timed out waiting for daemon to quit")
		}
	})
}

// Runs "elvish -relay" in a goroutine with pipes as its standard streams,
// and returns a daemon client on the other end of the pipes and a channel
// that receives the exit status.
func startRelay(t *testing.T, sock, db string) (daemondefs.Client, <-chan int) {
	t.Helper()
	inR, inW := mustPipe(t)
	outR, outW := mustPipe(t)
	done := make(chan int, 1)
	go func() {
		done <- run(t, []string{"elvish", "-relay", "-sock", sock, "-db", db},
			inR, outW)
		inR.Close()
		outW.Close()
	}()
	return daemon.NewClientConn(pipeConn{outR, inW}), done
}

func run(t *testing.T, args []string, stdin *os.File, stdout io.Writer) int {
	t.Helper()
	if stdin == nil {
		stdin = devNull(t)
	}
	out, ok := stdout.(*os.File)
	if !ok {
		out = devNull(t)
	}
	return prog.Run([3]*os.File{stdin, out, os.Stderr}, args, &Program{})
}

func devNull(t *testing.T) *os.File {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func mustPipe(t *testing.T) (*os.File, *os.File) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return r, w
}

func waitExit(t *testing.T, done <-chan int) int {
	t.Helper()
	select {
	case exit := <-done:
		return exit
	case <-time.After(testutil.Scaled(2 * time.Second)):
		t.Fatal("timed out waiting for relay to exit")
		return -1
	}
}

type pipeConn struct {
	r *os.File
	w *os.File
}

func (c pipeConn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c pipeConn) Write(p []byte) (int, error) { return c.w.Write(p) }
func (c pipeConn) Close() error {
	c.w.Close()
	return c.r.Close()
}

// A client whose Version is canned. Every other method panics, which is what
// should happen: SyncClients must refuse the remote before touching either.
type fakeClient struct {
	daemondefs.Client
	version int
	err     error
}

func (c *fakeClient) Version() (int, error) { return c.version, c.err }
