package sync

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// The command run on the remote. It is also what a forced command in the
// remote's authorized_keys should run.
var remoteCommand = []string{"elvish", "-relay"}

// Runs "elvish -relay" on the remote over ssh and returns a connection to its
// standard streams. Closing the connection closes the remote's input and
// waits for ssh to exit.
//
// BatchMode makes ssh fail instead of prompting: there is no terminal when
// run from a timer, and the key should be passwordless anyway. The remote's
// standard error goes to stderr so that ssh and relay errors are visible.
func dialSSH(remote string, stderr io.Writer) (io.ReadWriteCloser, error) {
	// A plain pipe rather than exec's StdoutPipe: exec closes its pipe in
	// Wait, which would race with the RPC client still reading from it.
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	args := append([]string{"-o", "BatchMode=yes", "--", remote}, remoteCommand...)
	cmd := exec.Command("ssh", args...)
	cmd.Stdout = outW
	cmd.Stderr = stderr
	inW, err := cmd.StdinPipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("run ssh: %w", err)
	}
	// The child has its own copy; keep ours closed so that reads see EOF
	// when the child exits.
	outW.Close()
	return &sshConn{outR, inW, cmd}, nil
}

type sshConn struct {
	out io.ReadCloser
	in  io.WriteCloser
	cmd *exec.Cmd
}

func (c *sshConn) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *sshConn) Write(p []byte) (int, error) { return c.in.Write(p) }

func (c *sshConn) Close() error {
	// Closing the input ends the relay on the remote, which ends ssh.
	err := c.in.Close()
	if werr := c.cmd.Wait(); werr != nil {
		var exitErr *exec.ExitError
		if errors.As(werr, &exitErr) {
			werr = fmt.Errorf("ssh exited with %d", exitErr.ExitCode())
		}
		err = errors.Join(err, werr)
	}
	return errors.Join(err, c.out.Close())
}
