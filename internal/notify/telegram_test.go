package notify

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "123456:AAtesttoken-not-real" // gitleaks:allow (test dummy)

// reply is one answer of the mock Bot API: an HTTP status and a fixture
// from testdata/telegram (see testdata/SOURCES.md).
type reply struct {
	status  int
	fixture string
}

// fakeTelegram is a mock Bot API that answers with the given replies in
// order (the last one repeats) and records the requests.
type fakeTelegram struct {
	*httptest.Server
	mu       sync.Mutex
	paths    []string
	requests []map[string]any
}

func newFakeTelegram(t *testing.T, replies ...reply) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != http.MethodPost ||
			r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		n := len(f.requests)
		f.paths = append(f.paths, r.URL.Path)
		f.requests = append(f.requests, body)
		f.mu.Unlock()

		rp := replies[min(n, len(replies)-1)]
		data, err := os.ReadFile(filepath.Join("testdata", "telegram", rp.fixture))
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rp.status)
		w.Write(data)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTelegram) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// newTelegram returns a client for f that records its waits instead of
// sleeping.
func newTelegram(url, chat string, waits *[]time.Duration) *Telegram {
	return &Telegram{BaseURL: url, Token: testToken, ChatID: chat,
		Sleep: func(_ context.Context, d time.Duration) error {
			*waits = append(*waits, d)
			return nil
		}}
}

func TestTelegramSend(t *testing.T) {
	f := newFakeTelegram(t, reply{200, "ok.json"})
	var waits []time.Duration
	if err := newTelegram(f.URL, "-1001234567890", &waits).Send(context.Background(), "<b>a</b> &amp; b"); err != nil {
		t.Fatal(err)
	}
	if f.paths[0] != "/bot"+testToken+"/sendMessage" {
		t.Errorf("path = %q", f.paths[0])
	}
	r := f.requests[0]
	// JSON numbers decode to float64 in a map[string]any.
	if r["chat_id"] != float64(-1001234567890) || r["text"] != "<b>a</b> &amp; b" || r["parse_mode"] != "HTML" {
		t.Errorf("request body = %v", r)
	}
	if lp, _ := r["link_preview_options"].(map[string]any); lp["is_disabled"] != true || len(r) != 4 {
		t.Errorf("request body = %v", r)
	}

	// A channel name is sent as a string.
	if err := newTelegram(f.URL, "@x3f_alerts", &waits).Send(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if c := f.requests[1]["chat_id"]; c != "@x3f_alerts" {
		t.Errorf("chat_id = %#v", c)
	}
	if len(waits) != 0 {
		t.Errorf("waited %v", waits)
	}
}

func TestTelegramErrors(t *testing.T) {
	tests := []struct {
		name     string
		replies  []reply
		wantErr  string // "" for success
		requests int
		waits    []time.Duration
	}{
		{
			name:    "wrong token is not retried",
			replies: []reply{{401, "unauthorized.json"}},
			wantErr: "Telegram: HTTP 401: Unauthorized", requests: 1,
		},
		{
			name:    "wrong chat is not retried",
			replies: []reply{{400, "chat-not-found.json"}},
			wantErr: "Telegram: HTTP 400: Bad Request: chat not found", requests: 1,
		},
		{
			name:     "flood control: wait as told, then success",
			replies:  []reply{{429, "too-many-requests.json"}, {200, "ok.json"}},
			requests: 2, waits: []time.Duration{5 * time.Second},
		},
		{
			name:    "server error: retried once",
			replies: []reply{{500, "internal-server-error.json"}},
			wantErr: "Telegram: HTTP 500: Internal Server Error: restart", requests: 2,
			waits: []time.Duration{time.Second},
		},
		{
			name:     "server error, then success",
			replies:  []reply{{500, "internal-server-error.json"}, {200, "ok.json"}},
			requests: 2, waits: []time.Duration{time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeTelegram(t, tt.replies...)
			var waits []time.Duration
			err := newTelegram(f.URL, "1", &waits).Send(context.Background(), "x")
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
			if f.count() != tt.requests {
				t.Errorf("%d requests, want %d", f.count(), tt.requests)
			}
			if !equalDurations(waits, tt.waits) {
				t.Errorf("waits = %v, want %v", waits, tt.waits)
			}
		})
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTelegramFloodWaitBeyondDeadline: a retry_after longer than the time
// left is not waited for.
func TestTelegramFloodWaitBeyondDeadline(t *testing.T) {
	f := newFakeTelegram(t, reply{429, "too-many-requests.json"})
	var waits []time.Duration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := newTelegram(f.URL, "1", &waits).Send(ctx, "x")
	if err == nil || err.Error() != "Telegram: HTTP 429: Too Many Requests: retry after 5" {
		t.Errorf("error = %v", err)
	}
	if f.count() != 1 || len(waits) != 0 {
		t.Errorf("%d requests, waits %v; want 1 request, no wait", f.count(), waits)
	}
}

// TestTelegramTokenNeverInErrors covers network errors, whose *url.Error
// contains the request URL, and an unexpected answer.
func TestTelegramTokenNeverInErrors(t *testing.T) {
	// A listener that is closed at once: connections are refused.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := "http://" + l.Addr().String()
	l.Close()

	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<html>"+r.URL.Path+"</html>", http.StatusBadGateway)
	}))
	defer html.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid"+r.URL.Path, http.StatusFound)
	}))
	defer redirect.Close()

	for _, tt := range []struct{ url, want string }{
		{dead, "Telegram: dial tcp"},
		{html.URL, "Telegram: HTTP 502"},
		{redirect.URL, "Telegram: HTTP 302, unexpected response"},
	} {
		var waits []time.Duration
		err := newTelegram(tt.url, "1", &waits).Send(context.Background(), "x")
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want prefix %q", tt.want, err, tt.want)
			continue
		}
		// No "/bot" either: the URL is dropped, not just the token
		// masked (scrub is only the safety net).
		if strings.Contains(err.Error(), "AAtesttoken") || strings.Contains(err.Error(), "/bot") {
			t.Errorf("error leaks the token: %v", err)
		}
	}
}

func TestTelegramScrub(t *testing.T) {
	tg := &Telegram{Token: testToken}
	err := tg.scrub(&retryable{err: errorString("Post /bot" + testToken + "/sendMessage: boom")})
	if err.Error() != "Post /bot[token]/sendMessage: boom" {
		t.Errorf("got %q", err)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

func TestEscape(t *testing.T) {
	got := Escape(`<b>node</b> & "x" 'y' &amp;`)
	want := `&lt;b&gt;node&lt;/b&gt; &amp; &#34;x&#34; &#39;y&#39; &amp;amp;`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
