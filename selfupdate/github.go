package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Repo is the fixed release source. Making it configurable would grant anyone
// who can edit configuration a remote-code-execution path, which is unacceptable here.
const Repo = "Autumn-27/ARTEX"

// latestURL is GitHub's latest stable release endpoint; prereleases and drafts are skipped.
const latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// allowedHosts restricts update destinations. Combined with CheckRedirect,
// any redirect hop to an unlisted host fails. This is the first defense against
// substituted binaries through DNS/MITM attacks; SHA256SUMS is the second gate.
var allowedHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true, // Object storage hosting release assets.
	"release-assets.githubusercontent.com": true,
	"raw.githubusercontent.com":            true,
}

// Release contains the GitHub release fields needed by the updater.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset describes one attached release file.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// NewClient accepts only GitHub hosts. An empty proxy uses a direct connection.
//
// Do not reuse the default transport: updates must require TLS and certificate
// verification regardless of InsecureSkipVerify settings elsewhere.
func NewClient(proxy string) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Minute, // Allow a full package download, not just a short API request.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return locale.NewError("Too many redirects")
			}
			return checkURL(req.URL)
		},
	}
}

// checkURL requires HTTPS and an allowlisted hostname.
func checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return locale.Errorf("Non-HTTPS URL refused: %s", u.Scheme+"://"+u.Host)
	}
	if !allowedHosts[strings.ToLower(u.Hostname())] {
		return locale.Errorf("Non-GitHub host refused: %s", u.Hostname())
	}
	return nil
}

// FetchLatest fetches the latest stable release.
func FetchLatest(ctx context.Context, c *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "artex-selfupdate")

	resp, err := c.Do(req)
	if err != nil {
		return nil, locale.Errorf("GitHub request failed (configure the global proxy in settings if needed): %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		// Unauthenticated GitHub API access allows 60 requests/hour/IP; shared egress reaches this easily.
		return nil, locale.NewError("GitHub API rate limit reached (60 requests/hour); try again later")
	case resp.StatusCode == http.StatusNotFound:
		return nil, locale.Errorf("Repository %s has no stable releases yet", Repo)
	case resp.StatusCode != http.StatusOK:
		return nil, locale.Errorf("GitHub returned %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, locale.Errorf("Parse release: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return nil, locale.NewError("Release is missing a tag")
	}
	return &rel, nil
}

// AssetName returns the platform package name, matching build.sh package_binary:
// artex-<version>-<os>-<arch>.zip, with the leading v removed from the version.
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("artex-%s-%s-%s.zip", strings.TrimPrefix(tag, "v"), goos, goarch)
}

// FindAsset looks up a release asset by name.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Asset{}, false
}
