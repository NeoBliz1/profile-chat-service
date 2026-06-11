package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"profile-chat-service/api"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyRecaptcha(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"tokenProperties": map[string]interface{}{
					"valid": true,
				},
			})
		}))
		defer server.Close()

		cfg := &api.Config{
			GCPProjectID: "test-project",
			GCPAPIKey:    "test-api-key",
			GCPSiteKey:   "test-site-key",
		}
		// Override the URL to point to our mock server, ensuring it can handle the format args
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = server.URL + "/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		valid, err := api.VerifyRecaptcha(cfg, "valid-token")
		assert.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("Missing Config", func(t *testing.T) {
		cfg := &api.Config{}
		_, err := api.VerifyRecaptcha(cfg, "any-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing critical Google Cloud environment configurations")
	})

	t.Run("API Request Fails", func(t *testing.T) {
		cfg := &api.Config{
			GCPProjectID: "test-project",
			GCPAPIKey:    "test-api-key",
			GCPSiteKey:   "test-site-key",
		}
		// Point to a non-existent server
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = "http://localhost:12345/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		_, err := api.VerifyRecaptcha(cfg, "any-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network error")
	})

	t.Run("API Returns Non-200 Status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		cfg := &api.Config{
			GCPProjectID: "test-project",
			GCPAPIKey:    "test-api-key",
			GCPSiteKey:   "test-site-key",
		}
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = server.URL + "/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		_, err := api.VerifyRecaptcha(cfg, "any-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned failure status")
	})

	t.Run("Invalid JSON Response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("invalid-json"))
		}))
		defer server.Close()

		cfg := &api.Config{
			GCPProjectID: "test-project",
			GCPAPIKey:    "test-api-key",
			GCPSiteKey:   "test-site-key",
		}
		originalURL := api.RecaptchaAPIURL
		api.RecaptchaAPIURL = server.URL + "/?p=%s&k=%s" // Consume the extra args
		defer func() { api.RecaptchaAPIURL = originalURL }()

		_, err := api.VerifyRecaptcha(cfg, "any-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode Google JSON")
	})
}
