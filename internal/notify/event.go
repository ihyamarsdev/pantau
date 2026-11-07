package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pantau/internal/logger"
)

type eventChannel struct {
	name string
	send func(source, summary, detail string) error
}

// SendEvent delivers a generic (non host-alert) event to every configured channel without blocking.
// It is a no-op when no channel is configured.
func (d *Dispatcher) SendEvent(source, summary, detail string) {
	for _, ch := range d.eventChannels() {
		go func() {
			if err := ch.send(source, summary, detail); err != nil {
				logger.Warn("notify", source, "Failed to send "+ch.name+" event", "error", err.Error())
			}
		}()
	}
}

// SendEventSync delivers synchronously and reports per-channel results (used by the test endpoint).
func (d *Dispatcher) SendEventSync(source, summary, detail string) []ChannelResult {
	var res []ChannelResult
	for _, ch := range d.eventChannels() {
		r := ChannelResult{Channel: ch.name, OK: true}
		if err := ch.send(source, summary, detail); err != nil {
			r.OK, r.Error = false, err.Error()
		}
		res = append(res, r)
	}
	return res
}

func eventText(source, summary, detail string) string {
	s := summary + "\n" + source
	if ex := trimExcerpt(detail, 1500); ex != "" {
		s += "\n\n" + ex
	}
	return s
}

func (d *Dispatcher) eventChannels() []eventChannel {
	if d == nil || d.db == nil {
		return nil
	}
	get := func(k string) string { v, _ := d.db.GetSetting(k); return v }
	var out []eventChannel
	if tok, chat := get("telegram_token"), get("telegram_chat_id"); tok != "" && chat != "" {
		out = append(out, eventChannel{"telegram", func(source, summary, detail string) error {
			text := "<b>" + html.EscapeString(summary) + "</b>\n" + html.EscapeString(source)
			if ex := trimExcerpt(detail, 1500); ex != "" {
				text += "\n\n<pre>" + html.EscapeString(ex) + "</pre>"
			}
			return d.postJSON("https://api.telegram.org/bot"+tok+"/sendMessage", map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML"})
		}})
	}
	if u := get("webhook_url"); u != "" {
		out = append(out, eventChannel{"webhook", func(source, summary, detail string) error {
			return d.postJSON(u, map[string]any{
				"event": "terraform_event", "timestamp": time.Now().UTC().Format(time.RFC3339),
				"source": source, "summary": summary, "detail": detail,
			})
		}})
	}
	if u := get("ntfy_url"); u != "" {
		token := strings.TrimSpace(get("ntfy_token"))
		out = append(out, eventChannel{"ntfy", func(source, summary, detail string) error {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u, strings.NewReader(eventText(source, summary, detail)))
			if err != nil {
				return err
			}
			req.Header.Set("Title", "Pantau: "+summary)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			return d.do(req)
		}})
	}
	if u := get("slack_webhook_url"); u != "" {
		out = append(out, eventChannel{"slack", func(source, summary, detail string) error {
			return d.postJSON(u, map[string]string{"text": "*" + summary + "*\n" + source + codeBlock(detail)})
		}})
	}
	if u := get("discord_webhook_url"); u != "" {
		out = append(out, eventChannel{"discord", func(source, summary, detail string) error {
			c := "**" + summary + "**\n" + source + codeBlock(detail)
			if len(c) > 1800 {
				c = c[:1780] + "\n...[truncated]"
			}
			return d.postJSON(u, map[string]string{"content": c})
		}})
	}
	if host, to := get("smtp_host"), get("smtp_to"); host != "" && to != "" {
		cfg := SMTPConfig{Host: host, Port: NormalizeSMTPPort(get("smtp_port")), User: get("smtp_user"), Pass: get("smtp_pass"),
			From: get("smtp_from"), To: ParseSMTPTo(to), Security: get("smtp_security")}
		out = append(out, eventChannel{"smtp", func(source, summary, detail string) error {
			sender := d.smtpSender
			if sender == nil {
				sender = &defaultSMTPSender{}
			}
			return sender.Send(cfg, fmt.Sprintf("[Pantau] %s: %s", source, summary), strings.ReplaceAll(eventText(source, summary, detail), "\n", "\r\n"))
		}})
	}
	return out
}

func codeBlock(detail string) string {
	if ex := trimExcerpt(detail, 1500); ex != "" {
		return "\n```\n" + ex + "\n```"
	}
	return ""
}

func (d *Dispatcher) postJSON(u string, payload any) error {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return sanitizeURLErr(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return d.do(req)
}

func (d *Dispatcher) do(req *http.Request) error {
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return sanitizeURLErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// sanitizeURLErr drops the request URL from transport errors: bot tokens and webhook secrets live in it.
func sanitizeURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
