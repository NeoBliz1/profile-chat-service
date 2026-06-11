# Profile Chat Service

A lightweight, serverless-first backend service designed to handle contact form submissions and real-time chat functionalities. Built with Go, this service integrates with SMTP for sending emails, IMAP for retrieving message history, and Google reCAPTCHA for spam prevention. It is architected to run seamlessly on Vercel for serverless deployment and includes a traditional server setup for local development.

## Key Features

- **Contact Form Processing**: Accepts POST requests from a contact form and forwards the submission as an email via SMTP.
- **Chat History Retrieval**: Fetches and reconstructs conversation threads by querying an IMAP mailbox, allowing for a persistent chat-like experience.
- **Spam Protection**: Integrates with Google reCAPTCHA v3 to verify that submissions are from legitimate users.
- **Session Bypass**: Active chat sessions (identified by a UUID) can bypass reCAPTCHA for a smoother user experience.
- **Dual Architecture**:
  - **Serverless**: Optimized for Vercel with a `api/index.go` entry point.
  - **Local Development**: Runs as a standard Go web server via `main.go`.
- **Secure & Robust**: Implements secure TLS connections for email protocols and includes a comprehensive testing suite.

## Architecture Overview

The service is composed of two primary API endpoints:

- `/api/send`: Handles incoming contact form submissions. It validates the payload, verifies the user with reCAPTCHA (or a session UUID), and sends the message as an email.
- `/api/check`: Retrieves the chat history for a given session UUID. It connects to an IMAP server, searches for relevant emails, and returns a chronologically sorted conversation thread.

The project follows a standard Go project structure and includes a suite of unit and integration tests that mock external services (SMTP, IMAP, reCAPTCHA) to ensure reliability.

## Local Development

To run the service on your local machine, you need to have Go installed.

1.  **Clone the repository:**
    ```sh
    git clone https://github.com/your-username/profile-chat-service.git
    cd profile-chat-service
    ```

2.  **Set Environment Variables:**
    The service is configured using environment variables. You can create a `.env` file and use a tool like `godotenv` or set them directly in your shell.

    **Required variables:**
    - `ALLOWED_ORIGINS`: The CORS origin to allow requests from (e.g., `http://localhost:3000`).
    - `GCP_PROJECT_ID`: Your Google Cloud Project ID for reCAPTCHA.
    - `GCP_API_KEY`: Your Google Cloud API Key for reCAPTCHA.
    - `GCP_SITE_KEY`: Your reCAPTCHA v3 site key.
    - `MAIL_EMAIL`: The email address to send from and to.
    - `MAIL_APP_PASSWORD`: An app-specific password for your email account.
    - `SMTP_HOST`: The SMTP server hostname (e.g., `smtp.gmail.com`).
    - `SMTP_PORT`: The SMTP server port (e.g., `587`).
    - `IMAP_HOST`: The IMAP server hostname (e.g., `imap.gmail.com:993`).

3.  **Run the server:**
    ```sh
    go run main.go
    ```
    The server will start on `http://localhost:8080`.
