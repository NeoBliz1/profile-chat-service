package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"profile-chat-service/pkg"
	"profile-chat-service/tests"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelectImapInbox(t *testing.T) {
	mockProto := &tests.MockTextprotoCommander{}
	lines := []string{
		"S0 OK SELECT completed",
	}
	var lineIdx int
	mockProto.ReadLineFunc = func() (string, error) {
		if lineIdx >= len(lines) {
			return "", fmt.Errorf("EOF")
		}
		line := lines[lineIdx]
		lineIdx++
		return line, nil
	}
	txCounter := 0

	err := selectImapInbox(mockProto, &txCounter)

	assert.NoError(t, err)
	assert.Equal(t, 1, txCounter)
	assert.Contains(t, mockProto.Commands[0], `SELECT "INBOX"`)
}

func TestSearchEmailsByUuid(t *testing.T) {
	mockProto := &tests.MockTextprotoCommander{}
	lines := []string{
		"* SEARCH 1 2 3",
		"H0 OK SEARCH completed",
	}
	var lineIdx int
	mockProto.ReadLineFunc = func() (string, error) {
		if lineIdx >= len(lines) {
			return "", fmt.Errorf("EOF")
		}
		line := lines[lineIdx]
		lineIdx++
		return line, nil
	}
	txCounter := 0
	uuid := "test-uuid"

	ids, err := searchEmailsByUuid(mockProto, uuid, &txCounter)

	assert.NoError(t, err)
	assert.Equal(t, 1, txCounter)
	assert.Equal(t, []string{"1", "2", "3"}, ids)
	assert.Contains(t, mockProto.Commands[0], fmt.Sprintf(`SEARCH SUBJECT "%s"`, uuid))
}

func TestParseEmailHeaders(t *testing.T) {
	bodyLines := []string{
		`* 1 FETCH (INTERNALDATE "01-Jan-2023 12:00:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)])`,
		"Subject: Test Subject",
		"",
		"Body starts here",
	}
	parsedTime, subject := parseEmailHeaders(bodyLines)

	expectedTime, _ := time.Parse("02-Jan-2006 15:04:05 -0700", "01-Jan-2023 12:00:00 +0000")
	assert.Equal(t, expectedTime, parsedTime)
	assert.Equal(t, "Test Subject", subject)
}

func TestExtractEmailBody(t *testing.T) {
	bodyLines := []string{
		`* 1 FETCH (INTERNALDATE "..." BODY[...])`,
		"Subject: Test",
		"",
		"RFC822.TEXT {123}",
		"This is the email body.",
		"Another line.",
		")",
	}
	body := extractEmailBody(bodyLines)
	assert.Equal(t, "This is the email body.\nAnother line.", strings.TrimSpace(body))
}

func TestCleanMessageContent(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"HTML reply", "Some content <b>To:</b> quoted part", "Some content"},
		{"Blockquote reply", "Some content <blockquote class='...>quoted part", "Some content"},
		{"Message reply", "-----\nMessage: Quoted part", "Quoted part"},
		{"Normal content", "Just a simple message.", "Just a simple message."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleaned := cleanMessageContent(tc.input)
			assert.Equal(t, strings.TrimSpace(tc.expected), strings.TrimSpace(cleaned))
		})
	}
}

func TestDetermineSender(t *testing.T) {
	assert.Equal(t, "innerUser", determineSender("re: Hello"))
	assert.Equal(t, "outerUser", determineSender("Hello"))
}

func TestFetchAndProcessMessage(t *testing.T) {
	mockProto := &tests.MockTextprotoCommander{}
	lines := []string{
		// First call: FETCH FLAGS
		`* 1 FETCH (FLAGS (\Seen))`,
		"FL0 OK FETCH completed",
		// Second call: FETCH BODY
		`* 1 FETCH (INTERNALDATE "02-Jan-2023 10:00:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)] RFC822.TEXT)`,
		"Subject: Re: Test",
		"",
		"RFC822.TEXT {123}",
		"This is a reply.",
		")",
		"F1 OK FETCH completed",
	}
	var lineIdx int
	var mu sync.Mutex
	mockProto.ReadLineFunc = func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if lineIdx >= len(lines) {
			return "", fmt.Errorf("EOF")
		}
		line := lines[lineIdx]
		lineIdx++
		return line, nil
	}

	txCounter := 0
	msgID := "1"

	chatMessage, err := fetchAndProcessMessage(mockProto, msgID, &txCounter)

	assert.NoError(t, err)
	assert.Equal(t, 2, txCounter) // FL0, F1
	assert.Equal(t, "innerUser", chatMessage.Sender)
	assert.Equal(t, "This is a reply.", chatMessage.Content)
	expectedTime, _ := time.Parse("02-Jan-2006 15:04:05 -0700", "02-Jan-2023 10:00:00 +0000")
	assert.Equal(t, expectedTime, chatMessage.Timestamp)
	assert.Equal(t, 2, len(mockProto.Commands))
	assert.Contains(t, mockProto.Commands[0], "FETCH 1 (FLAGS)")
	assert.Contains(t, mockProto.Commands[1], "FETCH 1 (INTERNALDATE")
}

func TestGetAllEmailsChainByUuid(t *testing.T) {
	mockProto := &tests.MockTextprotoCommander{}
	lines := []string{
		// S0 SELECT
		"S0 OK SELECT completed",
		// H1 SEARCH
		"* SEARCH 1",
		"H1 OK SEARCH completed",
		// FL2 FETCH (for msg 1)
		`* 1 FETCH (FLAGS (\Unseen))`,
		"FL2 OK FETCH completed",
		// F3 FETCH (for msg 1)
		`* 1 FETCH (INTERNALDATE "03-Jan-2023 11:00:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)] RFC822.TEXT)`,
		"Subject: Initial Message",
		"",
		"RFC822.TEXT {123}",
		"Hello from app.",
		")",
		"F3 OK FETCH completed",
		// ST4 STORE (for msg 1)
		"ST4 OK STORE completed",
	}
	var lineIdx int
	var mu sync.Mutex
	mockProto.ReadLineFunc = func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if lineIdx >= len(lines) {
			return "", fmt.Errorf("EOF")
		}
		line := lines[lineIdx]
		lineIdx++
		return line, nil
	}

	uuid := "test-uuid"

	messages, err := GetAllEmailsChainByUuid(mockProto, uuid)

	assert.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, "outerUser", messages[0].Sender)
	assert.Equal(t, "Hello from app.", messages[0].Content)
	assert.Equal(t, 5, len(mockProto.Commands)) // SELECT, SEARCH, FETCH FLAGS, FETCH BODY, STORE
	assert.Contains(t, mockProto.Commands[4], "STORE 1 -FLAGS.SILENT (\\Seen)")
}

func TestCheckReplyHandler(t *testing.T) {
	originalGetImapConnection := GetImapConnection
	defer func() { GetImapConnection = originalGetImapConnection }()

	mockProto := &tests.MockTextprotoCommander{}
	lines := []string{
		// S0 SELECT
		"S0 OK SELECT completed",
		// H1 SEARCH
		"* SEARCH 1",
		"H1 OK SEARCH completed",
		// FL2 FETCH
		`* 1 FETCH (FLAGS (\Seen))`,
		"FL2 OK FETCH completed",
		// F3 FETCH
		`* 1 FETCH (INTERNALDATE "04-Jan-2023 12:00:00 +0000" BODY[HEADER.FIELDS (FROM TO SUBJECT)] RFC822.TEXT)`,
		"Subject: Test",
		"",
		"Hello",
		")",
		"F3 OK FETCH completed",
		// A99 LOGOUT
		"A99 OK LOGOUT completed",
	}
	var lineIdx int
	var mu sync.Mutex
	mockProto.ReadLineFunc = func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if lineIdx >= len(lines) {
			return "", fmt.Errorf("EOF")
		}
		line := lines[lineIdx]
		lineIdx++
		return line, nil
	}

	GetImapConnection = func(cfg *pkg.Config) (pkg.TextprotoCommander, error) {
		return mockProto, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/check?uuid=test-uuid", nil)
	rr := httptest.NewRecorder()
	cfg := &pkg.Config{MailEmail: "test", MailAppPassword: "test", IMAPHost: "test"}

	CheckReplyHandler(rr, req, cfg)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"history":[{`)
	assert.Contains(t, mockProto.Commands[len(mockProto.Commands)-1], "LOGOUT")
}
