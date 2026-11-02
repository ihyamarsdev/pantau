package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

type ChannelResult struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type TestNotifyResponse struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Results []ChannelResult `json:"results"`
}

type Dispatcher struct {
	db         *store.DB
	httpClient *http.Client
	smtpSender SMTPSender
}

func New(db *store.DB) *Dispatcher {
	return &Dispatcher{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		smtpSender: &defaultSMTPSender{},
	}
}

// SetSMTPSender overrides the SMTP sender for testing.
func (d *Dispatcher) SetSMTPSender(sender SMTPSender) {
	d.smtpSender = sender
}

// SetHTTPClient overrides the HTTP client for testing.
func (d *Dispatcher) SetHTTPClient(client *http.Client) {
	d.httpClient = client
}

func (d *Dispatcher) SendAlert(host *store.Host, rule *store.DesiredRule, summary, rootCause string) {
	tgToken, _ := d.db.GetSetting("telegram_token")
	tgChatID, _ := d.db.GetSetting("telegram_chat_id")
	webhookURL, _ := d.db.GetSetting("webhook_url")
	ntfyURL, _ := d.db.GetSetting("ntfy_url")
	ntfyToken, _ := d.db.GetSetting("ntfy_token")
	slackURL, _ := d.db.GetSetting("slack_webhook_url")
	discordURL, _ := d.db.GetSetting("discord_webhook_url")

	smtpHost, _ := d.db.GetSetting("smtp_host")
	smtpPort, _ := d.db.GetSetting("smtp_port")
	smtpUser, _ := d.db.GetSetting("smtp_user")
	smtpPass, _ := d.db.GetSetting("smtp_pass")
	smtpFrom, _ := d.db.GetSetting("smtp_from")
	smtpTo, _ := d.db.GetSetting("smtp_to")
	smtpSecurity, _ := d.db.GetSetting("smtp_security")

	// 1. Dispatch Telegram
	if tgToken != "" && tgChatID != "" {
		go func() {
			if err := d.sendTelegram(tgToken, tgChatID, host.Name, summary, rootCause); err != nil {
				logger.Warn("notify", host.Name, "Failed to send Telegram alert", "error", err.Error())
			}
		}()
	}

	// 2. Dispatch Webhook
	if webhookURL != "" {
		go func() {
			if err := d.sendWebhook(webhookURL, host, rule, summary, rootCause); err != nil {
				logger.Warn("notify", host.Name, "Failed to send Webhook alert", "error", err.Error())
			}
		}()
	}

	// 3. Dispatch ntfy
	if ntfyURL != "" {
		go func() {
			if err := d.sendNtfy(ntfyURL, ntfyToken, host.Name, summary, rootCause); err != nil {
				logger.Warn("notify", host.Name, "Failed to send ntfy alert", "error", err.Error())
			}
		}()
	}

	// 4. Dispatch Slack
	if slackURL != "" {
		go func() {
			if err := d.sendSlack(slackURL, host.Name, summary, rootCause); err != nil {
				logger.Warn("notify", host.Name, "Failed to send Slack alert", "error", err.Error())
			}
		}()
	}

	// 5. Dispatch Discord
	if discordURL != "" {
		go func() {
			if err := d.sendDiscord(discordURL, host.Name, summary, rootCause); err != nil {
				logger.Warn("notify", host.Name, "Failed to send Discord alert", "error", err.Error())
			}
		}()
	}

	// 6. Dispatch SMTP
	if smtpHost != "" && smtpTo != "" {
		cfg := SMTPConfig{
			Host:     smtpHost,
			Port:     NormalizeSMTPPort(smtpPort),
			User:     smtpUser,
			Pass:     smtpPass,
			From:     smtpFrom,
			To:       ParseSMTPTo(smtpTo),
			Security: smtpSecurity,
		}
		go func() {
			if err := d.sendSMTP(cfg, host.Name, summary, rootCause); err != nil {
				logger.Warn("notify", host.Name, "Failed to send SMTP alert", "error", err.Error())
			}
		}()
	}
}

func trimExcerpt(rootCause string, maxLen int) string {
	excerpt := strings.TrimSpace(rootCause)
	if len(excerpt) > maxLen {
		return excerpt[:maxLen] + "\n...[truncated]"
	}
	return excerpt
}

func (d *Dispatcher) sendTelegram(token, chatID, hostName, summary, rootCause string) error {
	excerpt := trimExcerpt(rootCause, 1500)

	var msg strings.Builder
	msg.WriteString("🚨 <b>PANTAU DRIFT ALERT</b>\n\n")
	msg.WriteString(fmt.Sprintf("<b>Host:</b> %s\n", html.EscapeString(hostName)))
	msg.WriteString(fmt.Sprintf("<b>Issue:</b> %s\n\n", html.EscapeString(summary)))
	if excerpt != "" {
		msg.WriteString("<b>Root Cause Excerpt:</b>\n")
		msg.WriteString(fmt.Sprintf("<pre>%s</pre>", html.EscapeString(excerpt)))
	}

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       msg.String(),
		"parse_mode": "HTML",
	}

	body, _ := json.Marshal(payload)
	resp, err := d.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendWebhook(url string, host *store.Host, rule *store.DesiredRule, summary, rootCause string) error {
	payload := map[string]interface{}{
		"event":              "drift_alert",
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
		"host_id":            host.ID,
		"host_name":          host.Name,
		"host_ip":            host.Host,
		"summary":            summary,
		"root_cause_excerpt": rootCause,
	}
	if rule != nil {
		payload["rule_kind"] = rule.Kind
		payload["rule_target"] = rule.Target
		payload["rule_expected"] = rule.Expected
	}

	body, _ := json.Marshal(payload)
	resp, err := d.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendNtfy(rawURL, token, hostName, summary, rootCause string) error {
	excerpt := trimExcerpt(rootCause, 1500)
	var bodyText strings.Builder
	bodyText.WriteString(fmt.Sprintf("Host: %s\nIssue: %s\n", hostName, summary))
	if excerpt != "" {
		bodyText.WriteString(fmt.Sprintf("\nRoot Cause Excerpt:\n%s", excerpt))
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, rawURL, strings.NewReader(bodyText.String()))
	if err != nil {
		return err
	}
	req.Header.Set("Title", fmt.Sprintf("Pantau Alert: %s", hostName))
	req.Header.Set("Priority", "high")
	req.Header.Set("Tags", "rotating_light")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy status %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendSlack(url, hostName, summary, rootCause string) error {
	excerpt := trimExcerpt(rootCause, 1500)
	var text strings.Builder
	text.WriteString(fmt.Sprintf("🚨 *PANTAU DRIFT ALERT*\n*Host:* %s\n*Issue:* %s\n", hostName, summary))
	if excerpt != "" {
		text.WriteString(fmt.Sprintf("\n*Root Cause Excerpt:*\n```\n%s\n```", excerpt))
	}

	payload := map[string]string{
		"text": text.String(),
	}
	body, _ := json.Marshal(payload)
	resp, err := d.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack status %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendDiscord(url, hostName, summary, rootCause string) error {
	// Discord message limit is 2000 chars; spec requires truncation <= 1800 chars
	var text strings.Builder
	text.WriteString(fmt.Sprintf("🚨 **PANTAU DRIFT ALERT**\n**Host:** %s\n**Issue:** %s\n", hostName, summary))
	if strings.TrimSpace(rootCause) != "" {
		text.WriteString(fmt.Sprintf("\n**Root Cause Excerpt:**\n```\n%s\n```", strings.TrimSpace(rootCause)))
	}

	content := text.String()
	if len(content) > 1800 {
		content = content[:1780] + "\n...[truncated]"
	}

	payload := map[string]string{
		"content": content,
	}
	body, _ := json.Marshal(payload)
	resp, err := d.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord status %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendSMTP(cfg SMTPConfig, hostName, summary, rootCause string) error {
	subject := fmt.Sprintf("[Pantau] %s: %s", hostName, summary)

	excerpt := trimExcerpt(rootCause, 2000)
	var body strings.Builder
	body.WriteString(fmt.Sprintf("Host: %s\r\n", hostName))
	body.WriteString(fmt.Sprintf("Issue: %s\r\n\r\n", summary))
	if excerpt != "" {
		body.WriteString(fmt.Sprintf("Root Cause Excerpt:\r\n%s\r\n", excerpt))
	}

	sender := d.smtpSender
	if sender == nil {
		sender = &defaultSMTPSender{}
	}
	return sender.Send(cfg, subject, body.String())
}

// TestNotification runs test deliveries for all configured channels and returns results.
func (d *Dispatcher) TestNotification() TestNotifyResponse {
	tgToken, _ := d.db.GetSetting("telegram_token")
	tgChatID, _ := d.db.GetSetting("telegram_chat_id")
	webhookURL, _ := d.db.GetSetting("webhook_url")
	ntfyURL, _ := d.db.GetSetting("ntfy_url")
	ntfyToken, _ := d.db.GetSetting("ntfy_token")
	slackURL, _ := d.db.GetSetting("slack_webhook_url")
	discordURL, _ := d.db.GetSetting("discord_webhook_url")

	smtpHost, _ := d.db.GetSetting("smtp_host")
	smtpPort, _ := d.db.GetSetting("smtp_port")
	smtpUser, _ := d.db.GetSetting("smtp_user")
	smtpPass, _ := d.db.GetSetting("smtp_pass")
	smtpFrom, _ := d.db.GetSetting("smtp_from")
	smtpTo, _ := d.db.GetSetting("smtp_to")
	smtpSecurity, _ := d.db.GetSetting("smtp_security")

	var results []ChannelResult

	// Telegram
	if tgToken != "" || tgChatID != "" {
		if tgToken == "" || tgChatID == "" {
			results = append(results, ChannelResult{
				Channel: "telegram",
				OK:      false,
				Error:   "both telegram_token and telegram_chat_id are required",
			})
		} else {
			endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgToken)
			payload := map[string]interface{}{
				"chat_id":    tgChatID,
				"text":       "✅ <b>Pantau Notification Test</b>\nKoneksi notifikasi Telegram berhasil terhubung!",
				"parse_mode": "HTML",
			}
			body, _ := json.Marshal(payload)
			resp, err := d.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
			if err != nil {
				results = append(results, ChannelResult{Channel: "telegram", OK: false, Error: err.Error()})
			} else {
				resp.Body.Close()
				if resp.StatusCode >= 300 {
					results = append(results, ChannelResult{Channel: "telegram", OK: false, Error: fmt.Sprintf("status %d", resp.StatusCode)})
				} else {
					results = append(results, ChannelResult{Channel: "telegram", OK: true})
				}
			}
		}
	}

	// Webhook
	if webhookURL != "" {
		payload := map[string]string{
			"event":   "test_ping",
			"message": "Pantau webhook test notification",
		}
		body, _ := json.Marshal(payload)
		resp, err := d.httpClient.Post(webhookURL, "application/json", bytes.NewReader(body))
		if err != nil {
			results = append(results, ChannelResult{Channel: "webhook", OK: false, Error: err.Error()})
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				results = append(results, ChannelResult{Channel: "webhook", OK: false, Error: fmt.Sprintf("status %d", resp.StatusCode)})
			} else {
				results = append(results, ChannelResult{Channel: "webhook", OK: true})
			}
		}
	}

	// ntfy
	if ntfyURL != "" {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ntfyURL, strings.NewReader("Pantau ntfy test notification"))
		if err != nil {
			results = append(results, ChannelResult{Channel: "ntfy", OK: false, Error: err.Error()})
		} else {
			req.Header.Set("Title", "Pantau Notification Test")
			req.Header.Set("Priority", "high")
			req.Header.Set("Tags", "rotating_light")
			if strings.TrimSpace(ntfyToken) != "" {
				req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(ntfyToken))
			}
			resp, err := d.httpClient.Do(req)
			if err != nil {
				results = append(results, ChannelResult{Channel: "ntfy", OK: false, Error: err.Error()})
			} else {
				resp.Body.Close()
				if resp.StatusCode >= 300 {
					results = append(results, ChannelResult{Channel: "ntfy", OK: false, Error: fmt.Sprintf("status %d", resp.StatusCode)})
				} else {
					results = append(results, ChannelResult{Channel: "ntfy", OK: true})
				}
			}
		}
	}

	// Slack
	if slackURL != "" {
		payload := map[string]string{
			"text": "✅ *Pantau Notification Test*\nSlack webhook connection successful!",
		}
		body, _ := json.Marshal(payload)
		resp, err := d.httpClient.Post(slackURL, "application/json", bytes.NewReader(body))
		if err != nil {
			results = append(results, ChannelResult{Channel: "slack", OK: false, Error: err.Error()})
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				results = append(results, ChannelResult{Channel: "slack", OK: false, Error: fmt.Sprintf("status %d", resp.StatusCode)})
			} else {
				results = append(results, ChannelResult{Channel: "slack", OK: true})
			}
		}
	}

	// Discord
	if discordURL != "" {
		payload := map[string]string{
			"content": "✅ **Pantau Notification Test**\nDiscord webhook connection successful!",
		}
		body, _ := json.Marshal(payload)
		resp, err := d.httpClient.Post(discordURL, "application/json", bytes.NewReader(body))
		if err != nil {
			results = append(results, ChannelResult{Channel: "discord", OK: false, Error: err.Error()})
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				results = append(results, ChannelResult{Channel: "discord", OK: false, Error: fmt.Sprintf("status %d", resp.StatusCode)})
			} else {
				results = append(results, ChannelResult{Channel: "discord", OK: true})
			}
		}
	}

	// SMTP
	if smtpHost != "" || smtpTo != "" {
		toList := ParseSMTPTo(smtpTo)
		if smtpHost == "" || len(toList) == 0 {
			results = append(results, ChannelResult{
				Channel: "smtp",
				OK:      false,
				Error:   "smtp_host and at least one recipient in smtp_to are required",
			})
		} else {
			cfg := SMTPConfig{
				Host:     smtpHost,
				Port:     NormalizeSMTPPort(smtpPort),
				User:     smtpUser,
				Pass:     smtpPass,
				From:     smtpFrom,
				To:       toList,
				Security: smtpSecurity,
			}
			sender := d.smtpSender
			if sender == nil {
				sender = &defaultSMTPSender{}
			}
			err := sender.Send(cfg, "[Pantau] Test Notification", "Pantau SMTP notification test successful!")
			if err != nil {
				results = append(results, ChannelResult{Channel: "smtp", OK: false, Error: err.Error()})
			} else {
				results = append(results, ChannelResult{Channel: "smtp", OK: true})
			}
		}
	}

	if len(results) == 0 {
		return TestNotifyResponse{
			OK:      false,
			Error:   "no notification channels configured",
			Results: []ChannelResult{},
		}
	}

	allOK := true
	var errMsgs []string
	for _, res := range results {
		if !res.OK {
			allOK = false
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", res.Channel, res.Error))
		}
	}

	var overallErr string
	if len(errMsgs) > 0 {
		overallErr = strings.Join(errMsgs, "; ")
	}

	return TestNotifyResponse{
		OK:      allOK,
		Error:   overallErr,
		Results: results,
	}
}
