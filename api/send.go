package api

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/textproto"
	pkg2 "profile-chat-service/pkg"
	"strings"
	"time"
)

var (
	// RecaptchaAPIURL is the endpoint for Google's reCAPTCHA verification.
	RecaptchaAPIURL = "https://recaptchaenterprise.googleapis.com/v1/projects/%s/assessments?key=%s"
	TlsDial         = tls.Dial
)

func MsgProxyResender(w http.ResponseWriter, r *http.Request, cfg *pkg2.Config) {
	if r.Method != http.MethodPost {
		pkg2.WriteErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var payload pkg2.EmailPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		pkg2.WriteErrorResponse(w, http.StatusBadRequest, "Malformed JSON payload")
		return
	}

	if errStr := pkg2.ValidatePayload(&payload); errStr != "" {
		pkg2.WriteErrorResponse(w, http.StatusUnprocessableEntity, errStr)
		return
	}

	cleanUUID := strings.TrimSpace(payload.Uuid)
	var isUserAlreadyVerified bool

	if cleanUUID != "" {
		proto, err := GetImapConnection(cfg)
		if err != nil {
			pkg2.WriteErrorResponse(w, http.StatusBadGateway, "Failed to establish IMAP connection: "+err.Error())
			return
		}
		defer func() {
			_, _ = pkg2.SendImapCommand(proto, "A99", `LOGOUT`)
			if conn, ok := proto.(*textproto.Conn); ok {
				conn.Close()
			}
		}()

		alreadyEmailed, err := FindEmailByUuid(proto, cleanUUID)
		if err != nil {
			pkg2.WriteErrorResponse(w, http.StatusBadGateway, "IMAP UUID check failed: "+err.Error())
			return
		}
		if alreadyEmailed {
			isUserAlreadyVerified = true
		}
	}

	if isUserAlreadyVerified {
		log.Printf("Bypassing reCAPTCHA: Active session confirmed")
	} else {
		valid, err := VerifyRecaptcha(cfg, payload.RecaptchaResponse)
		if err != nil {
			pkg2.WriteErrorResponse(w, http.StatusForbidden, err.Error())
			return
		} else if !valid {
			pkg2.WriteErrorResponse(w, http.StatusForbidden, "Bot verification failed or token has expired")
			return
		}
	}

	if err := SendSecureEmail(cfg, &payload); err != nil {
		pkg2.WriteErrorResponse(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(pkg2.SuccessResponse{Success: true})
}

// VerifyRecaptcha creates an Enterprise assessment using Google Cloud platform project tokens
func VerifyRecaptcha(cfg *pkg2.Config, token string) (bool, error) {
	if cfg.GCPProjectID == "" || cfg.GCPAPIKey == "" || cfg.GCPSiteKey == "" {
		return false, fmt.Errorf("missing critical Google Cloud environment configurations")
	}

	apiURL := fmt.Sprintf(RecaptchaAPIURL, cfg.GCPProjectID, cfg.GCPAPIKey)

	var reqBody pkg2.AssessmentRequest
	reqBody.Event.Token = token
	reqBody.Event.SiteKey = cfg.GCPSiteKey

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("failed to marshal assessment JSON payload: %w", err)
	}

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return false, fmt.Errorf("network error communicating with Google API gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("google cloud assessment endpoint returned failure status: %d", resp.StatusCode)
	}

	var googleResult pkg2.AssessmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&googleResult); err != nil {
		return false, fmt.Errorf("failed to decode Google JSON assessment response payload: %w", err)
	}

	return googleResult.TokenProperties.Valid, nil
}

// SendSecureEmail manages lower-level SMTP socket routing networks over strict implicit TLS
func SendSecureEmail(cfg *pkg2.Config, p *pkg2.EmailPayload) error {
	if cfg.MailEmail == "" || cfg.MailAppPassword == "" || cfg.SMTPHost == "" || cfg.SMTPPort == "" {
		return fmt.Errorf("server configuration missing backend variables")
	}

	// 1. Build your dynamic string with Cyrillic characters
	rawSubject := fmt.Sprintf("New Profile Site Submission from %s - Session UUID: %s", p.Name, p.Uuid)

	// 2. Encode the subject using RFC 2047 standard for UTF-8 Base64
	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(rawSubject)))

	// 3. Assemble headers using the encoded subject line
	fromHeader := fmt.Sprintf("From: %s\r\n", cfg.MailEmail)
	toHeader := fmt.Sprintf("To: %s\r\n", cfg.MailEmail)
	subjectHeader := fmt.Sprintf("Subject: %s\r\n", encodedSubject) // 👈 Use encoded string here
	replyToHeader := fmt.Sprintf("Reply-To: %s\r\n", cfg.MailEmail)

	// 4. Declare Content-Type for the body text to support Russian characters inside the message body
	contentTypeHeader := "Content-Type: text/plain; charset=UTF-8\r\n"

	body := fmt.Sprintf("\r\nMessage:\r\n%s", p.Message)

	msg := []byte(fromHeader + toHeader + subjectHeader + replyToHeader + contentTypeHeader + body)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         cfg.SMTPHost,
	}

	conn, err := TlsDial("tcp", fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort), tlsConfig)
	if err != nil {
		return fmt.Errorf("failed direct TLS dial connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Warning: failed to close TLS socket connection: %v", closeErr)
		}
	}()

	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("failed to initialize secure SMTP client: %w", err)
	}
	defer func() {
		if quitErr := client.Quit(); quitErr != nil {
			log.Printf("Warning: failed to close SMTP client transaction cleanly: %v", quitErr)
		}
	}()

	auth := smtp.PlainAuth("", cfg.MailEmail, cfg.MailAppPassword, cfg.SMTPHost)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication rejected inside TLS: %w", err)
	}

	if err = client.Mail(cfg.MailEmail); err != nil {
		return fmt.Errorf("SMTP MAIL transaction failed: %w", err)
	}
	if err = client.Rcpt(cfg.MailEmail); err != nil {
		return fmt.Errorf("SMTP RCPT transaction failed: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA transaction opening failed: %w", err)
	}

	if _, err = writer.Write(msg); err != nil {
		return fmt.Errorf("SMTP data writing failed: %w", err)
	}

	if err = writer.Close(); err != nil {
		return fmt.Errorf("SMTP transaction close confirmation failed: %w", err)
	}

	return nil
}
