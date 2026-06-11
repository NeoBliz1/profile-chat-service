package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/textproto"
	"regexp"
	"sort"
	"strings"
	"time"
)

// GetImapConnection is a package-level variable that can be overridden for testing.
// It returns a TextprotoCommander which can be a *textproto.Conn or a mock.
var GetImapConnection = defaultGetImapConnection

// defaultGetImapConnection establishes a real IMAP connection.
func defaultGetImapConnection(cfg *Config) (TextprotoCommander, error) {
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

	if _, err := SendImapCommand(proto, "A1", fmt.Sprintf(`LOGIN "%s" "%s"`, cfg.MailEmail, cfg.MailAppPassword)); err != nil {
		proto.Close()
		return nil, fmt.Errorf("auth error: %w", err)
	}

	return proto, nil
}

// CheckReplyHandler manages incoming history synchronization validation queries via IMAP
func CheckReplyHandler(w http.ResponseWriter, r *http.Request, cfg *Config) {
	// Frontend polling script strictly leverages HTTP GET parameters
	if r.Method != http.MethodGet {
		WriteErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 2. Parse and validate the incoming user identification token string
	uuid := strings.TrimSpace(r.URL.Query().Get("uuid"))
	if uuid == "" {
		WriteErrorResponse(w, http.StatusBadRequest, "Missing required query parameter: uuid")
		return
	}

	// 3. Query live mail folders instantly without any tracking constraints or filters!
	// Establish IMAP connection
	proto, err := GetImapConnection(cfg)
	if err != nil {
		WriteErrorResponse(w, http.StatusInternalServerError, "Failed to establish IMAP connection: "+err.Error())
		return
	}
	defer func() {
		// Ensure logout and close are called on the concrete type if it's a *textproto.Conn
		_, _ = SendImapCommand(proto, "A99", `LOGOUT`)
		if conn, ok := proto.(*textproto.Conn); ok {
			conn.Close()
		}
	}()

	fullChain, err := GetAllEmailsChainByUuid(proto, uuid, cfg)
	if err != nil {
		WriteErrorResponse(w, http.StatusInternalServerError, "Mailbox extraction transaction failed: "+err.Error())
		return
	}

	// 4. Always populate the payload wrapper with the complete conversational history
	var responsePayload ChatHistoryResponse
	responsePayload.History = fullChain

	// 5. Deliver finalized response stream matching JSON data payload architectures
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(responsePayload)
}

// FindEmailByUuid Check for single existence footprint (Used by MsgProxyResender to bypass captcha)
func FindEmailByUuid(proto TextprotoCommander, uuid string) (bool, error) {
	if _, err := SendImapCommand(proto, "A2", `SELECT INBOX`); err != nil {
		return false, fmt.Errorf("folder target error: %w", err)
	}

	// Let the helper collect the search response lines
	lines, err := SendImapCommand(proto, "A3", fmt.Sprintf(`SEARCH SUBJECT "%s"`, uuid))
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

// GetAllEmailsChainByUuid Scrape email contents and maps them straight to ChatMessage layout slices
func GetAllEmailsChainByUuid(proto TextprotoCommander, uuid string, cfg *Config) ([]ChatMessage, error) {
	messages := make([]ChatMessage, 0)
	txCounter := 0

	dateRegex := regexp.MustCompile(`(?i)INTERNALDATE\s+"([^"]+)"`)
	fromRegex := regexp.MustCompile(`(?i)From:\s*(.+)`)
	toRegex := regexp.MustCompile(`(?i)To:\s*(.+)`)
	subjectRegex := regexp.MustCompile(`(?i)Subject:\s*(.+)`)
	rfcTextRegex := regexp.MustCompile(`RFC822.TEXT\s*\{[0-9]+\}`)
	flagsRegex := regexp.MustCompile(`(?i)FLAGS \(([^)]+)\)`)

	// 1. Shift context focus onto the designated folder channel
	selectTag := fmt.Sprintf("S%d", txCounter)
	if _, err := SendImapCommand(proto, selectTag, `SELECT "INBOX"`); err != nil {
		return nil, fmt.Errorf("failed to select INBOX: %w", err)
	}
	txCounter++

	// 2. Scan folder headers for matching tracking token parameters
	searchTag := fmt.Sprintf("H%d", txCounter)
	searchLines, err := SendImapCommand(proto, searchTag, fmt.Sprintf(`SEARCH SUBJECT "%s"`, uuid))
	if err != nil {
		return nil, fmt.Errorf("failed to search for emails: %w", err)
	}
	txCounter++

	var folderMessageIDs []string
	for _, line := range searchLines {
		if strings.HasPrefix(line, "* SEARCH ") {
			rawIDs := strings.TrimSpace(strings.TrimPrefix(line, "* SEARCH "))
			if rawIDs != "" {
				folderMessageIDs = strings.Fields(rawIDs)
			}
		}
	}

	// 3. Loop and pull multi-line properties for the resulting local keys
	for _, msgID := range folderMessageIDs {
		// First, check the current flags to see if the message is already read
		flagsTag := fmt.Sprintf("FL%d", txCounter)
		flagsLines, err := SendImapCommand(proto, flagsTag, fmt.Sprintf("FETCH %s (FLAGS)", msgID))
		if err != nil {
			txCounter++
			continue
		}
		txCounter++

		var wasSeen bool
		for _, line := range flagsLines {
			if matches := flagsRegex.FindStringSubmatch(line); len(matches) > 1 {
				if strings.Contains(strings.ToUpper(matches[1]), "\\SEEN") {
					wasSeen = true
				}
			}
		}

		// Now, fetch the full message body
		fetchTag := fmt.Sprintf("F%d", txCounter)
		bodyLines, err := SendImapCommand(proto, fetchTag, fmt.Sprintf(`FETCH %s (INTERNALDATE BODY.PEEK[HEADER.FIELDS (FROM TO SUBJECT)] RFC822.TEXT)`, msgID))
		if err != nil {
			txCounter++
			continue
		}
		txCounter++

		var cleanTextLines []string
		var parsedTime = time.Now()
		var fromHeader, toHeader, subjectHeader string
		isReadingBody := false
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
				if fromMatches := fromRegex.FindStringSubmatch(line); len(fromMatches) > 1 {
					fromHeader = fromMatches[1]
				} else if toMatches := toRegex.FindStringSubmatch(line); len(toMatches) > 1 {
					toHeader = toMatches[1]
				} else if subjectMatches := subjectRegex.FindStringSubmatch(line); len(subjectMatches) > 1 {
					subjectHeader = subjectMatches[1]
				}

				if line == "" {
					isReadingHeader = false
					isReadingBody = true
					continue
				}
			}

			if line == ")" && isReadingBody {
				isReadingBody = false
				continue
			}
			if isReadingBody {
				if rfcTextRegex.MatchString(line) {
					loc := rfcTextRegex.FindStringIndex(line)
					contentOnLine := line[loc[1]:]
					if strings.TrimSpace(contentOnLine) != "" {
						cleanTextLines = append(cleanTextLines, contentOnLine)
					}
					continue
				}
				if strings.Contains(line, " FLAGS (") {
					parts := strings.SplitN(line, " FLAGS (", 2)
					line = parts[0]
				}
				cleanTextLines = append(cleanTextLines, line)
			}
		}

		// If the message was originally unread, restore its unread status
		if !wasSeen {
			storeTag := fmt.Sprintf("ST%d", txCounter)
			_, err := SendImapCommand(proto, storeTag, fmt.Sprintf("STORE %s -FLAGS.SILENT (\\Seen)", msgID))
			if err != nil {
				log.Printf("Warning: failed to restore unread flag for message %s: %v", msgID, err)
			}
			txCounter++
		}

		fullTextBody := strings.Join(cleanTextLines, "\n")
		var finalContent = strings.TrimSpace(fullTextBody)

		if strings.Contains(fullTextBody, "<b>To:</b>") {
			parts := strings.SplitN(fullTextBody, "<b>To:</b>", 2)
			finalContent = cleanReply(parts[0])
		} else if strings.Contains(fullTextBody, "<blockquote") {
			parts := strings.SplitN(fullTextBody, "<blockquote", 2)
			finalContent = cleanReply(parts[0])
		} else if strings.Contains(fullTextBody, "Message:") {
			lines := strings.Split(fullTextBody, "\n")
			var parts []string
			capture := false
			for _, l := range lines {
				lTrim := strings.TrimSpace(l)
				if strings.HasPrefix(lTrim, "Message:") {
					capture = true
					continue
				}
				if capture && lTrim != "" {
					parts = append(parts, lTrim)
				}
			}
			if len(parts) > 0 {
				finalContent = strings.Join(parts, " ")
			}
		}

		sender := "innerUser" // Default to innerUser

		fromAddr, fromErr := mail.ParseAddress(fromHeader)
		toAddr, toErr := mail.ParseAddress(toHeader)

		isFromApp := fromErr == nil && strings.EqualFold(fromAddr.Address, cfg.MailEmail)
		isToApp := toErr == nil && strings.EqualFold(toAddr.Address, cfg.MailEmail)
		isReplySubject := strings.HasPrefix(strings.ToLower(subjectHeader), "re:")

		if isFromApp && isToApp {
			// Both From and To are the app's email. Differentiate by subject.
			if isReplySubject {
				sender = "innerUser" // It's a reply from the user
			} else {
				sender = "outerUser" // It's an initial message from the app
			}
		} else if !isFromApp && isToApp {
			// From is different, To is app's email. This is a standard reply from a user.
			sender = "innerUser"
		}
		// If neither of the above, default to innerUser (e.g., if To is not the app's email, or parsing errors)

		messages = append(messages, ChatMessage{
			Sender:    sender,
			Content:   finalContent,
			Timestamp: parsedTime,
		})
	}

	// 4. Chronological Sorting Block: Arranges cross-folder messages seamlessly by true date
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}
