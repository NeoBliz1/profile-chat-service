package tests

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"profile-chat-service/api"
	pkg2 "profile-chat-service/pkg"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// MockNetConn implements the net.Conn interface for testing purposes.
type MockNetConn struct {
	ReadBuffer  bytes.Buffer
	WriteBuffer bytes.Buffer
}

func (m *MockNetConn) Read(b []byte) (n int, err error) {
	return m.ReadBuffer.Read(b)
}

func (m *MockNetConn) Write(b []byte) (n int, err error) {
	return m.WriteBuffer.Write(b)
}

func (m *MockNetConn) Close() error {
	return nil
}

func (m *MockNetConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *MockNetConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 54321}
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

func TestMsgProxyResender(t *testing.T) {
	// Common setup for tests
	newTestConfig := func() *pkg2.Config {
		return &pkg2.Config{
			OriginCORS:      "http://localhost:3000",
			GCPProjectID:    "test-project",
			GCPAPIKey:       "test-api-key",
			GCPSiteKey:      "test-site-key",
			MailEmail:       "test@example.com",
			MailAppPassword: "password",
		}
	}

	t.Run("Success with reCAPTCHA", func(t *testing.T) {
		cfg := newTestConfig()

		// Mock reCAPTCHA
		recaptchaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tokenProperties": map[string]interface{}{"valid": true}})
		}))
		defer recaptchaServer.Close()
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = recaptchaServer.URL + "/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		// Mock SMTP
		smtpServer, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer smtpServer.Close()
		host, port, _ := strings.Cut(smtpServer.Addr, ":")
		cfg.SMTPHost = host
		cfg.SMTPPort = port

		// Override TlsDial to skip verification
		originalTlsDial := api.TlsDial
		api.TlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
			config.InsecureSkipVerify = true
			return tls.Dial(network, addr, config)
		}
		defer func() { api.TlsDial = originalTlsDial }()

		payload := pkg2.EmailPayload{Name: "Test", Message: "Hello", RecaptchaResponse: "valid-token"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/send", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()

		api.MsgProxyResender(rr, req, cfg)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("Success with UUID bypass", func(t *testing.T) {
		cfg := newTestConfig()

		// Mock IMAP to return a found email
		originalGetImapConnection := api.GetImapConnection
		defer func() { api.GetImapConnection = originalGetImapConnection }()

		api.GetImapConnection = func(c *pkg2.Config) (pkg2.TextprotoCommander, error) {
			mockConn := &MockNetConn{}
			mockConn.ReadBuffer.WriteString("* OK Mock IMAP Server Ready\r\n")
			mockConn.ReadBuffer.WriteString("A1 OK LOGIN completed\r\n")
			mockConn.ReadBuffer.WriteString("* 1 EXISTS\r\n")
			mockConn.ReadBuffer.WriteString("A2 OK [READ-WRITE] SELECT completed\r\n")
			mockConn.ReadBuffer.WriteString("* SEARCH 1\r\n")
			mockConn.ReadBuffer.WriteString("A3 OK SEARCH completed\r\n")
			mockConn.ReadBuffer.WriteString("A99 OK LOGOUT completed\r\n")
			return textproto.NewConn(mockConn), nil
		}

		// Mock SMTP
		smtpServer, err := NewMockTLSSMTPServer()
		assert.NoError(t, err)
		defer smtpServer.Close()
		smtpHost, smtpPort, _ := strings.Cut(smtpServer.Addr, ":")
		cfg.SMTPHost = smtpHost
		cfg.SMTPPort = smtpPort

		// Override TlsDial to skip verification
		originalTlsDial := api.TlsDial
		api.TlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
			config.InsecureSkipVerify = true
			return tls.Dial(network, addr, config)
		}
		defer func() { api.TlsDial = originalTlsDial }()

		payload := pkg2.EmailPayload{Name: "Test", Message: "Hello", Uuid: "test-uuid"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/send", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()

		api.MsgProxyResender(rr, req, cfg)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("Invalid Method", func(t *testing.T) {
		cfg := newTestConfig()
		req, _ := http.NewRequest("GET", "/api/send", nil)
		rr := httptest.NewRecorder()
		api.MsgProxyResender(rr, req, cfg)
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	})

	t.Run("Malformed JSON", func(t *testing.T) {
		cfg := newTestConfig()
		req, _ := http.NewRequest("POST", "/api/send", bytes.NewBufferString("not-json"))
		rr := httptest.NewRecorder()
		api.MsgProxyResender(rr, req, cfg)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("Invalid Payload", func(t *testing.T) {
		cfg := newTestConfig()
		payload := pkg2.EmailPayload{Name: "", Message: "Hello"} // Missing name
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/send", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()
		api.MsgProxyResender(rr, req, cfg)
		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	})

	t.Run("reCAPTCHA Fails", func(t *testing.T) {
		cfg := newTestConfig()
		recaptchaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tokenProperties": map[string]interface{}{"valid": false}})
		}))
		defer recaptchaServer.Close()
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = recaptchaServer.URL + "/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		payload := pkg2.EmailPayload{Name: "Test", Message: "Hello", RecaptchaResponse: "invalid-token"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/send", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()
		api.MsgProxyResender(rr, req, cfg)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("Send Email Fails", func(t *testing.T) {
		cfg := newTestConfig()
		// Mock reCAPTCHA to succeed
		recaptchaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tokenProperties": map[string]interface{}{"valid": true}})
		}))
		defer recaptchaServer.Close()
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = recaptchaServer.URL + "/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		// Mock SMTP to fail
		cfg.SMTPHost = "127.0.0.1"
		cfg.SMTPPort = "1" // Invalid port

		payload := pkg2.EmailPayload{Name: "Test", Message: "Hello", RecaptchaResponse: "valid-token"}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/send", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()
		api.MsgProxyResender(rr, req, cfg)
		assert.Equal(t, http.StatusBadGateway, rr.Code)
	})
}
