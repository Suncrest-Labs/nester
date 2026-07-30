package webhookssrf

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeResolver struct {
	addrs map[string][]net.IPAddr
	err   error
}

func (f fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.addrs[host], nil
}

func TestValidate_RejectsNonHTTPS(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "http://example.com/hook")
	if !errors.Is(err, ErrNotHTTPS) {
		t.Fatalf("got %v, want ErrNotHTTPS", err)
	}
}

func TestValidate_RejectsMalformedURL(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "://not-a-url")
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("got %v, want ErrInvalidURL", err)
	}
}

func TestValidate_RejectsLiteralPrivateIP(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "https://10.0.0.5/hook")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost", err)
	}
}

func TestValidate_RejectsCloudMetadataAddress(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "https://169.254.169.254/latest/meta-data")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost for cloud metadata address", err)
	}
}

func TestValidate_RejectsLoopback(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "https://127.0.0.1:8080/hook")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost", err)
	}
}

func TestValidate_AllowsPublicLiteralIP(t *testing.T) {
	if err := Validate(context.Background(), fakeResolver{}, "https://93.184.216.34/hook"); err != nil {
		t.Fatalf("unexpected error for public IP: %v", err)
	}
}

func TestValidate_AllowsHostnameResolvingPublic(t *testing.T) {
	resolver := fakeResolver{addrs: map[string][]net.IPAddr{
		"partner.example.com": {{IP: net.ParseIP("93.184.216.34")}},
	}}
	if err := Validate(context.Background(), resolver, "https://partner.example.com/hook"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RejectsHostnameResolvingPrivate_DNSRebindScenario(t *testing.T) {
	// Simulates a hostname that, at send time, now resolves to an internal
	// address — the exact DNS-rebinding scenario the issue calls out.
	resolver := fakeResolver{addrs: map[string][]net.IPAddr{
		"rebind.example.com": {{IP: net.ParseIP("10.0.0.1")}},
	}}
	err := Validate(context.Background(), resolver, "https://rebind.example.com/hook")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost", err)
	}
}

func TestValidate_RejectsIfAnyResolvedAddressIsDisallowed(t *testing.T) {
	resolver := fakeResolver{addrs: map[string][]net.IPAddr{
		"multi.example.com": {
			{IP: net.ParseIP("93.184.216.34")},
			{IP: net.ParseIP("192.168.1.1")},
		},
	}}
	err := Validate(context.Background(), resolver, "https://multi.example.com/hook")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost when any resolved address is private", err)
	}
}

func TestValidate_RejectsResolveFailure(t *testing.T) {
	resolver := fakeResolver{err: errors.New("no such host")}
	err := Validate(context.Background(), resolver, "https://nonexistent.example.com/hook")
	if !errors.Is(err, ErrResolveFailed) {
		t.Fatalf("got %v, want ErrResolveFailed", err)
	}
}

func TestValidate_RejectsMissingHost(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "https:///hook")
	if !errors.Is(err, ErrNoHost) {
		t.Fatalf("got %v, want ErrNoHost", err)
	}
}

func TestValidate_RejectsIPv6Loopback(t *testing.T) {
	err := Validate(context.Background(), fakeResolver{}, "https://[::1]/hook")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost", err)
	}
}

func TestValidate_RejectsIPv6UniqueLocal(t *testing.T) {
	// fd00::/8 is RFC4193 unique local — the IPv6 analogue of RFC1918.
	err := Validate(context.Background(), fakeResolver{}, "https://[fd00::1]/hook")
	if !errors.Is(err, ErrDisallowedHost) {
		t.Fatalf("got %v, want ErrDisallowedHost", err)
	}
}
