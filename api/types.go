package api

import "time"

// EmailPayload defines the structured contact form payload including captcha data
type EmailPayload struct {
	Name              string `json:"name"`
	Message           string `json:"message"`
	Uuid              string `json:"uuid"`
	RecaptchaResponse string `json:"g-recaptcha-response"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	Success bool `json:"success"`
}

type AssessmentRequest struct {
	Event EventData `json:"event"`
}

type EventData struct {
	Token   string `json:"token"`
	SiteKey string `json:"siteKey"`
}

type AssessmentResponse struct {
	TokenProperties struct {
		Valid  bool   `json:"valid"`
		Action string `json:"action"`
	} `json:"tokenProperties"`
	RiskAnalysis struct {
		Score float64 `json:"score"` // Value between 0.0 (bot) and 1.0 (human)
	} `json:"riskAnalysis"`
}

type ChatMessage struct {
	Sender    string    `json:"sender"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type ChatHistoryResponse struct {
	History []ChatMessage `json:"history"`
}

type FolderConfig struct {
	Name   string
	Sender string
}

// TextprotoCommander defines the interface for textproto.Conn methods used by SendImapCommand.
type TextprotoCommander interface {
	Cmd(format string, args ...interface{}) (uint, error)
	StartResponse(id uint)
	EndResponse(id uint)
	ReadLine() (string, error)
}
