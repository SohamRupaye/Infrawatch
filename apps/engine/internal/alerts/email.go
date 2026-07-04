package alerts

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
)

// EmailChannel sends alerts via SMTP.
type EmailChannel struct {
	cfg config.EmailConfig
}

// NewEmailChannel creates an EmailChannel. Returns an error if config is incomplete.
func NewEmailChannel(cfg config.EmailConfig) (*EmailChannel, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("email: smtp_host is required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("email: from is required")
	}
	if len(cfg.Recipients) == 0 {
		return nil, fmt.Errorf("email: at least one recipient required")
	}
	return &EmailChannel{cfg: cfg}, nil
}

// Name implements Channel.
func (e *EmailChannel) Name() string { return "email" }

// Send implements Channel. Uses SMTP with STARTTLS.
func (e *EmailChannel) Send(ctx context.Context, alert Alert) error {
	subject := fmt.Sprintf("[Infrawatch] [%s] %s", alert.State, alert.ServiceName)
	body := e.buildBody(alert)

	addr := fmt.Sprintf("%s:%d", e.cfg.SMTPHost, e.cfg.SMTPPort)
	if e.cfg.SMTPPort == 0 {
		addr = fmt.Sprintf("%s:587", e.cfg.SMTPHost)
	}

	var auth smtp.Auth
	if e.cfg.Username != "" {
		auth = smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.SMTPHost)
	}

	// Build raw message
	headers := map[string]string{
		"From":         e.cfg.From,
		"To":           strings.Join(e.cfg.Recipients, ", "),
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/plain; charset=utf-8",
		"Date":         time.Now().Format(time.RFC1123Z),
	}

	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// Use TLS dialer for port 465, STARTTLS otherwise
	if e.cfg.SMTPPort == 465 {
		return e.sendTLS(addr, auth, msg.String())
	}
	return smtp.SendMail(addr, auth, e.cfg.From, e.cfg.Recipients, []byte(msg.String()))
}

func (e *EmailChannel) sendTLS(addr string, auth smtp.Auth, msg string) error {
	tlsCfg := &tls.Config{ServerName: e.cfg.SMTPHost}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, e.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Quit()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(e.cfg.From); err != nil {
		return err
	}
	for _, rcpt := range e.cfg.Recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = w.Write([]byte(msg))
	return err
}

func (e *EmailChannel) buildBody(alert Alert) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Infrawatch Alert\n"))
	sb.WriteString(strings.Repeat("=", 40) + "\n\n")
	sb.WriteString(fmt.Sprintf("Service:        %s\n", alert.ServiceName))
	sb.WriteString(fmt.Sprintf("State:          %s\n", alert.State))
	sb.WriteString(fmt.Sprintf("Previous State: %s\n", alert.PreviousState))
	sb.WriteString(fmt.Sprintf("Response Time:  %dms\n", alert.ResponseTimeMs))
	sb.WriteString(fmt.Sprintf("Time:           %s\n\n", alert.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Message:\n%s\n\n", alert.Message))
	if alert.DashboardURL != "" {
		sb.WriteString(fmt.Sprintf("Dashboard: %s\n", alert.DashboardURL))
	}
	sb.WriteString("\n-- \nSent by Infrawatch\n")
	return sb.String()
}
