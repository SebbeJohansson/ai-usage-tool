// Package claudeusage reads the signed-in Claude.ai account's own quota
// meters — the "session" and "weekly" usage bars the web app shows under
// Settings — through the same private endpoint the web app itself calls.
// Anthropic doesn't publish this endpoint, so the shape below was reverse
// engineered from a browser session and can change without notice.
package claudeusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// Meter is one quota bar reported by the endpoint: how full it is (0-100)
// and, for per-model weekly bars, which model/surface it applies to.
type Meter struct {
	Kind     string
	Window   string
	PctUsed  float64
	Severity string
	ResetsAt *time.Time
	Model    string
	Surface  string
	Live     bool
}

// ID disambiguates meters that share Kind — several weekly per-model bars
// all report Kind "weekly_scoped" but differ by Model.
func (m Meter) ID() string {
	id := m.Kind
	if m.Model != "" {
		id += "/" + m.Model
	}
	if m.Surface != "" {
		id += "/" + m.Surface
	}
	return id
}

// Reading is what came back from a single poll.
type Reading struct {
	At     time.Time
	Meters []Meter
}

// Client polls quota meters for one Claude.ai account, identified by the
// org it belongs to plus that account's session cookie.
type Client struct {
	org     string
	cookie  string
	timeout time.Duration
}

func New(org, sessionCookie string) *Client {
	return &Client{org: org, cookie: sessionCookie, timeout: 30 * time.Second}
}

var errNotConfigured = errors.New("claudeusage: org and session cookie are both required")

// Poll fetches the account's current quota meters.
func (c *Client) Poll(_ context.Context) (*Reading, error) {
	if c.org == "" || c.cookie == "" {
		return nil, errNotConfigured
	}

	httpClient, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profiles.Chrome_144),
		tlsclient.WithTimeoutSeconds(int(c.timeout.Seconds())),
	)
	if err != nil {
		return nil, fmt.Errorf("claudeusage: build http client: %w", err)
	}

	endpoint := fmt.Sprintf("https://claude.ai/api/organizations/%s/usage", c.org)
	req, err := fhttp.NewRequest(fhttp.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("claudeusage: build request: %w", err)
	}
	applyBrowserHeaders(req, c.cookie)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claudeusage: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("claudeusage: reading response: %w", err)
	}
	if resp.StatusCode != fhttp.StatusOK {
		return nil, fmt.Errorf("claudeusage: server returned %d, session cookie may have expired: %s",
			resp.StatusCode, firstBytes(raw, 200))
	}

	return decodeReading(raw)
}

// browserHeaderOrder is the header sequence the Claude.ai frontend itself
// sends; without it (and without the matching fhttp/tls-client stack) the
// request looks automated and gets refused before it reaches the handler.
var browserHeaderOrder = []string{
	"accept",
	"accept-language",
	"anthropic-client-platform",
	"cookie",
	"user-agent",
}

func applyBrowserHeaders(req *fhttp.Request, sessionCookie string) {
	values := map[string]string{
		"accept":                    "*/*",
		"accept-language":           "en-US,en;q=0.9",
		"anthropic-client-platform": "web_claude_ai",
		"cookie":                    "sessionKey=" + sessionCookie,
		"user-agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
	}
	header := fhttp.Header{}
	for _, key := range browserHeaderOrder {
		header[key] = []string{values[key]}
	}
	header[fhttp.HeaderOrderKey] = browserHeaderOrder
	req.Header = header
}

// wireReading mirrors just the fields of the endpoint's JSON body this
// package actually reads.
type wireReading struct {
	Limits []struct {
		Kind     string     `json:"kind"`
		Group    string     `json:"group"`
		Percent  float64    `json:"percent"`
		Severity string     `json:"severity"`
		ResetsAt *time.Time `json:"resets_at"`
		IsActive bool       `json:"is_active"`
		Scope    *struct {
			Model *struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
			Surface *string `json:"surface"`
		} `json:"scope"`
	} `json:"limits"`
}

func decodeReading(raw []byte) (*Reading, error) {
	var wire wireReading
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("claudeusage: unexpected response shape: %w", err)
	}

	out := &Reading{At: time.Now().UTC()}
	for _, l := range wire.Limits {
		m := Meter{
			Kind:     l.Kind,
			Window:   l.Group,
			PctUsed:  l.Percent,
			Severity: l.Severity,
			ResetsAt: l.ResetsAt,
			Live:     l.IsActive,
		}
		if l.Scope != nil {
			if l.Scope.Model != nil {
				m.Model = l.Scope.Model.DisplayName
			}
			if l.Scope.Surface != nil {
				m.Surface = *l.Scope.Surface
			}
		}
		out.Meters = append(out.Meters, m)
	}
	return out, nil
}

func firstBytes(b []byte, n int) string {
	if len(b) < n {
		return string(b)
	}
	return string(b[:n])
}
