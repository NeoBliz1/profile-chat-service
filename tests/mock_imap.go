package tests

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// MockNetConn implements net.Conn for testing purposes.
type MockNetConn struct {
	ReadBuffer  bytes.Buffer
	WriteBuffer bytes.Buffer
	CloseCalled bool
	Local       net.Addr
	Remote      net.Addr
}

func (m *MockNetConn) Read(b []byte) (n int, err error) {
	return m.ReadBuffer.Read(b)
}

func (m *MockNetConn) Write(b []byte) (n int, err error) {
	return m.WriteBuffer.Write(b)
}

func (m *MockNetConn) Close() error {
	m.CloseCalled = true
	return nil
}

func (m *MockNetConn) LocalAddr() net.Addr {
	if m.Local == nil {
		return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	}
	return m.Local
}

func (m *MockNetConn) RemoteAddr() net.Addr {
	if m.Remote == nil {
		return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
	}
	return m.Remote
}

func (m *MockNetConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *MockNetConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *MockNetConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// MockIMAPServer is a mock IMAP server for testing.
type MockIMAPServer struct {
	Addr     string
	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex

	// Handlers for different IMAP commands
	LoginHandler  func(username, password string) error
	SelectHandler func(mailbox string) error
	SearchHandler func(criteria string) ([]string, error)
	FetchHandler  func(sequenceSet, item string) ([]string, error)
	LogoutHandler func() error

	// Controls for forcing errors
	ForceDisconnectAfter string
}

// NewMockIMAPServer creates a new mock IMAP server.
func NewMockIMAPServer() (*MockIMAPServer, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not listen on a port: %w", err)
	}
	s := &MockIMAPServer{
		Addr:     l.Addr().String(),
		listener: l,
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serve()
	}()
	return s, nil
}

// Close stops the mock server.
func (s *MockIMAPServer) Close() {
	s.listener.Close()
	s.wg.Wait()
}

func (s *MockIMAPServer) serve() {
	conn, err := s.listener.Accept()
	if err != nil {
		return // Server closed
	}
	defer conn.Close()
	s.handleConnection(conn)
}

func (s *MockIMAPServer) handleConnection(conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Greet the client
	write(writer, "* OK Mock IMAP Server Ready")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		tag, command := parts[0], strings.ToUpper(parts[1])

		s.mu.Lock()
		forceDisconnect := s.ForceDisconnectAfter == command
		s.mu.Unlock()
		if forceDisconnect {
			conn.Close()
			return
		}

		switch command {
		case "LOGIN":
			if s.LoginHandler != nil {
				if err := s.LoginHandler("", ""); err != nil {
					write(writer, fmt.Sprintf("%s NO %s", tag, err.Error()))
					continue
				}
			}
			write(writer, fmt.Sprintf("%s OK LOGIN completed", tag))
		case "SELECT":
			mailbox := ""
			if len(parts) > 2 {
				mailbox = parts[2]
			}
			if s.SelectHandler != nil {
				if err := s.SelectHandler(mailbox); err != nil {
					write(writer, fmt.Sprintf("%s NO %s", tag, err.Error()))
					continue
				}
			}
			write(writer, "* 1 EXISTS")
			write(writer, fmt.Sprintf("%s OK [READ-WRITE] SELECT completed", tag))
		case "SEARCH":
			criteria := ""
			if len(parts) > 2 {
				criteria = strings.Join(parts[2:], " ")
			}
			if s.SearchHandler != nil {
				results, err := s.SearchHandler(criteria)
				if err != nil {
					write(writer, fmt.Sprintf("%s NO %s", tag, err.Error()))
					continue
				}
				write(writer, fmt.Sprintf("* SEARCH %s", strings.Join(results, " ")))
			}
			write(writer, fmt.Sprintf("%s OK SEARCH completed", tag))
		case "FETCH":
			sequenceSet, item := parts[2], parts[3]
			if s.FetchHandler != nil {
				lines, err := s.FetchHandler(sequenceSet, item)
				if err != nil {
					write(writer, fmt.Sprintf("%s NO %s", tag, err.Error()))
					continue
				}
				for _, l := range lines {
					write(writer, l)
				}
			}
			write(writer, fmt.Sprintf("%s OK FETCH completed", tag))
		case "LOGOUT":
			if s.LogoutHandler != nil {
				_ = s.LogoutHandler()
			}
			write(writer, "* BYE IMAP server shutting down")
			write(writer, fmt.Sprintf("%s OK LOGOUT completed", tag))
			return
		default:
			write(writer, fmt.Sprintf("%s BAD Unrecognized command", tag))
		}
	}
}
