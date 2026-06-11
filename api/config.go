package api

import "github.com/kelseyhightower/envconfig"

// Config holds all the configuration for the application.
type Config struct {
	OriginCORS      string `envconfig:"ORIGIN_CORS"`
	GCPProjectID    string `envconfig:"GCP_PROJECT_ID"`
	GCPAPIKey       string `envconfig:"GCP_API_KEY"`
	GCPSiteKey      string `envconfig:"GCP_SITE_KEY"`
	MailEmail       string `envconfig:"MAIL_EMAIL"`
	MailAppPassword string `envconfig:"MAIL_APP_PASSWORD"`
	SMTPHost        string `envconfig:"SMTP_HOST"`
	SMTPPort        string `envconfig:"SMTP_PORT"`
	IMAPHost        string `envconfig:"IMAP_HOST"`
}

// LoadConfig loads the configuration from environment variables.
func LoadConfig() (*Config, error) {
	var c Config
	err := envconfig.Process("", &c)
	return &c, err
}
