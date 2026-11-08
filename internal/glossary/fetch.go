package glossary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pelokalan/internal/netsafe"
)

// ErrBlockedAddress is returned when a URL resolves to a private or local address
var ErrBlockedAddress = netsafe.ErrBlockedAddress

// ErrNotModified is returned by Fetch when the server reports no change
var ErrNotModified = errors.New("not modified")

// FetchResult is a downloaded file
type FetchResult struct {
	Data         []byte
	Filename     string
	ETag         string
	LastModified string
}

// Fetcher downloads glossary sources over HTTP(S)
type Fetcher struct {
	client    *http.Client
	maxBytes  int64
	userAgent string
}

// NewFetcher creates a fetcher. Unless allowPrivate is set, connections to
// loopback, private, link-local and other non-public addresses are refused
// (checked at dial time, so redirects and DNS tricks are covered too).
func NewFetcher(maxBytes int64, allowPrivate bool, userAgent string) *Fetcher {
	transport := netsafe.Transport(allowPrivate)
	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return errors.New("redirect to unsupported scheme")
				}
				return nil
			},
		},
		maxBytes:  maxBytes,
		userAgent: userAgent,
	}
}

// ValidateURL checks that a URL can be used as a download source
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(raw) > 2000 {
		return nil, errors.New("invalid URL")
	}
	return u, nil
}

// Fetch downloads the URL. When etag or lastModified are given, the request is
// conditional and ErrNotModified is returned if the server reports no change.
func (f *Fetcher) Fetch(ctx context.Context, rawURL, etag, lastModified string) (*FetchResult, error) {
	u, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > f.maxBytes {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > f.maxBytes {
		return nil, ErrTooLarge
	}
	name := resp.Request.URL.Path
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = "download"
	}
	return &FetchResult{
		Data:         data,
		Filename:     name,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}
