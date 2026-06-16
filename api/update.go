package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/textproto"
	pkg2 "profile-chat-service/pkg"
	"regexp"
	"sort"
	"strings"
	"time"
)

// GetImapConnection is a package-level variable that can be overridden for testing.
// It returns a TextprotoCommander which can be a *textproto.Conn or a mock.
var GetImapConnection = defaultGetImapConnection

// defaultGetImapConnection establishes a real IMAP connection.
func defaultGetImapConnection(cfg *pkg2.Config) (pkg2.TextprotoCommander, error) {
	if cfg.MailEmail == "" || cfg.MailAppPassword == "" || cfg.IMAPHost == "" {
		return nil, fmt.Errorf("missing IMAP login configurations")
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: false, ServerName: strings.Split(cfg.IMAPHost, ":")[0]}
	conn, err := tls.Dial("tcp", cfg.IMAPHost, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("network handshake failed: %w", err)
	}

	proto := textproto.NewConn(conn)
	_, _, _ = proto.ReadResponse(-1) // Flush greeting line

	if _, err := pkg2.SendImapCommand(proto, "A1", fmt.Sprintf(`LOGIN "%s" "%s"`, cfg.MailEmail, cfg.MailAppPassword)); err != nil {
		proto.Close()
		return nil, fmt.Errorf("auth error: %w", err)
	}

	return proto, nil
}

// CheckReplyHandler manages incoming history synchronization validation queries via IMAP
func CheckReplyHandler(w http.ResponseWriter, r *http.Request, cfg *pkg2.Config) {
	// Frontend polling script strictly leverages HTTP GET parameters
	if r.Method != http.MethodGet {
		pkg2.WriteErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 2. Parse and validate the incoming user identification token string
	uuid := strings.TrimSpace(r.URL.Query().Get("uuid"))
	if uuid == "" {
		pkg2.WriteErrorResponse(w, http.StatusBadRequest, "Missing required query parameter: uuid")
		return
	}

	// 3. Query live mail folders instantly without any tracking constraints or filters!
	// Establish IMAP connection
	proto, err := GetImapConnection(cfg)
	if err != nil {
		pkg2.WriteErrorResponse(w, http.StatusInternalServerError, "Failed to establish IMAP connection: "+err.Error())
		return
	}
	defer func() {
		// Ensure logout and close are called on the concrete type if it's a *textproto.Conn
		_, _ = pkg2.SendImapCommand(proto, "A99", `LOGOUT`)
		if conn, ok := proto.(*textproto.Conn); ok {
			conn.Close()
		}
	}()

	fullChain, err := GetAllEmailsChainByUuid(proto, uuid)
	if err != nil {
		pkg2.WriteErrorResponse(w, http.StatusInternalServerError, "Mailbox extraction transaction failed: "+err.Error())
		return
	}

	// 4. Always populate the payload wrapper with the complete conversational history
	var responsePayload pkg2.ChatHistoryResponse
	responsePayload.History = fullChain

	// 5. Deliver finalized response stream matching JSON data payload architectures
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(responsePayload)
}

// FindEmailByUuid Check for single existence footprint (Used by MsgProxyResender to bypass captcha)
func FindEmailByUuid(proto pkg2.TextprotoCommander, uuid string) (bool, error) {
	if _, err := pkg2.SendImapCommand(proto, "A2", `SELECT INBOX`); err != nil {
		return false, fmt.Errorf("folder target error: %w", err)
	}

	// Let the helper collect the search response lines
	lines, err := pkg2.SendImapCommand(proto, "A3", fmt.Sprintf(`SEARCH SUBJECT "%s"`, uuid))
	if err != nil {
		return false, err
	}

	// Check if any line contains a search result with numbers
	for _, line := range lines {
		if strings.HasPrefix(line, "* SEARCH ") {
			resultData := strings.TrimPrefix(line, "* SEARCH ")
			if len(strings.TrimSpace(resultData)) > 0 {
				return true, nil // Footprint found!
			}
		}
	}
	return false, nil
}

// selectImapInbox selects the "INBOX" folder.
func selectImapInbox(proto pkg2.TextprotoCommander, txCounter *int) error {
	selectTag := fmt.Sprintf("S%d", *txCounter)
	if _, err := pkg2.SendImapCommand(proto, selectTag, `SELECT "INBOX"`); err != nil {
		return fmt.Errorf("failed to select INBOX: %w", err)
	}
	*txCounter++
	return nil
}

// searchEmailsByUuid searches for emails with a subject matching the UUID and returns their message IDs.
func searchEmailsByUuid(proto pkg2.TextprotoCommander, uuid string, txCounter *int) ([]string, error) {
	searchTag := fmt.Sprintf("H%d", *txCounter)
	searchLines, err := pkg2.SendImapCommand(proto, searchTag, fmt.Sprintf(`SEARCH SUBJECT "%s"`, uuid))
	if err != nil {
		return nil, fmt.Errorf("failed to search for emails: %w", err)
	}
	*txCounter++

	var folderMessageIDs []string
	for _, line := range searchLines {
		if strings.HasPrefix(line, "* SEARCH ") {
			rawIDs := strings.TrimSpace(strings.TrimPrefix(line, "* SEARCH "))
			if rawIDs != "" {
				folderMessageIDs = strings.Fields(rawIDs)
			}
		}
	}
	return folderMessageIDs, nil
}

// parseEmailHeaders extracts the internal date and subject from the fetched email lines.
func parseEmailHeaders(bodyLines []string) (time.Time, string) {
	dateRegex := regexp.MustCompile(`(?i)INTERNALDATE\s+"([^"]+)"`)
	subjectRegex := regexp.MustCompile(`(?i)Subject:\s*(.+)`)

	var parsedTime = time.Now()
	var subjectHeader string
	isReadingHeader := false

	for _, line := range bodyLines {
		if strings.HasPrefix(line, "* ") && strings.Contains(line, "FETCH") {
			if matches := dateRegex.FindStringSubmatch(line); len(matches) > 1 {
				if t, errParse := time.Parse("02-Jan-2006 15:04:05 -0700", matches[1]); errParse == nil {
					parsedTime = t
				}
			}
			isReadingHeader = true
			continue
		}

		if isReadingHeader {
			if subjectMatches := subjectRegex.FindStringSubmatch(line); len(subjectMatches) > 1 {
				rawSub := subjectMatches[1]
				if decodedSub, err := (&mime.WordDecoder{}).DecodeHeader(rawSub); err == nil {
					subjectHeader = decodedSub
				} else {
					subjectHeader = rawSub
				}
			}
			if line == "" { // End of headers
				break
			}
		}
	}
	return parsedTime, subjectHeader
}

// extractEmailBody extracts the raw text body from the fetched email lines.
func extractEmailBody(bodyLines []string) string {
	rfcTextRegex := regexp.MustCompile(`RFC822.TEXT\s*\{[0-9]+\}`)
	flagsRegex := regexp.MustCompile(`(?i)FLAGS \(([^)]+)\)`)

	var cleanTextLines []string
	isReadingBody := false
	isReadingHeader := false // To skip header lines

	for _, line := range bodyLines {
		if strings.HasPrefix(line, "* ") && strings.Contains(line, "FETCH") {
			isReadingHeader = true
			continue
		}
		if isReadingHeader {
			if line == "" { // End of headers, start of body
				isReadingHeader = false
				isReadingBody = true
				continue
			}
			continue // Still in headers
		}

		if isReadingBody {
			if line == ")" { // End of body fetch
				isReadingBody = false
				continue
			}
			if rfcTextRegex.MatchString(line) {
				loc := rfcTextRegex.FindStringIndex(line)
				contentOnLine := line[loc[1]:]
				if strings.TrimSpace(contentOnLine) != "" {
					cleanTextLines = append(cleanTextLines, contentOnLine)
				}
				continue
			}
			if flagsRegex.MatchString(line) { // Remove FLAGS part if present in body line
				parts := strings.SplitN(line, " FLAGS (", 2)
				line = parts[0]
			}
			cleanTextLines = append(cleanTextLines, line)
		}
	}
	return strings.Join(cleanTextLines, "\n")
}

// cleanMessageContent cleans the extracted email content.
func cleanMessageContent(fullTextBody string) string {
	var finalContent = strings.TrimSpace(fullTextBody)

	if strings.Contains(fullTextBody, "<b>To:</b>") {
		parts := strings.SplitN(fullTextBody, "<b>To:</b>", 2)
		finalContent = pkg2.CleanReply(parts[0])
	} else if strings.Contains(fullTextBody, "<blockquote") {
		parts := strings.SplitN(fullTextBody, "<blockquote", 2)
		finalContent = pkg2.CleanReply(parts[0])
	} else if strings.Contains(fullTextBody, "Message:") {
		parts := strings.SplitN(fullTextBody, "Message:", 2)
		if len(parts) > 1 {
			finalContent = strings.TrimSpace(parts[1])
		} else {
			finalContent = pkg2.CleanReply(parts[0])
		}
	}
	return finalContent
}

// determineSender determines the sender based on the subject header.
func determineSender(subjectHeader string) string {
	if strings.HasPrefix(strings.ToLower(subjectHeader), "re:") {
		return "innerUser" // It's a reply from the user
	}
	return "outerUser" // It's an initial message from the app
}

// fetchAndProcessMessage fetches and processes a single email message.
func fetchAndProcessMessage(proto pkg2.TextprotoCommander, msgID string, txCounter *int) (pkg2.ChatMessage, error) {
	flagsRegex := regexp.MustCompile(`(?i)FLAGS \(([^)]+)\)`)

	// First, check the current flags to see if the message is already read
	flagsTag := fmt.Sprintf("FL%d", *txCounter)
	flagsLines, err := pkg2.SendImapCommand(proto, flagsTag, fmt.Sprintf("FETCH %s (FLAGS)", msgID))
	if err != nil {
		*txCounter++
		return pkg2.ChatMessage{}, fmt.Errorf("failed to fetch flags for message %s: %w", msgID, err)
	}
	*txCounter++

	var wasSeen bool
	for _, line := range flagsLines {
		if matches := flagsRegex.FindStringSubmatch(line); len(matches) > 1 {
			if strings.Contains(strings.ToUpper(matches[1]), "\\SEEN") {
				wasSeen = true
			}
		}
	}

	// Now, fetch the full message body
	fetchTag := fmt.Sprintf("F%d", *txCounter)
	bodyLines, err := pkg2.SendImapCommand(proto, fetchTag, fmt.Sprintf(`FETCH %s (INTERNALDATE BODY.PEEK[HEADER.FIELDS (FROM TO SUBJECT)] RFC822.TEXT)`, msgID))
	if err != nil {
		*txCounter++
		return pkg2.ChatMessage{}, fmt.Errorf("failed to fetch body for message %s: %w", msgID, err)
	}
	*txCounter++

	parsedTime, subjectHeader := parseEmailHeaders(bodyLines)
	fullTextBody := extractEmailBody(bodyLines)
	finalContent := cleanMessageContent(fullTextBody)
	sender := determineSender(subjectHeader)

	// If the message was originally unread, restore its unread status
	if !wasSeen {
		storeTag := fmt.Sprintf("ST%d", *txCounter)
		_, err := pkg2.SendImapCommand(proto, storeTag, fmt.Sprintf("STORE %s -FLAGS.SILENT (\\Seen)", msgID))
		if err != nil {
			log.Printf("Warning: failed to restore unread flag for message %s: %v", msgID, err)
		}
		*txCounter++
	}

	return pkg2.ChatMessage{
		Sender:    sender,
		Content:   finalContent,
		Timestamp: parsedTime,
	}, nil
}

// GetAllEmailsChainByUuid Scrape email contents and maps them straight to ChatMessage layout slices
func GetAllEmailsChainByUuid(proto pkg2.TextprotoCommander, uuid string) ([]pkg2.ChatMessage, error) {
	messages := make([]pkg2.ChatMessage, 0)
	txCounter := 0

	// 1. Shift context focus onto the designated folder channel
	if err := selectImapInbox(proto, &txCounter); err != nil {
		return nil, err
	}

	// 2. Scan folder headers for matching tracking token parameters
	folderMessageIDs, err := searchEmailsByUuid(proto, uuid, &txCounter)
	if err != nil {
		return nil, err
	}

	// 3. Loop and pull multi-line properties for the resulting local keys
	for _, msgID := range folderMessageIDs {
		chatMessage, err := fetchAndProcessMessage(proto, msgID, &txCounter)
		if err != nil {
			log.Printf("Error processing message %s: %v", msgID, err)
			continue // Continue to the next message even if one fails
		}
		messages = append(messages, chatMessage)
	}

	// 4. Chronological Sorting Block: Arranges cross-folder messages seamlessly by true date
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}
