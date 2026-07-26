package notify

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

type EmailNotifier struct {
	cfg types.EmailConfig
}

func NewEmailNotifier(cfg types.EmailConfig) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

func (n *EmailNotifier) Type() types.ChannelType {
	return types.ChannelEmail
}

func (n *EmailNotifier) Send(ctx context.Context, event types.NotificationEvent) error {
	subject, body := formatEmail(event)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		n.cfg.From, n.cfg.To, subject, body)

	addr := fmt.Sprintf("%s:%d", n.cfg.SMTPServer, n.cfg.SMTPPort)

	var auth smtp.Auth
	if n.cfg.Username != "" {
		auth = smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.SMTPServer)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return smtp.SendMail(addr, auth, n.cfg.From, []string{n.cfg.To}, []byte(msg))
}

func formatEmail(event types.NotificationEvent) (subject, body string) {
	ts := event.Timestamp.Format(time.RFC3339)
	prefix := ""
	if event.AppName != "" {
		prefix = fmt.Sprintf("[%s] ", event.AppName)
	}
	subject = fmt.Sprintf("%s%s", prefix, event.Message)
	if len(subject) > 78 {
		subject = subject[:75] + "..."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Tengiz Notification\n"))
	b.WriteString(fmt.Sprintf("Time: %s\n", ts))
	b.WriteString(fmt.Sprintf("Event: %s\n", event.Type))
	if event.AppName != "" {
		b.WriteString(fmt.Sprintf("App: %s\n", event.AppName))
	}
	b.WriteString(fmt.Sprintf("\n%s\n", event.Message))
	if len(event.Metadata) > 0 {
		b.WriteString("\nMetadata:\n")
		for k, v := range event.Metadata {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	return subject, b.String()
}
