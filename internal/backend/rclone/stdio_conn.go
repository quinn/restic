package rclone

import (
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/quinn/restic/internal/debug"
)

// StdioConn implements a net.Conn via stdin/stdout.
type StdioConn struct {
	stdin  *os.File
	stdout *os.File
	cmd    *exec.Cmd
	close  sync.Once
}

func (s *StdioConn) Read(p []byte) (int, error) {
	n, err := s.stdin.Read(p)
	return n, err
}

func (s *StdioConn) Write(p []byte) (int, error) {
	n, err := s.stdout.Write(p)
	return n, err
}

// Close closes both streams.
func (s *StdioConn) Close() (err error) {
	s.close.Do(func() {
		debug.Log("close stdio connection")
		var errs []error

		for _, f := range []func() error{s.stdin.Close, s.stdout.Close} {
			err := f()
			if err != nil {
				errs = append(errs, err)
			}
		}

		if len(errs) > 0 {
			err = errs[0]
		}
	})

	return err
}

// LocalAddr returns nil.
func (s *StdioConn) LocalAddr() net.Addr {
	return Addr{}
}

// RemoteAddr returns nil.
func (s *StdioConn) RemoteAddr() net.Addr {
	return Addr{}
}

// SetDeadline sets the read/write deadline.
func (s *StdioConn) SetDeadline(t time.Time) error {
	err1 := s.stdin.SetReadDeadline(t)
	err2 := s.stdout.SetWriteDeadline(t)
	if err1 != nil {
		return err1
	}
	return err2
}

// SetReadDeadline sets the read/write deadline.
func (s *StdioConn) SetReadDeadline(t time.Time) error {
	return s.stdin.SetReadDeadline(t)
}

// SetWriteDeadline sets the read/write deadline.
func (s *StdioConn) SetWriteDeadline(t time.Time) error {
	return s.stdout.SetWriteDeadline(t)
}

// make sure StdioConn implements net.Conn
var _ net.Conn = &StdioConn{}

// Addr implements net.Addr for stdin/stdout.
type Addr struct{}

// Network returns the network type as a string.
func (a Addr) Network() string {
	return "stdio"
}

func (a Addr) String() string {
	return "stdio"
}
