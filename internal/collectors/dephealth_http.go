// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRegistryResponseBytes caps the size of a dependency-registry JSON
// response body. A malicious or misconfigured registry could otherwise serve
// an arbitrarily large payload and exhaust memory — the per-ecosystem check
// caps mean up to ~50 such responses could be in flight over a scan.
const maxRegistryResponseBytes = 10 << 20 // 10 MiB

// registryRetryBackoff is the wait before the single retry of a 429 without
// a usable Retry-After header, or of a 5xx. Tests shorten it.
var registryRetryBackoff = time.Second

// registryStatusError is a non-200 registry response. lookupFailed classifies
// failures by Status (429 rate limited, 404 not found, 5xx server error).
type registryStatusError struct {
	Registry string // e.g. "npm registry", "maven metadata"
	Status   int
	Subject  string // package or artifact the lookup was for
}

func (e *registryStatusError) Error() string {
	return fmt.Sprintf("%s returned %d for %s", e.Registry, e.Status, e.Subject)
}

// registryStatus returns the HTTP status behind a lookup error, or 0 when
// the error is not a registryStatusError.
func registryStatus(err error) int {
	var se *registryStatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

// doRegistryRequest performs a registry GET and returns the response only on
// HTTP 200. A 429 or 5xx is retried exactly once (stringer-jfh.6): a 429
// waits for its Retry-After header when present, capped at the client
// timeout, otherwise registryRetryBackoff; a 5xx waits registryRetryBackoff.
// No other status is retried, and no retry starts when the request context
// would expire before the wait ends. Non-200 results are returned as a
// *registryStatusError; transport errors are wrapped with the request URL.
func doRegistryRequest(hc *http.Client, req *http.Request, registry, subject string) (*http.Response, error) {
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		resp, err := hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", req.URL, err)
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		serr := &registryStatusError{Registry: registry, Status: resp.StatusCode, Subject: subject}
		wait, retry := registryRetryWait(resp, hc.Timeout)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if !retry || attempt > 0 {
			return nil, serr
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= wait {
			return nil, serr // the retry could not finish inside the lookup deadline
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ctx.Err()
			}
			return nil, serr
		}
		req = req.Clone(ctx)
	}
}

// registryRetryWait decides whether a non-200 response is retried and how
// long to wait first. capAt bounds a Retry-After value (0 means the default
// registry timeout).
func registryRetryWait(resp *http.Response, capAt time.Duration) (time.Duration, bool) {
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		if capAt <= 0 {
			capAt = defaultRegistryTimeout
		}
		wait := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		if wait <= 0 {
			wait = registryRetryBackoff
		}
		return min(wait, capAt), true
	case resp.StatusCode >= 500:
		return registryRetryBackoff, true
	}
	return 0, false
}

// parseRetryAfter reads a Retry-After header given either as delay-seconds
// or as an HTTP-date. It returns 0 for an absent, malformed or past value.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return max(time.Duration(secs)*time.Second, 0)
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

// decodeJSONLimited decodes JSON from body into v, reading at most
// maxRegistryResponseBytes. If the body exceeds the cap the decode fails with
// an unexpected-EOF error rather than allocating without bound.
func decodeJSONLimited(body io.Reader, v any) error {
	return json.NewDecoder(io.LimitReader(body, maxRegistryResponseBytes)).Decode(v)
}
