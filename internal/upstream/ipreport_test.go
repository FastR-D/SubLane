package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLookupIPReportBoundsThirdPartyFields(t *testing.T) {
	body := `{"ip":"203.0.113.9","trust_score":86.6,"company_type":"isp","isResidential":true,"is_datacenter":false,"is_mobile":false,"is_vpn":false,"is_proxy":true,"is_tor":false,"is_abuser":false,"is_crawler":false,"asn":64500,"asOrganization":"Synthetic ISP","isp":"Synthetic ISP","countryCode":"jp","country":"Japan","region":"Tokyo","city":"Tokyo","rdns":"synthetic.example","ai_verdict":{"label":"住宅宽带","confidence":90},"intelligence":{"threats":["spam",{"name":"scanner"}],"abuser_level":"safe"}}`
	calls := 0
	previous := IPReportClient
	IPReportClient = &http.Client{Transport: usageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != IPReportURL+"203.0.113.9" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected lookup request", r.URL.String())
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	t.Cleanup(func() { IPReportClient = previous })
	report, err := LookupIPReport(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if report.TrustScore == nil || *report.TrustScore != 87 || !report.Residential || !report.Proxy || report.CountryCode != "JP" || report.ASN != 64500 || report.Verdict != "住宅宽带" {
		t.Fatalf("report: %+v", report)
	}
	for _, private := range []string{"10.0.0.1", "127.0.0.1", "", "not-an-ip", "::1"} {
		if _, err := LookupIPReport(context.Background(), private); !errors.Is(err, ErrInput) {
			t.Fatal("non-public address was looked up", private, err)
		}
	}
	body = `{"ip":"198.51.100.1","trust_score":90}`
	if _, err := LookupIPReport(context.Background(), "203.0.113.9"); !errors.Is(err, ErrResponse) {
		t.Fatal("mismatched report accepted", err)
	}
	body = `{"ip":"203.0.113.9","trust_score":""}`
	if report, err := LookupIPReport(context.Background(), "203.0.113.9"); err != nil || report.TrustScore != nil {
		t.Fatal("missing score must stay unknown", report, err)
	}
	if calls != 3 {
		t.Fatal("non-public addresses reached the service", calls)
	}
}
