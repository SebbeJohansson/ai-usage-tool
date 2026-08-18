// Package copilotcredit reads how many of GitHub's included Copilot AI
// credits one seat has burned through this month, using GitHub's Enhanced
// Billing REST API scoped to github.com (not a GitHub Enterprise instance).
package copilotcredit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	githubAPI     = "https://api.github.com"
	billingAPIRev = "2026-03-10"
)

// LineItem is one product/SKU/model row from GitHub's usage report.
type LineItem struct {
	Product      string
	SKU          string
	Model        string
	Unit         string
	Rate         float64
	GrossUnits   float64
	GrossUSD     float64
	InPlanUnits  float64 // covered by the included monthly allowance
	InPlanUSD    float64
	OverageUnits float64 // billed beyond the allowance
	OverageUSD   float64
}

// Report is one seat's usage for a billing period.
type Report struct {
	Org      string
	Login    string
	PulledAt time.Time
	Year     int
	Month    int
	Lines    []LineItem
}

// InPlanTotal sums the portion of usage covered by the included allowance.
func (r *Report) InPlanTotal() float64 {
	var total float64
	for _, l := range r.Lines {
		total += l.InPlanUnits
	}
	return total
}

// OverageTotal sums the portion of usage billed beyond the allowance.
func (r *Report) OverageTotal() float64 {
	var total float64
	for _, l := range r.Lines {
		total += l.OverageUnits
	}
	return total
}

// Client reads Copilot AI-credit usage for logins inside one org.
type Client struct {
	org   string
	token string
	http  *http.Client
}

func New(org, token string) *Client {
	return &Client{org: org, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// ForLogin fetches the month-to-date report for a single GitHub login. The
// token must belong to an org owner or admin — fine-grained tokens need the
// organization "Administration" permission at read level.
func (c *Client) ForLogin(ctx context.Context, login string) (*Report, error) {
	if c.token == "" || c.org == "" || login == "" {
		return nil, fmt.Errorf("copilotcredit: org, token and login are all required")
	}

	q := url.Values{"user": {login}}
	endpoint := fmt.Sprintf("%s/organizations/%s/settings/billing/ai_credit/usage?%s",
		githubAPI, url.PathEscape(c.org), q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", billingAPIRev)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilotcredit: request for %s failed: %w", login, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("copilotcredit: %s returned %d for %s/%s: %s",
			endpoint, resp.StatusCode, c.org, login, detail)
	}

	var body billingResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("copilotcredit: could not parse response for %s: %w", login, err)
	}

	report := &Report{
		Org:      c.org,
		Login:    login,
		PulledAt: time.Now().UTC(),
		Year:     body.TimePeriod.Year,
		Month:    body.TimePeriod.Month,
	}
	for _, item := range body.UsageItems {
		report.Lines = append(report.Lines, LineItem{
			Product:      item.Product,
			SKU:          item.SKU,
			Model:        item.Model,
			Unit:         item.UnitType,
			Rate:         item.PricePerUnit,
			GrossUnits:   item.GrossQuantity,
			GrossUSD:     item.GrossAmount,
			InPlanUnits:  item.DiscountQuantity,
			InPlanUSD:    item.DiscountAmount,
			OverageUnits: item.NetQuantity,
			OverageUSD:   item.NetAmount,
		})
	}
	return report, nil
}

// billingResponse is GitHub's wire format for the ai_credit/usage endpoint;
// field names/casing here are fixed by GitHub's API, not a style choice.
type billingResponse struct {
	TimePeriod struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	} `json:"timePeriod"`
	UsageItems []struct {
		Product          string  `json:"product"`
		SKU              string  `json:"sku"`
		Model            string  `json:"model"`
		UnitType         string  `json:"unitType"`
		PricePerUnit     float64 `json:"pricePerUnit"`
		GrossQuantity    float64 `json:"grossQuantity"`
		GrossAmount      float64 `json:"grossAmount"`
		DiscountQuantity float64 `json:"discountQuantity"`
		DiscountAmount   float64 `json:"discountAmount"`
		NetQuantity      float64 `json:"netQuantity"`
		NetAmount        float64 `json:"netAmount"`
	} `json:"usageItems"`
}
