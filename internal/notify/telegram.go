// Package notify tells people about status changes: it decides what is
// worth a message and sends it to Telegram.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// DefaultTelegramURL is the Telegram Bot API. It is not configurable: the
// bot token is part of every request URL, so pointing x3f elsewhere would
// hand the token to that server. Only tests replace it.
const DefaultTelegramURL = "https://api.telegram.org"

// Telegram sends messages through a Telegram bot.
type Telegram struct {
	// BaseURL is DefaultTelegramURL; tests use a mock server.
	BaseURL string
	// Token is the bot token. It never appears in errors.
	Token string
	// ChatID is a numeric chat ID or "@channelname".
	ChatID string
	// HTTP is the client to use; nil means one without redirects.
	HTTP *http.Client
	// Sleep waits before a retry; nil means a real wait. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
}

// retryDelay is the wait before retrying after a network or server error.
const retryDelay = time.Second

// Escape makes text safe for parse_mode HTML. Telegram requires <, > and &
// to be escaped; html.EscapeString also escapes quotes, as numeric
// entities, which Telegram supports.
func Escape(s string) string {
	return html.EscapeString(s)
}

// Send sends an HTML message (see Escape). Network errors, server errors
// and flood control (HTTP 429) are retried once; flood control only if its
// wait ends before ctx's deadline.
func (t *Telegram) Send(ctx context.Context, text string) error {
	err := t.send(ctx, text)
	var r *retryable
	if errors.As(err, &r) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > r.after {
			if t.sleep(ctx, r.after) == nil {
				err = t.send(ctx, text)
			}
		}
	}
	return t.scrub(err)
}

// retryable is an error worth one more attempt after a wait.
type retryable struct {
	err   error
	after time.Duration
}

func (r *retryable) Error() string { return r.err.Error() }

// response is the Bot API's answer, reduced to what x3f needs.
type response struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (t *Telegram) send(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id":              chatID(t.ChatID),
		"text":                 text,
		"parse_mode":           "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return errors.New("Telegram: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client().Do(req)
	if err != nil {
		// A *url.Error includes the request URL, and with it the token.
		// Keep only the underlying reason.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return &retryable{fmt.Errorf("Telegram: %v", err), retryDelay}
	}
	defer resp.Body.Close()

	var r response
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	switch {
	case err == nil && r.OK && resp.StatusCode == http.StatusOK:
		return nil
	case err != nil && resp.StatusCode >= 500:
		return &retryable{fmt.Errorf("Telegram: HTTP %d", resp.StatusCode), retryDelay}
	case err != nil:
		return fmt.Errorf("Telegram: HTTP %d, unexpected response", resp.StatusCode)
	}

	e := fmt.Errorf("Telegram: HTTP %d: %s", resp.StatusCode, clean(r.Description, 200))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &retryable{e, time.Duration(max(r.Parameters.RetryAfter, 1)) * time.Second}
	case resp.StatusCode >= 500:
		return &retryable{e, retryDelay}
	}
	return e
}

// client returns t.HTTP or a client that does not follow redirects: a
// redirect is not part of the Bot API, and the token belongs to
// api.telegram.org only.
func (t *Telegram) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func (t *Telegram) sleep(ctx context.Context, d time.Duration) error {
	if t.Sleep != nil {
		return t.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scrub removes the token from an error message, as a safety net in case
// some error text quotes the request URL after all.
func (t *Telegram) scrub(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if t.Token != "" && strings.Contains(msg, t.Token) {
		msg = strings.ReplaceAll(msg, t.Token, "[token]")
	}
	return errors.New(msg)
}

// chatID sends a numeric chat ID as a JSON number and "@channelname" as a
// string; the Bot API accepts "Integer or String".
func chatID(s string) any {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return s
}

// clean removes control and format characters from text the Telegram server
// sent, and cuts it to limit characters.
func clean(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}
