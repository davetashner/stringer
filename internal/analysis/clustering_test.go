// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/signal"
)

// -----------------------------------------------------------------------
// Jaccard similarity tests
// -----------------------------------------------------------------------

func TestJaccardSimilarity_IdenticalStrings(t *testing.T) {
	assert.InDelta(t, 1.0, jaccardSimilarity("fix the bug", "fix the bug"), 0.01)
}

func TestJaccardSimilarity_CompletelyDifferent(t *testing.T) {
	assert.InDelta(t, 0.0, jaccardSimilarity("authentication module", "database migration"), 0.01)
}

func TestJaccardSimilarity_PartialOverlap(t *testing.T) {
	sim := jaccardSimilarity("fix authentication bug", "fix authorization bug")
	assert.Greater(t, sim, 0.0)
	assert.Less(t, sim, 1.0)
}

func TestJaccardSimilarity_EmptyStrings(t *testing.T) {
	assert.InDelta(t, 0.0, jaccardSimilarity("", ""), 0.01)
}

func TestJaccardSimilarity_OneEmpty(t *testing.T) {
	assert.InDelta(t, 0.0, jaccardSimilarity("hello world", ""), 0.01)
}

func TestJaccardSimilarity_CaseInsensitive(t *testing.T) {
	assert.InDelta(t, 1.0, jaccardSimilarity("Fix Bug", "fix bug"), 0.01)
}

func TestJaccardSimilarity_StopWordsFiltered(t *testing.T) {
	// "fix the bug" and "fix a bug" should be identical after stop word removal.
	sim := jaccardSimilarity("fix the bug", "fix a bug")
	assert.InDelta(t, 1.0, sim, 0.01)
}

func TestJaccardSimilarity_Punctuation(t *testing.T) {
	sim := jaccardSimilarity("TODO: fix this!", "TODO: fix this")
	assert.InDelta(t, 1.0, sim, 0.01)
}

// -----------------------------------------------------------------------
// normalizeTitle tests
// -----------------------------------------------------------------------

func TestNormalizeTitle_Basic(t *testing.T) {
	words := normalizeTitle("Fix the authentication Bug")
	assert.Contains(t, words, "fix")
	assert.Contains(t, words, "authentication")
	assert.Contains(t, words, "bug")
	assert.NotContains(t, words, "the")
}

func TestNormalizeTitle_Empty(t *testing.T) {
	assert.Empty(t, normalizeTitle(""))
}

func TestNormalizeTitle_SingleChar(t *testing.T) {
	// Single character words should be filtered.
	words := normalizeTitle("a b c hello")
	assert.Equal(t, []string{"hello"}, words)
}

func TestNormalizeTitle_NumbersPreserved(t *testing.T) {
	words := normalizeTitle("fix issue 42")
	assert.Contains(t, words, "fix")
	assert.Contains(t, words, "issue")
	assert.Contains(t, words, "42")
}

// -----------------------------------------------------------------------
// PreFilterSignals tests
// -----------------------------------------------------------------------

func TestPreFilterSignals_Empty(t *testing.T) {
	groups := PreFilterSignals(nil, 0.7)
	assert.Nil(t, groups)
}

func TestPreFilterSignals_SingleSignal(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix bug", FilePath: "main.go"},
	}
	groups := PreFilterSignals(signals, 0.7)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Members, 1)
	assert.Equal(t, []int{0}, groups[0].MemberIndices)
}

func TestPreFilterSignals_SameDirectory(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix A", FilePath: "internal/auth/login.go"},
		{Source: "todos", Title: "fix B", FilePath: "internal/auth/logout.go"},
	}
	groups := PreFilterSignals(signals, 0.7)
	// Same source and same directory => grouped together.
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Members, 2)
}

func TestPreFilterSignals_DifferentSource(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix bug", FilePath: "internal/auth/login.go"},
		{Source: "gitlog", Title: "fix bug", FilePath: "internal/auth/login.go"},
	}
	groups := PreFilterSignals(signals, 0.7)
	// Different sources => separate groups even with same path.
	require.Len(t, groups, 2)
}

func TestPreFilterSignals_SimilarTitles(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix authentication error handling"},
		{Source: "todos", Title: "fix authentication error logging"},
	}
	groups := PreFilterSignals(signals, 0.5) // Lower threshold to catch partial overlap.
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Members, 2)
}

func TestPreFilterSignals_DissimilarTitles(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix database connection pool"},
		{Source: "todos", Title: "add user authentication"},
	}
	groups := PreFilterSignals(signals, 0.7)
	require.Len(t, groups, 2)
}

func TestPreFilterSignals_MemberIndicesCorrect(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "alpha", FilePath: "pkg/a.go"},
		{Source: "todos", Title: "beta", FilePath: "pkg/b.go"},
		{Source: "gitlog", Title: "gamma"},
	}
	groups := PreFilterSignals(signals, 0.7)

	// First two share source + directory => one group; third is separate.
	require.Len(t, groups, 2)

	// Verify indices map correctly.
	allIndices := make(map[int]bool)
	for _, g := range groups {
		for _, idx := range g.MemberIndices {
			allIndices[idx] = true
		}
	}
	assert.True(t, allIndices[0])
	assert.True(t, allIndices[1])
	assert.True(t, allIndices[2])
}

// -----------------------------------------------------------------------
// pathSimilar tests
// -----------------------------------------------------------------------

func TestPathSimilar_SameDirectory(t *testing.T) {
	assert.True(t, pathSimilar("pkg/auth/login.go", "pkg/auth/logout.go"))
}

func TestPathSimilar_DifferentDirectory(t *testing.T) {
	assert.False(t, pathSimilar("pkg/auth/login.go", "pkg/db/conn.go"))
}

func TestPathSimilar_RootFiles(t *testing.T) {
	// Both at root "." => false (we require non-"." directory).
	assert.False(t, pathSimilar("main.go", "utils.go"))
}

func TestPathSimilar_Empty(t *testing.T) {
	assert.False(t, pathSimilar("", "main.go"))
	assert.False(t, pathSimilar("main.go", ""))
}

// -----------------------------------------------------------------------
// Prompt building tests
// -----------------------------------------------------------------------

func TestBuildClusteringPrompt_ContainsSignals(t *testing.T) {
	groups := []SignalGroup{
		{
			Representative: signal.RawSignal{Title: "fix auth bug", Kind: "todo", Source: "todos", FilePath: "auth.go"},
			Members: []signal.RawSignal{
				{Title: "fix auth bug", Kind: "todo", Source: "todos", FilePath: "auth.go"},
			},
			MemberIndices: []int{0},
		},
	}
	prompt := buildClusteringPrompt(groups)

	assert.Contains(t, prompt, "sig-0")
	assert.Contains(t, prompt, "fix auth bug")
	assert.Contains(t, prompt, "todo")
	assert.Contains(t, prompt, "auth.go")
	assert.Contains(t, prompt, "JSON")
}

func TestBuildClusteringPrompt_TruncatesDescription(t *testing.T) {
	longDesc := ""
	for i := 0; i < 300; i++ {
		longDesc += "x"
	}
	groups := []SignalGroup{
		{
			Representative: signal.RawSignal{Title: "test", Description: longDesc},
			Members:        []signal.RawSignal{{Title: "test", Description: longDesc}},
			MemberIndices:  []int{0},
		},
	}
	prompt := buildClusteringPrompt(groups)
	assert.Contains(t, prompt, "...")
}

func TestBuildClusteringPrompt_MultipleGroups(t *testing.T) {
	groups := []SignalGroup{
		{
			Representative: signal.RawSignal{Title: "first"},
			Members:        []signal.RawSignal{{Title: "first"}},
			MemberIndices:  []int{0},
		},
		{
			Representative: signal.RawSignal{Title: "second"},
			Members:        []signal.RawSignal{{Title: "second"}},
			MemberIndices:  []int{1},
		},
	}
	prompt := buildClusteringPrompt(groups)
	assert.Contains(t, prompt, "sig-0")
	assert.Contains(t, prompt, "sig-1")
}

// -----------------------------------------------------------------------
// parseClusterResponse tests
// -----------------------------------------------------------------------

func TestParseClusterResponse_ValidWrapper(t *testing.T) {
	input := `{"clusters": [{"name": "Auth fixes", "description": "auth related", "signal_ids": ["sig-0", "sig-1"]}]}`
	items, err := parseClusterResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Auth fixes", items[0].Name)
	assert.Equal(t, []string{"sig-0", "sig-1"}, items[0].SignalIDs)
}

func TestParseClusterResponse_ValidArray(t *testing.T) {
	input := `[{"name": "DB fixes", "description": "database", "signal_ids": ["sig-0"]}]`
	items, err := parseClusterResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "DB fixes", items[0].Name)
}

func TestParseClusterResponse_WithCodeFences(t *testing.T) {
	input := "```json\n" + `{"clusters": [{"name": "test", "description": "d", "signal_ids": ["sig-0"]}]}` + "\n```"
	items, err := parseClusterResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "test", items[0].Name)
}

func TestParseClusterResponse_InvalidJSON(t *testing.T) {
	_, err := parseClusterResponse("not json at all")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestParseClusterResponse_EmptyContent(t *testing.T) {
	_, err := parseClusterResponse("")
	require.Error(t, err)
}

func TestParseClusterResponse_MultipleClusters(t *testing.T) {
	input := `{"clusters": [
		{"name": "A", "description": "first", "signal_ids": ["sig-0"]},
		{"name": "B", "description": "second", "signal_ids": ["sig-1", "sig-2"]}
	]}`
	items, err := parseClusterResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "A", items[0].Name)
	assert.Equal(t, "B", items[1].Name)
}

// -----------------------------------------------------------------------
// parseEpicResponse tests
// -----------------------------------------------------------------------

func TestParseEpicResponse_Valid(t *testing.T) {
	input := `{"title": "Auth Improvements", "description": "Several auth fixes needed"}`
	resp, err := parseEpicResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "Auth Improvements", resp.Title)
	assert.Equal(t, "Several auth fixes needed", resp.Description)
}

func TestParseEpicResponse_MissingTitle(t *testing.T) {
	input := `{"title": "", "description": "no title"}`
	_, err := parseEpicResponse(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing title")
}

func TestParseEpicResponse_InvalidJSON(t *testing.T) {
	_, err := parseEpicResponse("not json")
	require.Error(t, err)
}

func TestParseEpicResponse_WithCodeFences(t *testing.T) {
	input := "```json\n" + `{"title": "Test Epic", "description": "test"}` + "\n```"
	resp, err := parseEpicResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "Test Epic", resp.Title)
}

// -----------------------------------------------------------------------
// formClustersWithLLM tests (using mock provider)
// -----------------------------------------------------------------------

func TestFormClustersWithLLM_Success(t *testing.T) {
	signals := testSignals()

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Auth fixes", Description: "auth related", SignalIDs: []string{"sig-0", "sig-1"}},
			{Name: "DB work", Description: "database", SignalIDs: []string{"sig-2"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	groups := PreFilterSignals(signals, 0.7)

	clusters, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.NoError(t, err)
	require.Len(t, clusters, 2)
	assert.Equal(t, "Auth fixes", clusters[0].Name)
	assert.Equal(t, []string{"sig-0", "sig-1"}, clusters[0].SignalIDs)
	assert.Equal(t, "DB work", clusters[1].Name)
}

func TestFormClustersWithLLM_InvalidSignalIDs(t *testing.T) {
	signals := testSignals()

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Mixed", Description: "some invalid", SignalIDs: []string{"sig-0", "sig-999"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	groups := PreFilterSignals(signals, 0.7)

	clusters, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.NoError(t, err)
	require.Len(t, clusters, 1)
	// sig-999 should be filtered out.
	assert.Equal(t, []string{"sig-0"}, clusters[0].SignalIDs)
}

func TestFormClustersWithLLM_LLMError(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Err: errors.New("API error")})
	groups := PreFilterSignals(signals, 0.7)

	_, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM completion failed")
}

func TestFormClustersWithLLM_BadJSON(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Content: "this is not json"})
	groups := PreFilterSignals(signals, 0.7)

	_, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse cluster response")
}

func TestFormClustersWithLLM_EmptyClusterFiltered(t *testing.T) {
	signals := testSignals()

	// All signal IDs are invalid => empty cluster should be dropped.
	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Ghost", Description: "all invalid", SignalIDs: []string{"sig-100", "sig-200"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	groups := PreFilterSignals(signals, 0.7)

	clusters, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.NoError(t, err)
	assert.Empty(t, clusters)
}

func TestFormClustersWithLLM_ConfidenceComputed(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "low conf", Confidence: 0.3},
		{Source: "todos", Title: "high conf", Confidence: 0.9},
	}

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Mixed", Description: "test", SignalIDs: []string{"sig-0", "sig-1"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	groups := PreFilterSignals(signals, 0.7)

	clusters, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.NoError(t, err)
	require.Len(t, clusters, 1)
	assert.InDelta(t, 0.9, clusters[0].Confidence, 0.01)
}

func TestFormClustersWithLLM_TagsCombined(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "a", Tags: []string{"auth", "security"}},
		{Source: "todos", Title: "b", Tags: []string{"security", "backend"}},
	}

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Security", Description: "test", SignalIDs: []string{"sig-0", "sig-1"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	groups := PreFilterSignals(signals, 0.7)

	clusters, err := formClustersWithLLM(context.Background(), groups, mock, signals)
	require.NoError(t, err)
	require.Len(t, clusters, 1)
	assert.Contains(t, clusters[0].Tags, "auth")
	assert.Contains(t, clusters[0].Tags, "security")
	assert.Contains(t, clusters[0].Tags, "backend")
	// "security" should not be duplicated.
	secCount := 0
	for _, tag := range clusters[0].Tags {
		if tag == "security" {
			secCount++
		}
	}
	assert.Equal(t, 1, secCount)
}

// -----------------------------------------------------------------------
// MergeClusterToBeads tests
// -----------------------------------------------------------------------

func TestMergeClusterToBeads_SingleSignal(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix auth", Description: "auth is broken", Confidence: 0.8, Tags: []string{"auth"}},
	}
	cluster := Cluster{
		ID:        "cluster-0",
		Name:      "Auth",
		SignalIDs: []string{"sig-0"},
		Tags:      []string{"cluster-tag"},
	}

	beads := MergeClusterToBeads(cluster, signals)
	require.Len(t, beads, 1)
	assert.Equal(t, "fix auth", beads[0].Title)
	assert.Equal(t, "auth is broken", beads[0].Description)
	assert.Equal(t, "task", beads[0].Type)
	assert.InDelta(t, 0.8, beads[0].Confidence, 0.01)
	// Tags should be combined from cluster and signal.
	assert.Contains(t, beads[0].Tags, "cluster-tag")
	assert.Contains(t, beads[0].Tags, "auth")
}

func TestMergeClusterToBeads_MultipleSignals(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "fix login", FilePath: "auth/login.go", Line: 10, Confidence: 0.6},
		{Source: "todos", Title: "fix logout", FilePath: "auth/logout.go", Line: 20, Confidence: 0.9},
	}
	cluster := Cluster{
		ID:          "cluster-0",
		Name:        "Auth Fixes",
		Description: "Authentication improvements",
		SignalIDs:   []string{"sig-0", "sig-1"},
		Confidence:  0.9,
		Tags:        []string{"auth"},
	}

	beads := MergeClusterToBeads(cluster, signals)
	require.Len(t, beads, 1)
	assert.Equal(t, "Auth Fixes", beads[0].Title)
	assert.Contains(t, beads[0].Description, "Authentication improvements")
	assert.Contains(t, beads[0].Description, "fix login")
	assert.Contains(t, beads[0].Description, "fix logout")
	assert.Contains(t, beads[0].Description, "auth/login.go:10")
	assert.InDelta(t, 0.9, beads[0].Confidence, 0.01)
}

func TestMergeClusterToBeads_EmptyCluster(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "test"},
	}
	cluster := Cluster{
		ID:        "cluster-0",
		Name:      "Empty",
		SignalIDs: []string{}, // No signals.
	}

	beads := MergeClusterToBeads(cluster, signals)
	assert.Nil(t, beads)
}

func TestMergeClusterToBeads_InvalidIDs(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "test"},
	}
	cluster := Cluster{
		ID:        "cluster-0",
		Name:      "Ghost",
		SignalIDs: []string{"sig-999"}, // Invalid index.
	}

	beads := MergeClusterToBeads(cluster, signals)
	assert.Nil(t, beads)
}

// -----------------------------------------------------------------------
// BeadsToSignals tests
// -----------------------------------------------------------------------

func TestBeadsToSignals_Basic(t *testing.T) {
	beads := []AnalysisBead{
		{
			ID:          "test-1",
			Title:       "Fix auth",
			Description: "Auth is broken",
			Type:        "task",
			Confidence:  0.8,
			Tags:        []string{"auth"},
			SourceSignals: []signal.RawSignal{
				{FilePath: "auth.go", Line: 10, Author: "dev", Timestamp: time.Now()},
			},
		},
	}

	signals := BeadsToSignals(beads)
	require.Len(t, signals, 1)
	assert.Equal(t, "cluster", signals[0].Source)
	assert.Equal(t, "Fix auth", signals[0].Title)
	assert.Equal(t, "auth.go", signals[0].FilePath)
	assert.Equal(t, 10, signals[0].Line)
	assert.Equal(t, "dev", signals[0].Author)
}

func TestBeadsToSignals_NoSourceSignals(t *testing.T) {
	beads := []AnalysisBead{
		{
			ID:    "test-1",
			Title: "orphan",
			Type:  "task",
		},
	}

	signals := BeadsToSignals(beads)
	require.Len(t, signals, 1)
	assert.Equal(t, "", signals[0].FilePath)
}

// -----------------------------------------------------------------------
// CreateEpicHierarchy tests
// -----------------------------------------------------------------------

func TestCreateEpicHierarchy_SmallCluster(t *testing.T) {
	signals := testSignals() // 3 signals, below EpicThreshold.
	cluster := Cluster{
		ID:        "cluster-0",
		Name:      "Small",
		SignalIDs: []string{"sig-0", "sig-1", "sig-2"},
		Tags:      []string{"small"},
	}

	mock := llm.NewMockProvider() // Should not be called.
	beads, err := CreateEpicHierarchy(context.Background(), cluster, signals, mock)
	require.NoError(t, err)
	// Should fall through to MergeClusterToBeads, not create an epic.
	require.Len(t, beads, 1)
	assert.Equal(t, "task", beads[0].Type)
	assert.Empty(t, mock.Calls(), "LLM should not be called for small clusters")
}

func TestCreateEpicHierarchy_LargeCluster(t *testing.T) {
	signals := makeLargeSignalSlice(8)
	var ids []string
	for i := 0; i < 8; i++ {
		ids = append(ids, fmt.Sprintf("sig-%d", i))
	}
	cluster := Cluster{
		ID:          "cluster-0",
		Name:        "Big Auth",
		Description: "Lots of auth work",
		SignalIDs:   ids,
		Confidence:  0.85,
		Tags:        []string{"auth"},
	}

	epicJSON := `{"title": "Authentication Overhaul", "description": "Comprehensive auth improvements needed across 8 modules."}`
	mock := llm.NewMockProvider(llm.MockResponse{Content: epicJSON})

	beads, err := CreateEpicHierarchy(context.Background(), cluster, signals, mock)
	require.NoError(t, err)

	// Should be 1 epic + 8 children = 9 total.
	require.Len(t, beads, 9)

	// First bead is the epic.
	assert.Equal(t, "epic", beads[0].Type)
	assert.Equal(t, "Authentication Overhaul", beads[0].Title)
	assert.Contains(t, beads[0].Tags, "epic")

	// Remaining beads are tasks linked to the epic.
	for i := 1; i < len(beads); i++ {
		assert.Equal(t, "task", beads[i].Type)
		assert.Equal(t, beads[0].ID, beads[i].ParentID)
	}
}

func TestCreateEpicHierarchy_LLMFailureFallback(t *testing.T) {
	signals := makeLargeSignalSlice(7)
	var ids []string
	for i := 0; i < 7; i++ {
		ids = append(ids, fmt.Sprintf("sig-%d", i))
	}
	cluster := Cluster{
		ID:          "cluster-0",
		Name:        "Fallback Cluster",
		Description: "Should use cluster name on LLM failure",
		SignalIDs:   ids,
		Confidence:  0.7,
	}

	mock := llm.NewMockProvider(llm.MockResponse{Err: errors.New("LLM down")})

	beads, err := CreateEpicHierarchy(context.Background(), cluster, signals, mock)
	require.NoError(t, err)

	// Should still produce epic + children, using cluster.Name as title.
	require.Len(t, beads, 8) // 1 epic + 7 children.
	assert.Equal(t, "epic", beads[0].Type)
	assert.Equal(t, "Fallback Cluster", beads[0].Title)
}

func TestCreateEpicHierarchy_ExactThreshold(t *testing.T) {
	signals := makeLargeSignalSlice(EpicThreshold)
	var ids []string
	for i := 0; i < EpicThreshold; i++ {
		ids = append(ids, fmt.Sprintf("sig-%d", i))
	}
	cluster := Cluster{
		ID:        "cluster-0",
		Name:      "Threshold",
		SignalIDs: ids,
	}

	mock := llm.NewMockProvider()
	beads, err := CreateEpicHierarchy(context.Background(), cluster, signals, mock)
	require.NoError(t, err)

	// At exactly the threshold, should NOT create an epic (uses <=).
	assert.Equal(t, "task", beads[0].Type)
	assert.Empty(t, mock.Calls())
}

// -----------------------------------------------------------------------
// ClusterSignals end-to-end tests
// -----------------------------------------------------------------------

func TestClusterSignals_EmptyInput(t *testing.T) {
	mock := llm.NewMockProvider()
	result, err := ClusterSignals(context.Background(), nil, mock, DefaultClusterConfig())
	require.NoError(t, err)
	assert.Empty(t, result.Clusters)
	assert.Empty(t, result.Unclustered)
}

func TestClusterSignals_EndToEnd(t *testing.T) {
	signals := testSignals()

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Auth", Description: "auth fixes", SignalIDs: []string{"sig-0", "sig-1"}},
			{Name: "DB", Description: "db work", SignalIDs: []string{"sig-2"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	cfg := DefaultClusterConfig()

	result, err := ClusterSignals(context.Background(), signals, mock, cfg)
	require.NoError(t, err)
	require.Len(t, result.Clusters, 2)
	assert.Equal(t, "Auth", result.Clusters[0].Name)
	assert.Equal(t, "DB", result.Clusters[1].Name)
	assert.Empty(t, result.Unclustered)
}

func TestClusterSignals_LLMFailureFallback(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Err: errors.New("API down")})
	cfg := DefaultClusterConfig()

	result, err := ClusterSignals(context.Background(), signals, mock, cfg)
	require.NoError(t, err)
	// Fallback: each signal gets its own cluster.
	assert.Len(t, result.Clusters, len(signals))
	assert.Empty(t, result.Unclustered)
}

func TestClusterSignals_MinClusterSize(t *testing.T) {
	signals := testSignals()

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Big", Description: "big cluster", SignalIDs: []string{"sig-0", "sig-1"}},
			{Name: "Tiny", Description: "single", SignalIDs: []string{"sig-2"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	cfg := ClusterConfig{
		SimilarityThreshold: 0.7,
		MinClusterSize:      2, // Require at least 2 signals per cluster.
		MaxClusterSize:      20,
	}

	result, err := ClusterSignals(context.Background(), signals, mock, cfg)
	require.NoError(t, err)
	assert.Len(t, result.Clusters, 1) // "Tiny" cluster should be filtered out.
	assert.Equal(t, "Big", result.Clusters[0].Name)
	assert.Contains(t, result.Unclustered, "sig-2")
}

func TestClusterSignals_MaxClusterSize(t *testing.T) {
	signals := makeLargeSignalSlice(10)
	var allIDs []string
	for i := 0; i < 10; i++ {
		allIDs = append(allIDs, fmt.Sprintf("sig-%d", i))
	}

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "Huge", Description: "too many", SignalIDs: allIDs},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})
	cfg := ClusterConfig{
		SimilarityThreshold: 0.7,
		MinClusterSize:      1,
		MaxClusterSize:      5, // Only allow 5 signals max.
	}

	result, err := ClusterSignals(context.Background(), signals, mock, cfg)
	require.NoError(t, err)
	require.Len(t, result.Clusters, 1)
	assert.Len(t, result.Clusters[0].SignalIDs, 5) // Truncated to max.
}

func TestClusterSignals_DefaultConfigApplied(t *testing.T) {
	signals := testSignals()

	clusterJSON := mustJSON(t, clusterResponseWrapper{
		Clusters: []clusterResponseItem{
			{Name: "All", Description: "all signals", SignalIDs: []string{"sig-0", "sig-1", "sig-2"}},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: clusterJSON})

	// Pass zero-valued config — defaults should be applied.
	result, err := ClusterSignals(context.Background(), signals, mock, ClusterConfig{})
	require.NoError(t, err)
	require.Len(t, result.Clusters, 1)
}

func TestClusterSignals_ContextCancellation(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Content: "{}"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// The mock respects context cancellation.
	result, err := ClusterSignals(ctx, signals, mock, DefaultClusterConfig())
	// Should fall back gracefully on context cancellation.
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// -----------------------------------------------------------------------
// DefaultClusterConfig test
// -----------------------------------------------------------------------

func TestDefaultClusterConfig(t *testing.T) {
	cfg := DefaultClusterConfig()
	assert.InDelta(t, 0.7, cfg.SimilarityThreshold, 0.01)
	assert.Equal(t, 1, cfg.MinClusterSize)
	assert.Equal(t, 20, cfg.MaxClusterSize)
}

// -----------------------------------------------------------------------
// findSignalInSlice tests
// -----------------------------------------------------------------------

func TestFindSignalInSlice_Valid(t *testing.T) {
	signals := testSignals()
	sig := findSignalInSlice("sig-1", signals)
	require.NotNil(t, sig)
	assert.Equal(t, "fix login bug", sig.Title)
}

func TestFindSignalInSlice_OutOfRange(t *testing.T) {
	signals := testSignals()
	assert.Nil(t, findSignalInSlice("sig-999", signals))
}

func TestFindSignalInSlice_InvalidFormat(t *testing.T) {
	signals := testSignals()
	assert.Nil(t, findSignalInSlice("invalid", signals))
	assert.Nil(t, findSignalInSlice("sig-abc", signals))
	assert.Nil(t, findSignalInSlice("", signals))
}

func TestFindSignalInSlice_NegativeIndex(t *testing.T) {
	signals := testSignals()
	assert.Nil(t, findSignalInSlice("sig--1", signals))
}

// -----------------------------------------------------------------------
// combineTags tests
// -----------------------------------------------------------------------

func TestCombineTags_Dedup(t *testing.T) {
	tags := combineTags([]string{"a", "b"}, []string{"b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, tags)
}

func TestCombineTags_Empty(t *testing.T) {
	assert.Nil(t, combineTags(nil, nil))
}

func TestCombineTags_OneEmpty(t *testing.T) {
	tags := combineTags([]string{"a"}, nil)
	assert.Equal(t, []string{"a"}, tags)
}

// -----------------------------------------------------------------------
// buildEpicPrompt tests
// -----------------------------------------------------------------------

func TestBuildEpicPrompt_ContainsClusterInfo(t *testing.T) {
	signals := testSignals()
	cluster := Cluster{
		Name:        "Auth Work",
		Description: "Auth improvements needed",
		SignalIDs:   []string{"sig-0", "sig-1"},
	}

	prompt := buildEpicPrompt(cluster, signals)
	assert.Contains(t, prompt, "Auth Work")
	assert.Contains(t, prompt, "Auth improvements needed")
	assert.Contains(t, prompt, "sig-0")
	assert.Contains(t, prompt, "fix auth error")
}

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

func testSignals() []signal.RawSignal {
	return []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "fix auth error", FilePath: "internal/auth/handler.go", Line: 42, Confidence: 0.7, Tags: []string{"auth"}},
		{Source: "todos", Kind: "fixme", Title: "fix login bug", FilePath: "internal/auth/login.go", Line: 15, Confidence: 0.8, Tags: []string{"auth", "bug"}},
		{Source: "gitlog", Kind: "churn", Title: "high churn in database layer", FilePath: "internal/db/conn.go", Confidence: 0.6, Tags: []string{"db"}},
	}
}

func makeLargeSignalSlice(n int) []signal.RawSignal {
	signals := make([]signal.RawSignal, n)
	for i := range signals {
		signals[i] = signal.RawSignal{
			Source:     "todos",
			Kind:       "todo",
			Title:      fmt.Sprintf("signal %d: fix issue in module %d", i, i%3),
			FilePath:   fmt.Sprintf("internal/mod%d/file.go", i%3),
			Line:       i*10 + 1,
			Confidence: 0.5 + float64(i%5)*0.1,
			Tags:       []string{fmt.Sprintf("mod%d", i%3)},
		}
	}
	return signals
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}
