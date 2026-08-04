package pixhost

import "testing"

func TestNormalizeDomainAllowsSupportedDomains(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty defaults to pixhost.to", value: " ", want: DefaultDomain},
		{name: "pixhost.to", value: " pixhost.to ", want: DefaultDomain},
		{name: "pixhost.cc", value: "PIXHOST.CC", want: AlternateDomain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeDomain(tt.value)
			if err != nil {
				t.Fatalf("NormalizeDomain returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeDomain(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestNormalizeDomainRejectsUnsupportedDomain(t *testing.T) {
	if _, err := NormalizeDomain("pixhost.example"); err == nil {
		t.Fatal("expected unsupported domain error")
	}
}

func TestEndpointUsesSelectedDomain(t *testing.T) {
	t.Setenv("PIXHOST_API_URL", "https://example.invalid/images")

	got, err := endpoint(UploadOptions{Domain: AlternateDomain})
	if err != nil {
		t.Fatalf("endpoint returned error: %v", err)
	}
	if got != "https://api.pixhost.cc/images" {
		t.Fatalf("endpoint = %q, want pixhost.cc API URL", got)
	}
}

func TestEndpointUsesEnvironmentOverrideWhenDomainEmpty(t *testing.T) {
	t.Setenv("PIXHOST_API_URL", "https://example.invalid/images")

	got, err := endpoint(UploadOptions{})
	if err != nil {
		t.Fatalf("endpoint returned error: %v", err)
	}
	if got != "https://example.invalid/images" {
		t.Fatalf("endpoint = %q, want environment override", got)
	}
}

func TestEndpointUsesEnvironmentOverrideWhenDefaultDomainSelected(t *testing.T) {
	t.Setenv("PIXHOST_API_URL", "https://example.invalid/images")

	got, err := endpoint(UploadOptions{Domain: DefaultDomain})
	if err != nil {
		t.Fatalf("endpoint returned error: %v", err)
	}
	if got != "https://example.invalid/images" {
		t.Fatalf("endpoint = %q, want environment override", got)
	}
}

func TestNormalizeDirectURLSupportsPixhostCC(t *testing.T) {
	got, err := normalizeDirectURL("https://t12.pixhost.cc/thumbs/456/shot.png")
	if err != nil {
		t.Fatalf("normalizeDirectURL returned error: %v", err)
	}
	if got != "https://img12.pixhost.cc/images/456/shot.png" {
		t.Fatalf("normalizeDirectURL = %q, want pixhost.cc direct URL", got)
	}
}
