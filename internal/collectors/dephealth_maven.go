// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// mavenSearchBaseURL is the Maven Central search URL, used only as a
// fallback: search.maven.org/solrsearch answers in ~20s per query and
// throttles concurrent clients (stringer-jfh.3).
const mavenSearchBaseURL = "https://search.maven.org/solrsearch"

// mavenMetadataBaseURL is the CDN-backed Maven Central repository root. Each
// artifact's maven-metadata.xml under it lists every version and the last
// deployment time and answers in well under a second.
const mavenMetadataBaseURL = "https://repo1.maven.org/maven2"

// mavenLastUpdatedLayout is the <lastUpdated> format in maven-metadata.xml
// (yyyyMMddHHmmss, UTC).
const mavenLastUpdatedLayout = "20060102150405"

// mavenRegistryClient fetches package metadata from Maven Central.
type mavenRegistryClient interface {
	FetchArtifact(ctx context.Context, groupID, artifactID string) (*mavenArtifactInfo, error)
}

// mavenArtifactInfo represents the subset of Maven Central search response we need.
// The metadata path synthesises the same shape so checkMavenDeps has one contract.
type mavenArtifactInfo struct {
	Response struct {
		NumFound int             `json:"numFound"`
		Docs     []mavenArtifact `json:"docs"`
	} `json:"response"`
}

// mavenArtifact represents a single artifact from Maven Central search results.
type mavenArtifact struct {
	GroupID    string `json:"g"`
	ArtifactID string `json:"a"`
	Version    string `json:"latestVersion"`
	Timestamp  int64  `json:"timestamp"` // millis since epoch
}

// mavenMetadata is the subset of maven-metadata.xml we read.
type mavenMetadata struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Versioning struct {
		Latest      string   `xml:"latest"`
		Release     string   `xml:"release"`
		Versions    []string `xml:"versions>version"`
		LastUpdated string   `xml:"lastUpdated"`
	} `xml:"versioning"`
}

// empty reports whether the metadata carries no versioning information at
// all, in which case the search API is consulted instead.
func (m *mavenMetadata) empty() bool {
	v := m.Versioning
	return v.Latest == "" && v.Release == "" && len(v.Versions) == 0 && v.LastUpdated == ""
}

// latestVersion picks the newest published version: <release> first, then a
// non-SNAPSHOT <latest>, then the last non-SNAPSHOT entry in <versions>, and
// only when nothing else exists a SNAPSHOT.
func (m *mavenMetadata) latestVersion() string {
	v := m.Versioning
	if v.Release != "" {
		return v.Release
	}
	if v.Latest != "" && !isMavenSnapshot(v.Latest) {
		return v.Latest
	}
	for i := len(v.Versions) - 1; i >= 0; i-- {
		if !isMavenSnapshot(v.Versions[i]) {
			return v.Versions[i]
		}
	}
	if v.Latest != "" {
		return v.Latest
	}
	if n := len(v.Versions); n > 0 {
		return v.Versions[n-1]
	}
	return ""
}

func isMavenSnapshot(version string) bool {
	return strings.HasSuffix(strings.ToUpper(version), "-SNAPSHOT")
}

// errMavenMetadataUnusable marks a metadata response that should be retried
// through the search API: a 404 (artifact not on Central under that path),
// malformed XML, or a document without a versioning block.
var errMavenMetadataUnusable = errors.New("maven metadata unusable")

// realMavenRegistryClient queries Maven Central: the repository's
// maven-metadata.xml first, the search API only as a fallback.
type realMavenRegistryClient struct {
	httpClient *http.Client
	baseURL    string // search API root; mavenSearchBaseURL when empty
	metaURL    string // repository root; mavenMetadataBaseURL when empty
}

// FetchArtifact returns an artifact's latest version and last-updated time.
// Any error other than an unusable metadata document (network failure,
// timeout, 5xx) is returned as-is so a slow registry is not queried twice
// within one lookup deadline.
func (c *realMavenRegistryClient) FetchArtifact(ctx context.Context, groupID, artifactID string) (*mavenArtifactInfo, error) {
	info, err := c.fetchMetadata(ctx, groupID, artifactID)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, errMavenMetadataUnusable) {
		return nil, err
	}
	return c.fetchSearch(ctx, groupID, artifactID)
}

// fetchMetadata reads <root>/<group path>/<artifact>/maven-metadata.xml.
func (c *realMavenRegistryClient) fetchMetadata(ctx context.Context, groupID, artifactID string) (*mavenArtifactInfo, error) {
	base := c.metaURL
	if base == "" {
		base = mavenMetadataBaseURL
	}
	segments := strings.Split(groupID, ".")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	u := fmt.Sprintf("%s/%s/%s/maven-metadata.xml", base, strings.Join(segments, "/"), url.PathEscape(artifactID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := doRegistryRequest(registryHTTPClient(c.httpClient), req, "maven metadata", groupID+":"+artifactID)
	if err != nil {
		if registryStatus(err) == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %w at %s", errMavenMetadataUnusable, err, base)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var meta mavenMetadata
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxRegistryResponseBytes)).Decode(&meta); err != nil {
		return nil, fmt.Errorf("%w: decoding metadata for %s:%s: %w", errMavenMetadataUnusable, groupID, artifactID, err)
	}
	if meta.empty() {
		return nil, fmt.Errorf("%w: no versioning for %s:%s", errMavenMetadataUnusable, groupID, artifactID)
	}

	doc := mavenArtifact{GroupID: groupID, ArtifactID: artifactID, Version: meta.latestVersion()}
	if meta.Versioning.LastUpdated != "" {
		t, err := time.ParseInLocation(mavenLastUpdatedLayout, strings.TrimSpace(meta.Versioning.LastUpdated), time.UTC)
		if err != nil {
			return nil, fmt.Errorf("%w: lastUpdated %q for %s:%s: %w", errMavenMetadataUnusable, meta.Versioning.LastUpdated, groupID, artifactID, err)
		}
		doc.Timestamp = t.UnixMilli()
	}

	info := &mavenArtifactInfo{}
	info.Response.NumFound = 1
	info.Response.Docs = []mavenArtifact{doc}
	return info, nil
}

// fetchSearch queries the Maven Central search API for an artifact's metadata.
func (c *realMavenRegistryClient) fetchSearch(ctx context.Context, groupID, artifactID string) (*mavenArtifactInfo, error) {
	base := c.baseURL
	if base == "" {
		base = mavenSearchBaseURL
	}
	u := fmt.Sprintf("%s/select?q=g:%%22%s%%22+AND+a:%%22%s%%22&rows=1&wt=json", base, groupID, artifactID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := doRegistryRequest(registryHTTPClient(c.httpClient), req, "maven central", groupID+":"+artifactID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var info mavenArtifactInfo
	if err := decodeJSONLimited(resp.Body, &info); err != nil {
		return nil, fmt.Errorf("decoding maven response for %s:%s: %w", groupID, artifactID, err)
	}

	return &info, nil
}

// mavenStalenessThreshold is 4 years — artifacts with no new release beyond
// this are flagged as potentially unmaintained.
const mavenStalenessThreshold = 4 * 365 * 24 * time.Hour

// checkMavenDeps queries Maven Central for each dependency (bounded-parallel,
// capped at maxRegistryChecks) and emits signals for artifacts that have not
// been updated in a long time (potentially abandoned).
func (r *registryRun) checkMavenDeps(ctx context.Context, client mavenRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	return lookupEach(ctx, r, "maven", deps, func(ctx context.Context, dep PackageQuery) []signal.RawSignal {
		// Split groupId:artifactId.
		parts := strings.SplitN(dep.Name, ":", 2)
		if len(parts) != 2 {
			return nil
		}
		groupID, artifactID := parts[0], parts[1]

		info, err := client.FetchArtifact(ctx, groupID, artifactID)
		if err != nil {
			r.lookupFailed("maven", dep.Name, err)
			return nil
		}

		if info.Response.NumFound == 0 || len(info.Response.Docs) == 0 {
			return nil
		}

		doc := info.Response.Docs[0]
		if doc.Timestamp <= 0 {
			return nil
		}
		lastUpdated := time.UnixMilli(doc.Timestamp)
		if time.Since(lastUpdated) <= mavenStalenessThreshold {
			return nil
		}
		return []signal.RawSignal{{
			Source:      "dephealth",
			Kind:        "stale-dependency",
			FilePath:    filePath,
			Title:       fmt.Sprintf("Stale Maven artifact: %s", dep.Name),
			Description: fmt.Sprintf("Maven artifact %s was last updated on %s (>%d years ago). The project may be unmaintained.", dep.Name, lastUpdated.Format("2006-01-02"), int(mavenStalenessThreshold.Hours()/24/365)),
			Confidence:  0.5,
			Tags:        []string{"stale-dependency", "dephealth", "maven"},
		}}
	})
}
