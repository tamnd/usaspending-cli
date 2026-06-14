package usaspending

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the domain info and host wiring,
// which need no network. The client's HTTP behaviour is covered in
// usaspending_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "usaspending" {
		t.Errorf("Scheme = %q, want usaspending", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "usaspending" {
		t.Errorf("Identity.Binary = %q, want usaspending", info.Identity.Binary)
	}
}

// TestHostWiring mounts the driver in a kit Host and checks that
// kit.Open finds it (the init in domain.go registers the domain).
func TestHostWiring(t *testing.T) {
	_, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAwardTypeCodes(t *testing.T) {
	codes := awardTypeCodes("contract")
	if len(codes) != 4 {
		t.Errorf("contract codes len = %d, want 4", len(codes))
	}
	codes = awardTypeCodes("grant")
	if len(codes) != 4 {
		t.Errorf("grant codes len = %d, want 4", len(codes))
	}
	// default falls to contract
	codes = awardTypeCodes("unknown")
	if len(codes) != 4 || codes[0] != "A" {
		t.Errorf("default codes = %v, want contract codes", codes)
	}
}
