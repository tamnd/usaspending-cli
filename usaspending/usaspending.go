// Package usaspending is the library behind the usaspending command line:
// the HTTP client, request shaping, and the typed data models for the
// USASpending API (https://api.usaspending.gov/api/v2).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package usaspending

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to USASpending.gov. An honest
// User-Agent is both polite and keeps you unblocked.
const DefaultUserAgent = "usaspending-cli/0.1 (tamnd87@gmail.com)"

// Host is the API hostname this client talks to, and the host the URI driver
// in domain.go claims.
const Host = "api.usaspending.gov"

// BaseURL is the root every request is built from.
const BaseURL = "https://api.usaspending.gov/api/v2"

// Award type code groups.
var (
	contractCodes = []string{"A", "B", "C", "D"}
	grantCodes    = []string{"02", "03", "04", "05"}
)

// Client talks to the USASpending API over HTTPS.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 300ms
// minimum gap between requests, and three retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Retries:   3,
	}
}

// post sends a POST request with a JSON body and decodes the JSON response
// into out. It paces and retries according to the client's settings.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		retry, err := c.doPost(ctx, path, data, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			return err
		}
	}
	return fmt.Errorf("post %s: %w", path, lastErr)
}

func (c *Client) doPost(ctx context.Context, path string, data []byte, out any) (retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, fmt.Errorf("decode: %w", err)
	}
	return false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// awardTypeCodes returns the API codes for "contract" or "grant".
func awardTypeCodes(awardType string) []string {
	if awardType == "grant" {
		return grantCodes
	}
	return contractCodes
}

// Award represents a single federal contract or grant award.
type Award struct {
	ID             string  `kit:"id" json:"id"`
	Recipient      string  `json:"recipient"`
	AwardingAgency string  `json:"awarding_agency"`
	StartDate      string  `json:"start_date"`
	Amount         float64 `json:"amount"`
	AwardType      string  `json:"award_type"`
}

// Agency represents a federal agency and its obligated spending.
type Agency struct {
	ID        string  `kit:"id" json:"id"`
	Name      string  `json:"name"`
	Obligated float64 `json:"obligated_amount"`
}

// Recipient represents a top spending recipient.
type Recipient struct {
	Name   string  `kit:"id" json:"name"`
	Amount float64 `json:"amount"`
	ID     string  `json:"id"`
}

// SearchAwardsInput is the input for SearchAwards.
type SearchAwardsInput struct {
	Agency    string
	Keyword   string
	AwardType string // "contract" or "grant"
	Year      string
	Limit     int
}

// ListAgenciesInput is the input for ListAgencies.
type ListAgenciesInput struct {
	FY    string
	Limit int
}

// TopRecipientsInput is the input for TopRecipients.
type TopRecipientsInput struct {
	Year      string
	AwardType string // "contract" or "grant"
	Limit     int
}

// SearchAwards searches federal awards (contracts or grants) via the
// /search/spending_by_award/ endpoint.
func (c *Client) SearchAwards(ctx context.Context, in SearchAwardsInput) ([]*Award, error) {
	if in.Limit <= 0 {
		in.Limit = 10
	}
	if in.Year == "" {
		in.Year = "2024"
	}

	filters := map[string]any{
		"award_type_codes": awardTypeCodes(in.AwardType),
		"time_period": []map[string]string{
			{"start_date": in.Year + "-01-01", "end_date": in.Year + "-12-31"},
		},
	}
	if in.Agency != "" {
		filters["agencies"] = []map[string]string{
			{"type": "awarding", "tier": "toptier", "name": in.Agency},
		}
	}
	if in.Keyword != "" {
		filters["keywords"] = []string{in.Keyword}
	}

	reqBody := map[string]any{
		"subawards": false,
		"limit":     in.Limit,
		"page":      1,
		"filters":   filters,
		"fields":    []string{"Award ID", "Recipient Name", "Start Date", "Award Amount", "Awarding Agency", "Award Type"},
	}

	var resp struct {
		Results []map[string]any `json:"results"`
	}
	if err := c.post(ctx, "/search/spending_by_award/", reqBody, &resp); err != nil {
		return nil, err
	}

	awards := make([]*Award, 0, len(resp.Results))
	for _, r := range resp.Results {
		a := &Award{}
		if v, ok := r["Award ID"].(string); ok {
			a.ID = v
		}
		if v, ok := r["Recipient Name"].(string); ok {
			a.Recipient = v
		}
		if v, ok := r["Awarding Agency"].(string); ok {
			a.AwardingAgency = v
		}
		if v, ok := r["Start Date"].(string); ok {
			a.StartDate = v
		}
		if v, ok := r["Award Amount"].(float64); ok {
			a.Amount = v
		}
		if v, ok := r["Award Type"].(string); ok {
			a.AwardType = v
		}
		awards = append(awards, a)
	}
	return awards, nil
}

// ListAgencies lists federal agencies and their obligated spending via the
// /spending/ endpoint.
func (c *Client) ListAgencies(ctx context.Context, in ListAgenciesInput) ([]*Agency, error) {
	if in.Limit <= 0 {
		in.Limit = 20
	}
	if in.FY == "" {
		in.FY = "2024"
	}

	reqBody := map[string]any{
		"type": "agency",
		"filters": map[string]string{
			"fy":      in.FY,
			"quarter": "4",
		},
		"limit": in.Limit,
	}

	var resp struct {
		Results []struct {
			ID     string  `json:"id"`
			Name   string  `json:"name"`
			Amount float64 `json:"amount"`
		} `json:"results"`
	}
	if err := c.post(ctx, "/spending/", reqBody, &resp); err != nil {
		return nil, err
	}

	agencies := make([]*Agency, 0, len(resp.Results))
	for _, r := range resp.Results {
		agencies = append(agencies, &Agency{
			ID:        r.ID,
			Name:      r.Name,
			Obligated: r.Amount,
		})
	}
	return agencies, nil
}

// TopRecipients returns the top spending recipients via the
// /search/spending_by_category/recipient/ endpoint.
func (c *Client) TopRecipients(ctx context.Context, in TopRecipientsInput) ([]*Recipient, error) {
	if in.Limit <= 0 {
		in.Limit = 10
	}
	if in.Year == "" {
		in.Year = "2024"
	}

	reqBody := map[string]any{
		"category": "recipient",
		"limit":    in.Limit,
		"filters": map[string]any{
			"time_period": []map[string]string{
				{"start_date": in.Year + "-01-01", "end_date": in.Year + "-12-31"},
			},
			"award_type_codes": awardTypeCodes(in.AwardType),
		},
	}

	var resp struct {
		Results []struct {
			Name              string   `json:"name"`
			AggregatedAmount  *float64 `json:"aggregated_amount"`
			Amount            *float64 `json:"amount"`
			ID                string   `json:"id"`
		} `json:"results"`
	}
	if err := c.post(ctx, "/search/spending_by_category/recipient/", reqBody, &resp); err != nil {
		return nil, err
	}

	recipients := make([]*Recipient, 0, len(resp.Results))
	for _, r := range resp.Results {
		rec := &Recipient{
			Name: r.Name,
			ID:   r.ID,
		}
		// aggregated_amount may be null; fall back to amount if set
		if r.AggregatedAmount != nil {
			rec.Amount = *r.AggregatedAmount
		} else if r.Amount != nil {
			rec.Amount = *r.Amount
		}
		recipients = append(recipients, rec)
	}
	return recipients, nil
}
