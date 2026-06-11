package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// setup sets the necessary environment variables for testing.
func setup() {
	os.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
	os.Setenv("GCP_PROJECT_ID", "test-project")
	os.Setenv("GCP_API_KEY", "test-api-key")
	os.Setenv("GCP_SITE_KEY", "test-site-key")
	os.Setenv("MAIL_EMAIL", "test@example.com")
	os.Setenv("MAIL_APP_PASSWORD", "test-password")
	os.Setenv("SMTP_HOST", "smtp.example.com")
	os.Setenv("SMTP_PORT", "587")
	os.Setenv("IMAP_HOST", "imap.example.com")
}

// TestHandler tests the main Vercel handler routing.
func TestHandler(t *testing.T) {
	setup()

	// Test case for /api/send
	t.Run("SendRoute", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(`{}`))
		rr := httptest.NewRecorder()
		Handler(rr, req)
		assert.NotEqual(t, http.StatusNotFound, rr.Code, "Handler should route /api/send")
	})

	// Test case for /api/check
	t.Run("CheckRoute", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/check?uuid=test-uuid", nil)
		rr := httptest.NewRecorder()
		Handler(rr, req)
		assert.NotEqual(t, http.StatusNotFound, rr.Code, "Handler should route /api/check")
	})

	// Test case for a non-existent route
	t.Run("NotFoundRoute", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
		rr := httptest.NewRecorder()
		Handler(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code, "Handler should return 404 for unknown routes")
	})
}
