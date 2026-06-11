package pkg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func WriteErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}

// SetupCORS configures access guidelines
func SetupCORS(w http.ResponseWriter, cfg *Config) error {
	if cfg.OriginCORS == "" {
		return fmt.Errorf("server configuration missing backend variables")
	}
	w.Header().Set("Access-Control-Allow-Origin", cfg.OriginCORS)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	return nil
}

// ValidatePayload scrubs text strings and confirms input parameters meet format rules
func ValidatePayload(p *EmailPayload) string {
	p.Name = SanitizeInput(p.Name)
	p.Message = SanitizeInput(p.Message)

	if p.Name == "" || p.Message == "" {
		return "Name and message fields cannot be empty"
	}
	if strings.TrimSpace(p.RecaptchaResponse) == "" && p.Uuid == "" {
		return "Missing required reCAPTCHA or Uuid token verification parameter"
	}
	return ""
}

var htmlSanitizer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#x27;",
	"/", "&#x2F;",
)

func SanitizeInput(input string) string {
	// 1. Trim empty spaces
	cleaned := strings.TrimSpace(input)
	// 2. Convert characters like < and > into harmless text strings (&lt; and &gt;)
	return htmlSanitizer.Replace(cleaned)
}

// SendImapCommand is a universal low-level sync helper (Upgraded to capture multi-line data streams)
func SendImapCommand(proto TextprotoCommander, tag, command string) ([]string, error) {
	id, err := proto.Cmd("%s %s", tag, command)
	if err != nil {
		return nil, err
	}
	proto.StartResponse(id)
	defer proto.EndResponse(id)

	var dataStreamLines []string

	for {
		line, err := proto.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("network connection severed: %w", err)
		}

		// 1. Capture untagged server data lines (e.g., * SEARCH 12 15 or * 1 FETCH...)
		if strings.HasPrefix(line, "* ") {
			dataStreamLines = append(dataStreamLines, line)
			continue
		}

		// 2. Success boundary: Command completed cleanly
		if strings.HasPrefix(line, tag+" OK") {
			return dataStreamLines, nil
		}

		// 3. Error boundary: Command rejected by email provider
		if strings.HasPrefix(line, tag+" NO") || strings.HasPrefix(line, tag+" BAD") {
			return nil, fmt.Errorf("IMAP failure reply: %s", line)
		}

		// 4. Capture raw text bodies (lines inside multi-line text blocks that lack prefixes)
		dataStreamLines = append(dataStreamLines, line)
	}
}

func CleanReply(text string) string {
	// Replace various forms of line breaks and special spaces with a single space
	cleaned := strings.ReplaceAll(text, "<br />", " ")
	cleaned = strings.ReplaceAll(cleaned, "<br>", " ")
	cleaned = strings.ReplaceAll(cleaned, "\u200a", " ") // Hair space
	return strings.TrimSpace(cleaned)
}
