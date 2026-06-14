package usaspending

import (
	"context"
	"time"

	"github.com/tamnd/any-cli/kit"
)

// domain.go exposes usaspending as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/usaspending-cli/usaspending"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// usaspending:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone usaspending binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the usaspending driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "usaspending",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "usaspending",
			Short:  "A command line for USASpending.gov federal spending data.",
			Long: `A command line for USASpending.gov federal spending data.

usaspending reads public US federal government spending data over plain HTTPS,
shapes it into clean records, and prints output that pipes into the rest of
your tools. No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/usaspending-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// awards: search federal contracts or grants.
	kit.Handle(app, kit.OpMeta{
		Name:    "awards",
		Group:   "read",
		List:    true,
		Summary: "Search federal contracts and grants",
	}, searchAwards)

	// agencies: list federal agencies with their obligated spending.
	kit.Handle(app, kit.OpMeta{
		Name:    "agencies",
		Group:   "read",
		List:    true,
		Summary: "List federal agencies and their obligated spending",
	}, listAgencies)

	// recipients: top recipients by spending amount.
	kit.Handle(app, kit.OpMeta{
		Name:    "recipients",
		Group:   "read",
		List:    true,
		Summary: "List top recipients by spending amount",
	}, topRecipients)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type awardsInput struct {
	Agency    string        `kit:"flag" help:"filter by awarding agency name"`
	Keyword   string        `kit:"flag" help:"keyword to search awards"`
	AwardType string        `kit:"flag" help:"award type: contract or grant" default:"contract"`
	Year      string        `kit:"flag" help:"fiscal year (e.g. 2024)" default:"2024"`
	Limit     int           `kit:"flag,inherit" help:"max results"`
	Timeout   time.Duration `kit:"flag,inherit" help:"request timeout"`
	Client    *Client       `kit:"inject"`
}

type agenciesInput struct {
	FY      string        `kit:"flag" help:"fiscal year (e.g. 2024)" default:"2024"`
	Limit   int           `kit:"flag,inherit" help:"max results"`
	Timeout time.Duration `kit:"flag,inherit" help:"request timeout"`
	Client  *Client       `kit:"inject"`
}

type recipientsInput struct {
	Year      string        `kit:"flag" help:"calendar year (e.g. 2024)" default:"2024"`
	AwardType string        `kit:"flag" help:"award type: contract or grant" default:"contract"`
	Limit     int           `kit:"flag,inherit" help:"max results"`
	Timeout   time.Duration `kit:"flag,inherit" help:"request timeout"`
	Client    *Client       `kit:"inject"`
}

// --- handlers ---

func searchAwards(ctx context.Context, in awardsInput, emit func(*Award) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	awards, err := in.Client.SearchAwards(ctx, SearchAwardsInput{
		Agency:    in.Agency,
		Keyword:   in.Keyword,
		AwardType: in.AwardType,
		Year:      in.Year,
		Limit:     limit,
	})
	if err != nil {
		return err
	}
	for _, a := range awards {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func listAgencies(ctx context.Context, in agenciesInput, emit func(*Agency) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	agencies, err := in.Client.ListAgencies(ctx, ListAgenciesInput{
		FY:    in.FY,
		Limit: limit,
	})
	if err != nil {
		return err
	}
	for _, a := range agencies {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func topRecipients(ctx context.Context, in recipientsInput, emit func(*Recipient) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	recipients, err := in.Client.TopRecipients(ctx, TopRecipientsInput{
		Year:      in.Year,
		AwardType: in.AwardType,
		Limit:     limit,
	})
	if err != nil {
		return err
	}
	for _, r := range recipients {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}
