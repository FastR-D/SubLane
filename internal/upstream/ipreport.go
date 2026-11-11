package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// IPReportURL is the ip.net.coffee lookup behind its IP scoring page. The exit IP is the only
// value sent; it never carries proxy credentials or account metadata.
var IPReportURL = "https://ip.net.coffee/api/ip/lookup/"

// IPReportClient is replaceable at test seams.
var IPReportClient = &http.Client{Timeout: 15 * time.Second}

// IPReport is a bounded subset of the third-party lookup. Unknown values stay empty or nil.
type IPReport struct {
	IP             string `json:"ip"`
	TrustScore     *int64 `json:"trust_score"`
	CompanyType    string `json:"company_type"`
	Residential    bool   `json:"residential"`
	Datacenter     bool   `json:"datacenter"`
	Mobile         bool   `json:"mobile"`
	VPN            bool   `json:"vpn"`
	Proxy          bool   `json:"proxy"`
	Tor            bool   `json:"tor"`
	Abuser         bool   `json:"abuser"`
	Crawler        bool   `json:"crawler"`
	ASN            int64  `json:"asn"`
	ASOrganization string `json:"as_organization"`
	ISP            string `json:"isp"`
	CountryCode    string `json:"country_code"`
	Country        string `json:"country"`
	Region         string `json:"region"`
	City           string `json:"city"`
	Verdict        string `json:"verdict"`
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return ""
	}
	if utf8.RuneCountInString(value) > limit {
		value = string([]rune(value)[:limit])
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
}

// LookupIPReport queries the reputation service for a public exit IP.
func LookupIPReport(ctx context.Context, ip string) (IPReport, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(ip))
	// Private, loopback and unspecified addresses carry no public reputation and must not leave the host.
	if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() {
		return IPReport{}, ErrInput
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, IPReportURL+url.PathEscape(address.String()), nil)
	if err != nil {
		return IPReport{}, ErrInput
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "SubLane")
	response, err := IPReportClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return IPReport{}, ctx.Err()
		}
		return IPReport{}, ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return IPReport{}, &UpstreamError{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	raw, err := readBounded(response.Body, 1<<20)
	if err != nil {
		return IPReport{}, ErrResponse
	}
	var payload struct {
		IP          string          `json:"ip"`
		TrustScore  json.RawMessage `json:"trust_score"`
		CompanyType string          `json:"company_type"`
		Residential bool            `json:"isResidential"`
		Datacenter  bool            `json:"is_datacenter"`
		Mobile      bool            `json:"is_mobile"`
		VPN         bool            `json:"is_vpn"`
		Proxy       bool            `json:"is_proxy"`
		Tor         bool            `json:"is_tor"`
		Abuser      bool            `json:"is_abuser"`
		Crawler     bool            `json:"is_crawler"`
		ASN         int64           `json:"asn"`
		ASOrg       string          `json:"asOrganization"`
		ISP         string          `json:"isp"`
		CountryCode string          `json:"countryCode"`
		Country     string          `json:"country"`
		Region      string          `json:"region"`
		City        string          `json:"city"`
		Verdict     *struct {
			Label string `json:"label"`
		} `json:"ai_verdict"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return IPReport{}, ErrResponse
	}
	if returned, err := netip.ParseAddr(payload.IP); err != nil || returned != address {
		return IPReport{}, ErrResponse
	}
	report := IPReport{IP: address.String(), CompanyType: boundedText(payload.CompanyType, 32), Residential: payload.Residential, Datacenter: payload.Datacenter, Mobile: payload.Mobile, VPN: payload.VPN, Proxy: payload.Proxy, Tor: payload.Tor, Abuser: payload.Abuser, Crawler: payload.Crawler,
		ASOrganization: boundedText(payload.ASOrg, 128), ISP: boundedText(payload.ISP, 128), CountryCode: strings.ToUpper(boundedText(payload.CountryCode, 2)), Country: boundedText(payload.Country, 64),
		Region: boundedText(payload.Region, 64), City: boundedText(payload.City, 64)}
	if payload.ASN > 0 && payload.ASN < 1<<32 {
		report.ASN = payload.ASN
	}
	var score float64
	// A missing or empty score stays unknown; it is never shown as zero.
	if json.Unmarshal(payload.TrustScore, &score) == nil && score >= 0 && score <= 100 {
		value := int64(score + 0.5)
		report.TrustScore = &value
	}
	if payload.Verdict != nil {
		report.Verdict = boundedText(payload.Verdict.Label, 48)
	}
	return report, nil
}
