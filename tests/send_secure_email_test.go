package tests

import (
	"crypto/tls"
	"fmt"
	"net"
	"profile-chat-service/api"
	pkg2 "profile-chat-service/pkg"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendSecureEmail(t *testing.T) {
	payload := &pkg2.EmailPayload{
		Name:    "Test User",
		Message: "Hello, this is a test.",
		Uuid:    "test-uuid",
	}

	// Override TlsDial for all tests in this file
	originalTlsDial := api.TlsDial
	api.TlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
		config.InsecureSkipVerify = true
		return tls.Dial(network, addr, config)
	}
	defer func() { api.TlsDial = originalTlsDial }()

	t.Run("Success", func(t *testing.T) {
		server, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer server.Close()

		host, port, _ := net.SplitHostPort(server.Addr)
		cfg := config(host, port)

		err = api.SendSecureEmail(cfg, payload)
		assert.NoError(t, err)
	})

	t.Run("Missing Server Config", func(t *testing.T) {
		err := api.SendSecureEmail(&pkg2.Config{}, payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server configuration missing backend variables")
	})

	t.Run("TLS Dial Fails", func(t *testing.T) {
		err := api.SendSecureEmail(config("127.0.0.1", "1"), payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed direct TLS dial connection")
	})

	t.Run("SMTP Auth Fails", func(t *testing.T) {
		server, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer server.Close()

		server.AuthHandler = func(username, password string) error {
			return fmt.Errorf("invalid credentials")
		}

		host, port, err := net.SplitHostPort(server.Addr)
		assert.NoError(t, err)
		cfg := config(host, port)

		err = api.SendSecureEmail(cfg, payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SMTP authentication rejected")
	})

	t.Run("SMTP MAIL Fails", func(t *testing.T) {
		server, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer server.Close()

		server.MailHandler = func(from string) error {
			return fmt.Errorf("invalid sender")
		}

		host, port, _ := net.SplitHostPort(server.Addr)
		cfg := config(host, port)

		err = api.SendSecureEmail(cfg, payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SMTP MAIL transaction failed")
	})

	t.Run("SMTP RCPT Fails", func(t *testing.T) {
		server, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer server.Close()

		server.RcptHandler = func(to string) error {
			return fmt.Errorf("invalid recipient")
		}

		host, port, _ := net.SplitHostPort(server.Addr)
		cfg := config(host, port)

		err = api.SendSecureEmail(cfg, payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SMTP RCPT transaction failed")
	})

	t.Run("SMTP DATA Fails", func(t *testing.T) {
		server, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer server.Close()

		server.ForceDisconnectAfter = "DATA"

		host, port, _ := net.SplitHostPort(server.Addr)
		cfg := config(host, port)

		err = api.SendSecureEmail(cfg, payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SMTP DATA transaction opening failed")
	})

	t.Run("SMTP Data Write Fails", func(t *testing.T) {
		server, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer server.Close()

		server.DataHandler = func(data string) error {
			return fmt.Errorf("data write error")
		}

		host, port, _ := net.SplitHostPort(server.Addr)
		cfg := config(host, port)

		err = api.SendSecureEmail(cfg, payload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SMTP transaction close confirmation failed")
	})
}

func config(host string, port string) *pkg2.Config {
	cfg := &pkg2.Config{
		MailEmail:       "test@example.com",
		MailAppPassword: "password",
		SMTPHost:        host,
		SMTPPort:        port,
		IMAPHost:        "host",
	}
	return cfg
}
