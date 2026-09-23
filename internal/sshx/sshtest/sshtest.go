// Package sshtest runs a minimal SSH server for plugin tests: password login for one
// user, exec requests answered by a handler.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Handler answers an exec request.
type Handler func(cmd string) (stdout string, exitCode int)

// Server is a running test server.
type Server struct {
	Addr *net.TCPAddr

	mu       sync.Mutex
	commands []string
	logins   int
}

// New starts a server accepting user/password; it stops when the test ends.
func New(t testing.TB, user, password string, h Handler) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &Server{Addr: ln.Addr().(*net.TCPAddr)}
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == user && string(pw) == password {
			s.mu.Lock()
			s.logins++
			s.mu.Unlock()
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(hostKey)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, cfg, h)
		}
	}()
	return s
}

// Port returns the listening port.
func (s *Server) Port() int { return s.Addr.Port }

// Commands returns the executed commands.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// Logins returns the number of successful logins.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

func (s *Server) serve(conn net.Conn, cfg *ssh.ServerConfig, h Handler) {
	defer conn.Close()
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, requests, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" || len(req.Payload) < 4 {
					_ = req.Reply(false, nil)
					continue
				}
				n := binary.BigEndian.Uint32(req.Payload[:4])
				cmd := string(req.Payload[4 : 4+n])
				s.mu.Lock()
				s.commands = append(s.commands, cmd)
				s.mu.Unlock()
				_ = req.Reply(true, nil)
				out, code := h(cmd)
				_, _ = ch.Write([]byte(out))
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, uint32(code))
				_, _ = ch.SendRequest("exit-status", false, status)
				return
			}
		}()
	}
}
