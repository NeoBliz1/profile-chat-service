package tests

import (
	"fmt"
	"profile-chat-service/api"
	"profile-chat-service/pkg"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockTextprotoCommander implements the api.TextprotoCommander interface for testing.
type MockTextprotoCommander struct {
	CmdFunc         func(format string, args ...interface{}) (uint, error)
	ReadLineFunc    func() (string, error)
	StartResponseMu sync.Mutex
	EndResponseMu   sync.Mutex
	CloseCalled     bool
	Commands        []string
}

func (m *MockTextprotoCommander) Cmd(format string, args ...interface{}) (uint, error) {
	m.Commands = append(m.Commands, fmt.Sprintf(format, args...))
	if m.CmdFunc != nil {
		return m.CmdFunc(format, args...)
	}
	return 1, nil // Default command ID
}

func (m *MockTextprotoCommander) StartResponse(_ uint) {
	m.StartResponseMu.Lock()
	defer m.StartResponseMu.Unlock()
	// No-op for mock
}

func (m *MockTextprotoCommander) EndResponse(_ uint) {
	m.EndResponseMu.Lock()
	defer m.EndResponseMu.Unlock()
	// No-op for mock
}

func (m *MockTextprotoCommander) ReadLine() (string, error) {
	if m.ReadLineFunc != nil {
		return m.ReadLineFunc()
	}
	return "", nil // Default empty line
}

// Close method for MockTextprotoCommander to satisfy the *textproto.Conn type assertion in api/update.go
func (m *MockTextprotoCommander) Close() error {
	m.CloseCalled = true
	return nil
}

func TestFindEmailByUuid(t *testing.T) {
	t.Run("Success - Found", func(t *testing.T) {
		mockProto := &MockTextprotoCommander{}
		lines := []string{
			"A2 OK SELECT completed",
			"* SEARCH 1",
			"A3 OK SEARCH completed",
		}
		lineIdx := 0
		mockProto.ReadLineFunc = func() (string, error) {
			if lineIdx >= len(lines) {
				return "", fmt.Errorf("EOF")
			}
			line := lines[lineIdx]
			lineIdx++
			return line, nil
		}

		found, err := api.FindEmailByUuid(mockProto, "test-uuid")
		assert.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("Success - Not Found", func(t *testing.T) {
		mockProto := &MockTextprotoCommander{}
		lines := []string{
			"A2 OK SELECT completed",
			"* SEARCH", // No message IDs
			"A3 OK SEARCH completed",
		}
		lineIdx := 0
		mockProto.ReadLineFunc = func() (string, error) {
			if lineIdx >= len(lines) {
				return "", fmt.Errorf("EOF")
			}
			line := lines[lineIdx]
			lineIdx++
			return line, nil
		}

		found, err := api.FindEmailByUuid(mockProto, "test-uuid")
		assert.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("IMAP Command Fails", func(t *testing.T) {
		mockProto := &MockTextprotoCommander{}
		mockProto.CmdFunc = func(format string, args ...interface{}) (uint, error) {
			return 0, fmt.Errorf("cmd error")
		}
		_, err := api.FindEmailByUuid(mockProto, "test-uuid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cmd error")
	})

}

func TestGetAllEmailsChainByUuid(t *testing.T) {
	cfg := &pkg.Config{MailEmail: "app@example.com"}

	t.Run("Differentiates Sender by Subject", func(t *testing.T) {
		mockProto := &MockTextprotoCommander{}
		var mu sync.Mutex
		var lineIdx int

		lines := []string{
			// S0 SELECT "INBOX"
			"S0 OK SELECT completed",
			// H1 SEARCH
			"* SEARCH 1 2 3",
			"H1 OK SEARCH completed",

			// Message 1: Initial message from the app (outerUser)
			// FL2 FETCH 1 (FLAGS)
			"* 1 FETCH (FLAGS (\\Recent))",
			"FL2 OK FETCH completed",
			// F3 FETCH 1 (Body)
			`* 1 FETCH (INTERNALDATE "01-Jan-2023 10:00:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)] {80}`,
			"From: app@example.com",
			"To: app@example.com",
			"Subject: New Submission",
			"",
			"RFC822.TEXT {10}",
			"Message:\nmsg1",
			")",
			"F3 OK FETCH completed",
			// ST4 STORE 1
			"ST4 OK STORE completed",

			// Message 2: Reply from the user (innerUser)
			// FL5 FETCH 2 (FLAGS)
			"* 2 FETCH (FLAGS (\\Recent))",
			"FL5 OK FETCH completed",
			// F6 FETCH 2 (Body)
			`* 2 FETCH (INTERNALDATE "01-Jan-2023 10:05:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)] {84}`,
			"From: app@example.com", // From is still the app's email
			"To: app@example.com",
			"Subject: Re: New Submission", // Subject indicates it's a reply
			"",
			"RFC822.TEXT {121}",
			"This is a reply",
			")",
			"F6 OK FETCH completed",
			// ST7 STORE 2
			"ST7 OK STORE completed",

			// Message 3: Standard reply from a different user email (innerUser)
			// FL8 FETCH 3 (FLAGS)
			"* 3 FETCH (FLAGS (\\Recent))",
			"FL8 OK FETCH completed",
			// F9 FETCH 3 (Body)
			`* 3 FETCH (INTERNALDATE "01-Jan-2023 10:10:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)] {88}`,
			"From: anotheruser@example.com",
			"To: app@example.com",
			"Subject: Re: New Submission",
			"",
			"RFC822.TEXT {121}",
			"Another reply",
			")",
			"F9 OK FETCH completed",
			// ST10 STORE 3
			"ST10 OK STORE completed",
		}

		mockProto.ReadLineFunc = func() (string, error) {
			mu.Lock()
			defer mu.Unlock()
			if lineIdx >= len(lines) {
				return "", fmt.Errorf("unexpected ReadLine call, index: %d", lineIdx)
			}
			line := lines[lineIdx]
			lineIdx++
			return line, nil
		}

		messages, err := api.GetAllEmailsChainByUuid(mockProto, "test-uuid", cfg)
		assert.NoError(t, err)
		assert.Len(t, messages, 3)

		// Check message 1 (from app)
		assert.Equal(t, "outerUser", messages[0].Sender)
		assert.Equal(t, "msg1", messages[0].Content)

		// Check message 2 (reply from user via app's email)
		assert.Equal(t, "innerUser", messages[1].Sender)
		assert.Equal(t, "This is a reply", messages[1].Content)

		// Check message 3 (standard reply from different user email)
		assert.Equal(t, "innerUser", messages[2].Sender)
		assert.Equal(t, "Another reply", messages[2].Content)
	})
}
