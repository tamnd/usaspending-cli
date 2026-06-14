package usaspending

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.Rate = 0
	c.HTTP = srv.Client()
	// point BaseURL at the test server by monkeypatching via the transport
	return c
}

// postFixture serves a fixed JSON body for any POST.
func postFixture(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad content-type", http.StatusBadRequest)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no user-agent", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestSearchAwards(t *testing.T) {
	fixture := `{"results":[
		{"Award ID":"TEST-001","Recipient Name":"ACME Corp","Awarding Agency":"Dept of Defense","Start Date":"2024-01-15","Award Amount":1500000.00,"Award Type":"Definitive Contract"},
		{"Award ID":"TEST-002","Recipient Name":"Example LLC","Awarding Agency":"Dept of Health","Start Date":"2024-03-01","Award Amount":750000.50,"Award Type":"Purchase Order"}
	],"page_metadata":{"total":2,"hasNext":false}}`

	srv := postFixture(fixture)
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	// Override the base URL by patching the transport to redirect to test server
	c.HTTP = &http.Client{
		Transport: rewriteTransport{base: srv.URL, real: BaseURL, inner: http.DefaultTransport},
	}

	awards, err := c.SearchAwards(context.Background(), SearchAwardsInput{
		AwardType: "contract",
		Year:      "2024",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SearchAwards: %v", err)
	}
	if len(awards) != 2 {
		t.Fatalf("len(awards) = %d, want 2", len(awards))
	}
	if awards[0].ID != "TEST-001" {
		t.Errorf("awards[0].ID = %q, want TEST-001", awards[0].ID)
	}
	if awards[0].Recipient != "ACME Corp" {
		t.Errorf("awards[0].Recipient = %q, want ACME Corp", awards[0].Recipient)
	}
	if awards[0].Amount != 1500000.00 {
		t.Errorf("awards[0].Amount = %f, want 1500000.00", awards[0].Amount)
	}
	if awards[1].ID != "TEST-002" {
		t.Errorf("awards[1].ID = %q, want TEST-002", awards[1].ID)
	}
}

func TestListAgencies(t *testing.T) {
	fixture := `{"results":[
		{"id":"97","name":"Department of Defense","amount":850000000000.00},
		{"id":"75","name":"Department of Health and Human Services","amount":120000000000.00}
	],"total_results":2}`

	srv := postFixture(fixture)
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{
		Transport: rewriteTransport{base: srv.URL, real: BaseURL, inner: http.DefaultTransport},
	}

	agencies, err := c.ListAgencies(context.Background(), ListAgenciesInput{
		FY:    "2024",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("ListAgencies: %v", err)
	}
	if len(agencies) != 2 {
		t.Fatalf("len(agencies) = %d, want 2", len(agencies))
	}
	if agencies[0].Name != "Department of Defense" {
		t.Errorf("agencies[0].Name = %q, want Department of Defense", agencies[0].Name)
	}
	if agencies[0].ID != "97" {
		t.Errorf("agencies[0].ID = %q, want 97", agencies[0].ID)
	}
	if agencies[0].Obligated != 850000000000.00 {
		t.Errorf("agencies[0].Obligated = %f", agencies[0].Obligated)
	}
}

func TestTopRecipients(t *testing.T) {
	fixture := `{"results":[
		{"name":"LOCKHEED MARTIN CORPORATION","aggregated_amount":null,"id":"abc123"},
		{"name":"BOEING COMPANY","aggregated_amount":45000000000.00,"id":"def456"}
	],"total":2}`

	srv := postFixture(fixture)
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.HTTP = &http.Client{
		Transport: rewriteTransport{base: srv.URL, real: BaseURL, inner: http.DefaultTransport},
	}

	recipients, err := c.TopRecipients(context.Background(), TopRecipientsInput{
		Year:      "2024",
		AwardType: "contract",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("TopRecipients: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("len(recipients) = %d, want 2", len(recipients))
	}
	// null aggregated_amount handled gracefully
	if recipients[0].Name != "LOCKHEED MARTIN CORPORATION" {
		t.Errorf("recipients[0].Name = %q, want LOCKHEED MARTIN CORPORATION", recipients[0].Name)
	}
	if recipients[0].Amount != 0 {
		t.Errorf("recipients[0].Amount = %f, want 0 (null)", recipients[0].Amount)
	}
	if recipients[1].Amount != 45000000000.00 {
		t.Errorf("recipients[1].Amount = %f, want 45000000000.00", recipients[1].Amount)
	}
}

func TestRetryOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{},
		})
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.Retries = 5
	c.HTTP = &http.Client{
		Transport: rewriteTransport{base: srv.URL, real: BaseURL, inner: http.DefaultTransport},
	}

	_, err := c.ListAgencies(context.Background(), ListAgenciesInput{FY: "2024", Limit: 1})
	if err != nil {
		t.Fatalf("ListAgencies after retries: %v", err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}

// rewriteTransport redirects requests aimed at the real API base to the test
// server, so we don't need to change the BaseURL constant.
type rewriteTransport struct {
	base  string // test server URL, e.g. http://127.0.0.1:PORT
	real  string // real BaseURL, e.g. https://api.usaspending.gov/api/v2
	inner http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone and rewrite the URL.
	u := *req.URL
	raw := u.String()
	if len(raw) >= len(t.real) && raw[:len(t.real)] == t.real {
		u.Scheme = "http"
		u.Host = req.Host
		// parse the test server host
		testURL := t.base
		// strip scheme from testURL
		if len(testURL) > 7 {
			u.Host = testURL[7:] // strip "http://"
		}
		req2 := req.Clone(req.Context())
		req2.URL = &u
		req2.Host = u.Host
		return t.inner.RoundTrip(req2)
	}
	return t.inner.RoundTrip(req)
}
