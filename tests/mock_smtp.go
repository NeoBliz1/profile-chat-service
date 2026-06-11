package tests

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// MockSMTPServer is a mock SMTP server for testing.
type MockSMTPServer struct {
	Addr     string
	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	// Handlers for different SMTP commands
	AuthHandler func(username, password string) error
	MailHandler func(from string) error
	RcptHandler func(to string) error
	DataHandler func(data string) error
	QuitHandler func() error

	// Controls for forcing errors
	ForceDisconnectAfter string // Disconnect after a specific command, e.g., "AUTH"
}

// NewMockSMTPServer creates a new mock SMTP server.
func NewMockSMTPServer() (*MockSMTPServer, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not listen on a port: %w", err)
	}
	s := &MockSMTPServer{
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

// NewMockTLSSMTPServer creates a new mock SMTP server with TLS.
func NewMockTLSSMTPServer() (*MockSMTPServer, error) {
	// Generate a self-signed certificate for testing
	cert, err := generateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("could not generate cert: %w", err)
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	l, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("could not listen on a port: %w", err)
	}
	s := &MockSMTPServer{
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

// generateSelfSignedCert generates a self-signed certificate for testing
func generateSelfSignedCert() (tls.Certificate, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test SMTP Server"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %w", err)
	}

	// PEM encode the certificate and private key
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	// Parse the PEM encoded certificate and key
	return tls.X509KeyPair(certPEM, keyPEM)
}

// Close stops the mock server.
func (s *MockSMTPServer) Close() {
	s.listener.Close()
	s.wg.Wait()
}

func (s *MockSMTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // Server closed
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.handleConnection(conn)
		}()
	}
}

func (s *MockSMTPServer) handleConnection(conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// Greet the client
	write(writer, "220 mock.smtp.server ESMTP")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		command := strings.ToUpper(parts[0])

		s.mu.Lock()
		forceDisconnect := s.ForceDisconnectAfter == command
		s.mu.Unlock()
		if forceDisconnect {
			conn.Close()
			return
		}

		switch command {
		case "EHLO", "HELO":
			write(writer, "250-mock.smtp.server")
			write(writer, "250 AUTH PLAIN")
		case "AUTH":
			if s.AuthHandler != nil {
				// Simplified PLAIN auth parsing
				if len(parts) > 1 && strings.ToUpper(parts[1]) == "PLAIN" {
					if err := s.AuthHandler("", ""); err != nil {
						write(writer, fmt.Sprintf("535 %s", err.Error()))
						continue
					}
				}
			}
			write(writer, "235 2.7.0 Authentication successful")
		case "MAIL":
			from := ""
			if len(parts) > 1 {
				from = strings.TrimPrefix(parts[1], "FROM:")
			}
			if s.MailHandler != nil {
				if err := s.MailHandler(from); err != nil {
					write(writer, fmt.Sprintf("550 %s", err.Error()))
					continue
				}
			}
			write(writer, "250 2.1.0 OK")
		case "RCPT":
			to := ""
			if len(parts) > 1 {
				to = strings.TrimPrefix(parts[1], "TO:")
			}
			if s.RcptHandler != nil {
				if err := s.RcptHandler(to); err != nil {
					write(writer, fmt.Sprintf("550 %s", err.Error()))
					continue
				}
			}
			write(writer, "250 2.1.5 OK")
		case "DATA":
			write(writer, "354 End data with <CR><LF>.<CR><LF>")
			var data strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				data.WriteString(dataLine)
			}
			if s.DataHandler != nil {
				if err := s.DataHandler(data.String()); err != nil {
					write(writer, fmt.Sprintf("554 %s", err.Error()))
					continue
				}
			}
			write(writer, "250 2.0.0 OK: queued")
		case "QUIT":
			if s.QuitHandler != nil {
				_ = s.QuitHandler()
			}
			write(writer, "221 2.0.0 Bye")
			return
		default:
			write(writer, "500 5.5.1 Unrecognized command")
		}
	}
}

func write(w *bufio.Writer, msg string) {
	_, _ = w.WriteString(msg + "\r\n")
	_ = w.Flush()
}
