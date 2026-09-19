// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mavenTestServer serves maven-metadata.xml at the repository path and a
// solrsearch JSON document at /select, counting hits on each so tests can
// assert which endpoint answered.
type mavenTestServer struct {
	srv          *httptest.Server
	metaStatus   int
	metaBody     string
	searchStatus int
	searchBody   string
	metaHits     atomic.Int32
	searchHits   atomic.Int32
	metaPath     string
}

func newMavenTestServer(t *testing.T) *mavenTestServer {
	t.Helper()
	m := &mavenTestServer{metaStatus: http.StatusOK, searchStatus: http.StatusOK}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/select" {
			m.searchHits.Add(1)
			w.WriteHeader(m.searchStatus)
			_, _ = w.Write([]byte(m.searchBody))
			return
		}
		m.metaHits.Add(1)
		m.metaPath = r.URL.EscapedPath()
		w.WriteHeader(m.metaStatus)
		_, _ = w.Write([]byte(m.metaBody))
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mavenTestServer) client() *realMavenRegistryClient {
	return &realMavenRegistryClient{httpClient: &http.Client{Timeout: 2 * time.Second}, baseURL: m.srv.URL, metaURL: m.srv.URL}
}

const joptMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>net.sf.jopt-simple</groupId>
  <artifactId>jopt-simple</artifactId>
  <versioning>
    <latest>6.0-alpha-3</latest>
    <release>5.0.4</release>
    <versions>
      <version>4.9</version>
      <version>5.0.4</version>
      <version>6.0-alpha-3</version>
    </versions>
    <lastUpdated>20180117210345</lastUpdated>
  </versioning>
</metadata>`

func TestRealMavenClient_MetadataHappyPath(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = joptMetadata

	info, err := m.client().FetchArtifact(context.Background(), "net.sf.jopt-simple", "jopt-simple")
	require.NoError(t, err)
	assert.Equal(t, "/net/sf/jopt-simple/jopt-simple/maven-metadata.xml", m.metaPath)
	assert.Equal(t, int32(1), m.metaHits.Load())
	assert.Equal(t, int32(0), m.searchHits.Load(), "search API must not be consulted when metadata is usable")

	require.Equal(t, 1, info.Response.NumFound)
	require.Len(t, info.Response.Docs, 1)
	doc := info.Response.Docs[0]
	assert.Equal(t, "net.sf.jopt-simple", doc.GroupID)
	assert.Equal(t, "jopt-simple", doc.ArtifactID)
	assert.Equal(t, "5.0.4", doc.Version, "<release> wins over <latest>")
	want := time.Date(2018, 1, 17, 21, 3, 45, 0, time.UTC).UnixMilli()
	assert.Equal(t, want, doc.Timestamp)
}

func TestRealMavenClient_MetadataDrivesStaleSignal(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = joptMetadata

	deps := []PackageQuery{{Ecosystem: "Maven", Name: "net.sf.jopt-simple:jopt-simple", Version: "5.0.4"}}
	signals := testRun().checkMavenDeps(context.Background(), m.client(), deps, "build.gradle")
	require.Len(t, signals, 1)
	assert.Equal(t, "Stale Maven artifact: net.sf.jopt-simple:jopt-simple", signals[0].Title)
	assert.Contains(t, signals[0].Description, "2018-01-17")
}

func TestRealMavenClient_MetadataEscapesPath(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = joptMetadata

	_, err := m.client().FetchArtifact(context.Background(), "com.example", "odd artifact")
	require.NoError(t, err)
	assert.Equal(t, "/com/example/odd%20artifact/maven-metadata.xml", m.metaPath)
}

func TestMavenMetadata_LatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		release  string
		latest   string
		versions []string
		want     string
	}{
		{"release preferred", "2.0", "3.0-SNAPSHOT", []string{"1.0", "2.0", "3.0-SNAPSHOT"}, "2.0"},
		{"latest when no release", "", "2.1", []string{"2.0", "2.1"}, "2.1"},
		{"snapshot latest skipped for last stable version", "", "3.0-SNAPSHOT", []string{"1.0", "2.0", "3.0-SNAPSHOT"}, "2.0"},
		{"snapshot only falls back to latest", "", "1.0-SNAPSHOT", []string{"1.0-SNAPSHOT"}, "1.0-SNAPSHOT"},
		{"snapshot only without latest uses last version", "", "", []string{"0.9-snapshot", "1.0-SNAPSHOT"}, "1.0-SNAPSHOT"},
		{"nothing", "", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var meta mavenMetadata
			meta.Versioning.Release = tt.release
			meta.Versioning.Latest = tt.latest
			meta.Versioning.Versions = tt.versions
			assert.Equal(t, tt.want, meta.latestVersion())
		})
	}
}

func TestRealMavenClient_SnapshotOnlyMetadata(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = `<metadata><groupId>g</groupId><artifactId>a</artifactId><versioning>
  <latest>1.0-SNAPSHOT</latest>
  <versions><version>1.0-SNAPSHOT</version></versions>
  <lastUpdated>20240301120000</lastUpdated>
</versioning></metadata>`

	info, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.NoError(t, err)
	require.Len(t, info.Response.Docs, 1)
	assert.Equal(t, "1.0-SNAPSHOT", info.Response.Docs[0].Version)
	assert.Equal(t, time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC).UnixMilli(), info.Response.Docs[0].Timestamp)
	assert.Equal(t, int32(0), m.searchHits.Load())
}

func TestRealMavenClient_MetadataWithoutLastUpdated(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = `<metadata><versioning><release>1.2</release></versioning></metadata>`

	info, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.NoError(t, err)
	require.Len(t, info.Response.Docs, 1)
	assert.Equal(t, "1.2", info.Response.Docs[0].Version)
	assert.Zero(t, info.Response.Docs[0].Timestamp)

	// No timestamp means no staleness verdict, never a false positive.
	deps := []PackageQuery{{Ecosystem: "Maven", Name: "g:a", Version: "1.2"}}
	assert.Empty(t, testRun().checkMavenDeps(context.Background(), m.client(), deps, "pom.xml"))
}

const searchStaleBody = `{"response":{"numFound":1,"docs":[{"g":"g","a":"a","latestVersion":"1.0","timestamp":1300000000000}]}}`

func TestRealMavenClient_NotFoundFallsBackToSearch(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaStatus = http.StatusNotFound
	m.searchBody = searchStaleBody

	info, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.metaHits.Load())
	assert.Equal(t, int32(1), m.searchHits.Load())
	require.Len(t, info.Response.Docs, 1)
	assert.Equal(t, int64(1300000000000), info.Response.Docs[0].Timestamp)
}

func TestRealMavenClient_MalformedMetadataFallsBackToSearch(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = `<metadata><versioning><release>1.0</release>`
	m.searchBody = searchStaleBody

	info, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.searchHits.Load())
	assert.Equal(t, "1.0", info.Response.Docs[0].Version)
}

func TestRealMavenClient_NoVersioningFallsBackToSearch(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = `<metadata><groupId>g</groupId><artifactId>a</artifactId></metadata>`
	m.searchBody = searchStaleBody

	_, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.searchHits.Load())
}

func TestRealMavenClient_BadLastUpdatedFallsBackToSearch(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaBody = `<metadata><versioning><release>1.0</release><lastUpdated>yesterday</lastUpdated></versioning></metadata>`
	m.searchBody = searchStaleBody

	info, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.searchHits.Load())
	assert.Equal(t, int64(1300000000000), info.Response.Docs[0].Timestamp)
}

func TestRealMavenClient_ServerErrorDoesNotFallBack(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaStatus = http.StatusServiceUnavailable
	m.searchBody = searchStaleBody

	_, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Equal(t, int32(0), m.searchHits.Load(), "a registry outage must not double the lookup time")
}

func TestRealMavenClient_SearchFallbackErrors(t *testing.T) {
	m := newMavenTestServer(t)
	m.metaStatus = http.StatusNotFound

	m.searchStatus = http.StatusInternalServerError
	_, err := m.client().FetchArtifact(context.Background(), "g", "a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")

	m.searchStatus = http.StatusOK
	m.searchBody = `{not json`
	_, err = m.client().FetchArtifact(context.Background(), "g", "a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding maven response")
}

func TestRealMavenClient_MetadataTimeoutHonoured(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		hits.Add(1)
		select {
		case <-hold:
		case <-req.Context().Done():
		}
	}))
	defer srv.Close()

	c := &realMavenRegistryClient{httpClient: &http.Client{Timeout: 30 * time.Millisecond}, baseURL: srv.URL, metaURL: srv.URL}
	start := time.Now()
	_, err := c.FetchArtifact(context.Background(), "g", "a")
	require.Error(t, err)
	assert.True(t, isTimeoutErr(err), "expected a timeout error, got %v", err)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, int32(1), hits.Load(), "a timed-out metadata request must not retry via search")

	// A per-lookup context deadline (as lookupEach applies) is honoured too.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = (&realMavenRegistryClient{httpClient: &http.Client{}, baseURL: srv.URL, metaURL: srv.URL}).FetchArtifact(ctx, "g", "a")
	require.Error(t, err)
	assert.True(t, isTimeoutErr(err), "expected a timeout error, got %v", err)
}

func TestRealMavenClient_DefaultURLs(t *testing.T) {
	// Without overrides the client targets the CDN-backed repository for
	// metadata and the search API for the fallback.
	assert.Equal(t, "https://repo1.maven.org/maven2", mavenMetadataBaseURL)
	assert.Equal(t, "https://search.maven.org/solrsearch", mavenSearchBaseURL)
	assert.True(t, isMavenSnapshot("1.0-snapshot"))
	assert.False(t, isMavenSnapshot("1.0"))
}
