package research

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

// Tier ranks source reliability; 1 is strongest.
//
//	1 government, regulators, filings, official docs, academic papers, standards
//	2 major financial publications, established research organisations
//	3 specialist publications, industry blogs, analyst commentary
//	4 forums, social media, anonymous sources, aggregators
type Tier int

func (t Tier) Label() string {
	switch t {
	case 1:
		return "Tier 1 · Primary / official"
	case 2:
		return "Tier 2 · Authoritative secondary"
	case 3:
		return "Tier 3 · Specialist / analyst"
	case 4:
		return "Tier 4 · Community / unverified"
	}
	return "Unrated"
}

// Source is a document that evidence was drawn from.
type Source struct {
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Publisher   string     `json:"publisher,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	AccessedAt  time.Time  `json:"accessed_at"`
	SourceType  string     `json:"source_type,omitempty"`
	Tier        Tier       `json:"tier"`
	// FoundBy lists the tasks that cited this source.
	FoundBy []string `json:"found_by,omitempty"`
}

// NormalizeURL canonicalises a URL for de-duplication: lower-case host without
// "www.", no fragment, no tracking parameters, no trailing slash.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimRight(raw, "/"))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme == "http" {
		u.Scheme = "https"
	}
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "ref" || lk == "fbclid" || lk == "gclid" || lk == "mc_cid" || lk == "mc_eid" {
			q.Del(k)
		}
	}
	u.RawQuery = encodeSorted(q)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func encodeSorted(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// Host returns the URL host without "www.".
func Host(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

var tier1Suffixes = []string{
	".gov", ".mil", ".gov.uk", ".gov.in", ".gc.ca", ".gov.au", ".europa.eu", ".edu", ".ac.uk", ".int",
}

var tier1Hosts = []string{
	"sec.gov", "arxiv.org", "doi.org", "ietf.org", "rfc-editor.org", "iso.org", "w3.org", "nist.gov",
	"oecd.org", "imf.org", "worldbank.org", "bis.org", "federalreserve.gov", "ecb.europa.eu",
	"kubernetes.io", "cncf.io", "nature.com", "science.org", "acm.org", "ieee.org", "usenix.org",
}

var tier4Hosts = []string{
	"reddit.com", "x.com", "twitter.com", "news.ycombinator.com", "quora.com", "facebook.com",
	"tiktok.com", "instagram.com", "stackoverflow.com", "stackexchange.com", "discord.com", "t.me",
}

// Self-publishing platforms host both good and bad writing; never above tier 3.
var tier3CapHosts = []string{"medium.com", "substack.com", "youtube.com", "linkedin.com", "dev.to", "hashnode.dev"}

func hostMatches(host string, list []string) bool {
	for _, h := range list {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// DomainTier returns a tier inferred from the host, or 0 when unknown.
func DomainTier(host string) Tier {
	if hostMatches(host, tier4Hosts) {
		return 4
	}
	if hostMatches(host, tier3CapHosts) {
		return 3
	}
	if hostMatches(host, tier1Hosts) {
		return 1
	}
	for _, s := range tier1Suffixes {
		if strings.HasSuffix(host, s) {
			return 1
		}
	}
	return 0
}

// ResolveTier combines the agent's claimed tier with domain knowledge. Domain
// rules can only lower trust for known community sites and raise it for known
// official hosts; an agent cannot promote a forum post to tier 1.
func ResolveTier(claimed Tier, rawURL string) Tier {
	if claimed < 1 || claimed > 4 {
		claimed = 3
	}
	switch DomainTier(Host(rawURL)) {
	case 4:
		return 4
	case 3:
		return max(claimed, 3)
	case 1:
		return 1
	}
	return claimed
}
