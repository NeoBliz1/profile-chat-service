package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"profile-chat-service/api"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteErrorResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	api.WriteErrorResponse(rr, http.StatusBadRequest, "Test Error")

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp api.ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "Test Error", resp.Error)
}

func TestSetupCORS(t *testing.T) {
	t.Run("CORS Origin Set", func(t *testing.T) {
		cfg := &api.Config{OriginCORS: "http://localhost:3000"}
		rr := httptest.NewRecorder()
		err := api.SetupCORS(rr, cfg)

		assert.NoError(t, err)
		assert.Equal(t, "http://localhost:3000", rr.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "POST, OPTIONS", rr.Header().Get("Access-Control-Allow-Methods"))
		assert.Equal(t, "Content-Type", rr.Header().Get("Access-Control-Allow-Headers"))
	})

	t.Run("CORS Origin Not Set", func(t *testing.T) {
		cfg := &api.Config{}
		rr := httptest.NewRecorder()
		err := api.SetupCORS(rr, cfg)
		assert.Error(t, err)
	})
}

func TestValidatePayload(t *testing.T) {
	tests := []struct {
		name          string
		payload       *api.EmailPayload
		expectedError string
	}{
		{
			name:          "Valid Payload",
			payload:       &api.EmailPayload{Name: "Test", Message: "Test Message", RecaptchaResponse: "token"},
			expectedError: "",
		},
		{
			name:          "Empty Name",
			payload:       &api.EmailPayload{Name: " ", Message: "Test Message", RecaptchaResponse: "token"},
			expectedError: "Name and message fields cannot be empty",
		},
		{
			name:          "Empty Message",
			payload:       &api.EmailPayload{Name: "Test", Message: " ", RecaptchaResponse: "token"},
			expectedError: "Name and message fields cannot be empty",
		},
		{
			name:          "Missing Token",
			payload:       &api.EmailPayload{Name: "Test", Message: "Test Message"},
			expectedError: "Missing required reCAPTCHA or Uuid token verification parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := api.ValidatePayload(tt.payload)
			assert.Equal(t, tt.expectedError, errStr)
		})
	}
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No HTML",
			input:    "Just a regular string.",
			expected: "Just a regular string.",
		},
		{
			name:     "With HTML",
			input:    "<p>Hello, world!</p>",
			expected: "&lt;p&gt;Hello, world!&lt;&#x2F;p&gt;",
		},
		{
			name:     "With Spaces",
			input:    "  leading and trailing spaces  ",
			expected: "leading and trailing spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := api.SanitizeInput(tt.input)
			assert.Equal(t, tt.expected, sanitized)
		})
	}
}

func TestSendImapCommand(t *testing.T) {
	t.Run("Successful command", func(t *testing.T) {
		mockProto := &MockTextprotoCommander{}
		lines := []string{
			"* SEARCH 1 2",
			"A1 OK SEARCH completed",
		}
		lineIdx := 0
		mockProto.ReadLineFunc = func() (string, error) {
			if lineIdx >= len(lines) {
				return "", nil
			}
			line := lines[lineIdx]
			lineIdx++
			return line, nil
		}
		_, err := api.SendImapCommand(mockProto, "A1", "SEARCH")
		assert.NoError(t, err)
	})
}
