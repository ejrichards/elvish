package docsite

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"src.elv.sh/pkg/prog"
)

// Program is the subprogram for the -docs flag: it serves the embedded
// website on the loopback interface until interrupted.
type Program struct {
	docs    bool
	port    int
	browser bool
	fs      *flag.FlagSet

	// Used in tests.
	listening func(url string)
	stop      <-chan struct{}
}

func (p *Program) RegisterFlags(fs *prog.FlagSet) {
	fs.BoolVar(&p.docs, "docs", false,
		"Serve the documentation website on localhost and print its URL")
	fs.IntVar(&p.port, "port", 0,
		"With -docs: port to listen on; 0 picks a free one")
	fs.BoolVar(&p.browser, "browser", true,
		"With -docs: open the website in a browser; on by default unless -port is given")
	p.fs = fs.FlagSet
}

func (p *Program) Run(fds [3]*os.File, args []string) error {
	if !p.docs {
		return prog.NextProgram()
	}
	if len(args) > 0 {
		return prog.BadUsage("arguments are not allowed with -docs")
	}
	// Like "zig std": open a browser when the port is ephemeral, since then
	// the URL is unknown in advance, unless told otherwise either way.
	browserGiven := false
	p.fs.Visit(func(f *flag.Flag) {
		if f.Name == "browser" {
			browserGiven = true
		}
	})
	open := p.browser
	if !browserGiven {
		open = p.port == 0
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)
	fmt.Fprintln(fds[1], url)
	if _, built := Site(); !built {
		fmt.Fprintln(fds[2], "Warning: the website is not built into this binary; serving a page that says so.")
	}
	if p.listening != nil {
		p.listening(url)
	}
	if open {
		if err := openBrowser(url); err != nil {
			fmt.Fprintln(fds[2], "Warning: cannot open browser:", err)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Handler: Handler()}
	go func() {
		select {
		case <-ctx.Done():
		case <-p.stop:
		}
		server.Shutdown(context.Background())
	}()
	err = server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Opens the URL in the default browser, without waiting for the browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap it whenever it exits; some openers block for the browser's life.
	go cmd.Wait()
	return nil
}
