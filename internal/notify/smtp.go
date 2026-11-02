package notify

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPSender represents an interface for sending emails, allowing unit test mocks.
type SMTPSender interface {
	Send(cfg SMTPConfig, subject, body string) error
}

type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Pass     string
	From     string
	To       []string
	Security string // "starttls" | "tls" | "none"
}

type defaultSMTPSender struct{}

func (s *defaultSMTPSender) Send(cfg SMTPConfig, subject, body string) error {
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	from := cfg.From
	if from == "" {
		from = cfg.User
	}
	if from == "" {
		return fmt.Errorf("smtp from address is required")
	}
	if len(cfg.To) == 0 {
		return fmt.Errorf("smtp to address is required")
	}

	header := make(map[string]string)
	header["From"] = from
	header["To"] = strings.Join(cfg.To, ", ")
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/plain; charset=utf-8"

	var msg strings.Builder
	for k, v := range header {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	sec := strings.ToLower(strings.TrimSpace(cfg.Security))
	if sec == "" {
		sec = "starttls"
	}

	var auth smtp.Auth
	if cfg.User != "" && cfg.Pass != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}

	connTimeout := 10 * time.Second

	switch sec {
	case "tls":
		tlsConfig := &tls.Config{
			ServerName: cfg.Host,
		}
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: connTimeout}, "tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("tls dial: %w", err)
		}
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
		defer c.Quit()

		if auth != nil {
			if ok, _ := c.Extension("AUTH"); ok {
				if err := c.Auth(auth); err != nil {
					return fmt.Errorf("auth: %w", err)
				}
			}
		}

		if err := c.Mail(from); err != nil {
			return fmt.Errorf("mail: %w", err)
		}
		for _, to := range cfg.To {
			if err := c.Rcpt(to); err != nil {
				return fmt.Errorf("rcpt %s: %w", to, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("data: %w", err)
		}
		if _, err := w.Write([]byte(msg.String())); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		return w.Close()

	case "none":
		conn, err := net.DialTimeout("tcp", addr, connTimeout)
		if err != nil {
			return fmt.Errorf("dial: %w", err)
		}
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
		defer c.Quit()

		if auth != nil {
			if ok, _ := c.Extension("AUTH"); ok {
				if err := c.Auth(auth); err != nil {
					return fmt.Errorf("auth: %w", err)
				}
			}
		}

		if err := c.Mail(from); err != nil {
			return fmt.Errorf("mail: %w", err)
		}
		for _, to := range cfg.To {
			if err := c.Rcpt(to); err != nil {
				return fmt.Errorf("rcpt %s: %w", to, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("data: %w", err)
		}
		if _, err := w.Write([]byte(msg.String())); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		return w.Close()

	case "starttls":
		fallthrough
	default:
		conn, err := net.DialTimeout("tcp", addr, connTimeout)
		if err != nil {
			return fmt.Errorf("dial: %w", err)
		}
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
		defer c.Quit()

		if ok, _ := c.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{
				ServerName: cfg.Host,
			}
			if err := c.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}

		if auth != nil {
			if ok, _ := c.Extension("AUTH"); ok {
				if err := c.Auth(auth); err != nil {
					return fmt.Errorf("auth: %w", err)
				}
			}
		}

		if err := c.Mail(from); err != nil {
			return fmt.Errorf("mail: %w", err)
		}
		for _, to := range cfg.To {
			if err := c.Rcpt(to); err != nil {
				return fmt.Errorf("rcpt %s: %w", to, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("data: %w", err)
		}
		if _, err := w.Write([]byte(msg.String())); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		return w.Close()
	}
}

// ParseSMTPTo parses comma-separated email addresses, trimming whitespace.
func ParseSMTPTo(s string) []string {
	parts := strings.Split(s, ",")
	var res []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}

// NormalizeSMTPPort returns "587" if port is empty or invalid.
func NormalizeSMTPPort(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return "587"
	}
	if p, err := strconv.Atoi(port); err == nil && p > 0 && p <= 65535 {
		return strconv.Itoa(p)
	}
	return "587"
}
