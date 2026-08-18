// Package terminal proxies an interactive SSH session to the device so the web
// UI can offer a shell (xterm.js in the browser ↔ WebSocket ↔ this SSH client).
// The SSH target is fixed by configuration (localhost) — the browser only
// supplies the username/password — so the app can't be used as an SSH pivot.
package terminal

import (
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// Service holds terminal configuration.
type Service struct {
	enabled bool
	sshAddr string // fixed SSH target, e.g. "127.0.0.1:22"
}

// New builds a terminal service. When disabled, no sessions can be opened.
func New(enabled bool, sshAddr string) *Service {
	if sshAddr == "" {
		sshAddr = "127.0.0.1:22"
	}
	return &Service{enabled: enabled, sshAddr: sshAddr}
}

func (s *Service) Enabled() bool   { return s.enabled }
func (s *Service) SSHAddr() string { return s.sshAddr }

// Session is a live SSH shell with a PTY. Read yields combined stdout+stderr;
// Write feeds stdin; Resize changes the PTY window.
type Session struct {
	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser
	out    *io.PipeReader
}

// Dial opens an SSH connection to the configured target and starts a login
// shell with a PTY of the given size.
func (s *Service) Dial(user, password string, cols, rows int) (*Session, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // localhost, device-local trust
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", s.sshAddr, cfg)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw

	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	go func() { sess.Wait(); pw.Close() }() // close the reader when the shell exits

	return &Session{client: client, sess: sess, stdin: stdin, out: pr}, nil
}

func (s *Session) Read(p []byte) (int, error)  { return s.out.Read(p) }
func (s *Session) Write(p []byte) (int, error) { return s.stdin.Write(p) }

// Resize changes the PTY window size.
func (s *Session) Resize(cols, rows int) error { return s.sess.WindowChange(rows, cols) }

// Close ends the session and connection.
func (s *Session) Close() error {
	s.sess.Close()
	return s.client.Close()
}
