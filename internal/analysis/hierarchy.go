package analysis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/signal"
)

// EpicThreshold is the minimum number of signals in a cluster before an
// epic hierarchy is created. Clusters at or below this size produce flat beads.
const EpicThreshold = 5

// CreateEpicHierarchy generates a parent epic bead with child task beads for
// clusters that exceed EpicThreshold signals. It uses the LLM to generate
// an appropriate epic title and description. For clusters with 5 or fewer
// signals, it falls back to MergeClusterToBeads.
func CreateEpicHierarchy(ctx context.Context, cluster Cluster, signals []signal.RawSignal, provider llm.Provider) ([]AnalysisBead, error) {
	memberSignals := resolveClusterSignals(cluster, signals)

	// For small clusters, don't create an epic hierarchy.
	if len(memberSignals) <= EpicThreshold {
		return MergeClusterToBeads(cluster, signals), nil
	}

	// Use LLM to generate epic title and description.
	epicTitle, epicDesc, err := generateEpicMetadata(ctx, cluster, signals, provider)
	if err != nil {
		slog.Warn("LLM epic generation failed, using cluster name", "error", err)
		epicTitle = cluster.Name
		epicDesc = cluster.Description
	}

	epicID := fmt.Sprintf("epic-%s", cluster.ID)

	// Create the parent epic bead.
	epic := AnalysisBead{
		ID:          epicID,
		Title:       epicTitle,
		Description: epicDesc,
		Type:        "epic",
		Confidence:  cluster.Confidence,
		Tags:        append(cluster.Tags, "epic"),
	}

	// Create child task beads, each linked to the parent epic.
	beads := []AnalysisBead{epic}
	for i, sig := range memberSignals {
		child := AnalysisBead{
			ID:            fmt.Sprintf("%s-task-%d", cluster.ID, i),
			Title:         sig.Title,
			Description:   sig.Description,
			Type:          "task",
			Confidence:    sig.Confidence,
			Tags:          sig.Tags,
			ParentID:      epicID,
			SourceSignals: []signal.RawSignal{sig},
		}
		beads = append(beads, child)
	}

	return beads, nil
}

// generateEpicMetadata uses the LLM to create an epic title and description
// that summarizes the cluster's work items.
func generateEpicMetadata(ctx context.Context, cluster Cluster, signals []signal.RawSignal, provider llm.Provider) (title, description string, err error) {
	prompt := buildEpicPrompt(cluster, signals)

	resp, err := provider.Complete(ctx, llm.Request{
		SystemPrompt: "You are a software engineering assistant that creates concise epic summaries. Always respond with valid JSON only.",
		Prompt:       prompt,
		MaxTokens:    1024,
	})
	if err != nil {
		return "", "", fmt.Errorf("LLM completion failed: %w", err)
	}

	parsed, err := parseEpicResponse(resp.Content)
	if err != nil {
		return "", "", fmt.Errorf("parse epic response: %w", err)
	}

	return parsed.Title, parsed.Description, nil
}
