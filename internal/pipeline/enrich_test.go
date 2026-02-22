// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

func TestBoostColocatedSignals_ChurnBoost(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "main.go", Confidence: 0.50},
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.60 {
		t.Errorf("todo confidence = %v, want 0.60 (+0.10 churn boost)", got)
	}
}

func TestBoostColocatedSignals_VulnBoost(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "main.go", Confidence: 0.50},
		{Kind: "vulnerable-dependency", FilePath: "main.go", Confidence: 0.70},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.55 {
		t.Errorf("todo confidence = %v, want 0.55 (+0.05 vuln boost)", got)
	}
}

func TestBoostColocatedSignals_LotteryBoost(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "main.go", Confidence: 0.50},
		{Kind: "low-lottery-risk", FilePath: "main.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.55 {
		t.Errorf("todo confidence = %v, want 0.55 (+0.05 lottery boost)", got)
	}
}

func TestBoostColocatedSignals_MultipleBoostsStack(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "main.go", Confidence: 0.50},
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
		{Kind: "vulnerable-dependency", FilePath: "main.go", Confidence: 0.70},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.65 {
		t.Errorf("todo confidence = %v, want 0.65 (+0.10 churn + 0.05 vuln)", got)
	}
}

func TestBoostColocatedSignals_AllThreeBoostsStack(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "main.go", Confidence: 0.50},
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
		{Kind: "vulnerable-dependency", FilePath: "main.go", Confidence: 0.70},
		{Kind: "low-lottery-risk", FilePath: "main.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.70 {
		t.Errorf("todo confidence = %v, want 0.70 (+0.10 + 0.05 + 0.05)", got)
	}
}

func TestBoostColocatedSignals_CapAt1(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "main.go", Confidence: 0.95},
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 1.0 {
		t.Errorf("todo confidence = %v, want 1.0 (capped)", got)
	}
}

func TestBoostColocatedSignals_NoSelfBoost(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.60 {
		t.Errorf("churn confidence = %v, want 0.60 (no self-boost)", got)
	}
}

func TestBoostColocatedSignals_NoSelfBoostWithOtherSignals(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
		{Kind: "todo", FilePath: "main.go", Confidence: 0.50},
	}
	BoostColocatedSignals(signals)

	// churn should not get boosted by its own kind, but it has no other
	// boost-eligible kinds on that file (todo is not a boost trigger).
	if got := signals[0].Confidence; got != 0.60 {
		t.Errorf("churn confidence = %v, want 0.60 (no self-boost)", got)
	}
	// todo gets +0.10 from churn co-location.
	if got := signals[1].Confidence; got != 0.60 {
		t.Errorf("todo confidence = %v, want 0.60 (+0.10 churn boost)", got)
	}
}

func TestBoostColocatedSignals_DifferentFiles(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "a.go", Confidence: 0.50},
		{Kind: "churn", FilePath: "b.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.50 {
		t.Errorf("todo confidence = %v, want 0.50 (no co-location)", got)
	}
}

func TestBoostColocatedSignals_Empty(t *testing.T) {
	BoostColocatedSignals(nil)
	BoostColocatedSignals([]signal.RawSignal{})
	// No panic is the success condition.
}

func TestBoostColocatedSignals_NoCascading(t *testing.T) {
	// Two boost-eligible kinds on the same file: churn and low-lottery-risk.
	// Each should boost other signals but not create new eligibility during
	// the boost pass.
	signals := []signal.RawSignal{
		{Kind: "churn", FilePath: "main.go", Confidence: 0.60},
		{Kind: "low-lottery-risk", FilePath: "main.go", Confidence: 0.50},
	}
	BoostColocatedSignals(signals)

	// churn gets +0.05 from low-lottery-risk (not self).
	if got := signals[0].Confidence; got != 0.65 {
		t.Errorf("churn confidence = %v, want 0.65 (+0.05 lottery boost)", got)
	}
	// low-lottery-risk gets +0.10 from churn (not self).
	if got := signals[1].Confidence; got != 0.60 {
		t.Errorf("low-lottery-risk confidence = %v, want 0.60 (+0.10 churn boost)", got)
	}
}

func TestBoostColocatedSignals_EmptyFilePath(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "todo", FilePath: "", Confidence: 0.50},
		{Kind: "churn", FilePath: "", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	// Signals with empty file paths should not be indexed or boosted.
	if got := signals[0].Confidence; got != 0.50 {
		t.Errorf("todo confidence = %v, want 0.50 (empty path, no boost)", got)
	}
}

func TestBoostColocatedSignals_VulnDoesNotSelfBoost(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "vulnerable-dependency", FilePath: "go.mod", Confidence: 0.80},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.80 {
		t.Errorf("vuln confidence = %v, want 0.80 (no self-boost)", got)
	}
}

func TestBoostColocatedSignals_LotteryDoesNotSelfBoost(t *testing.T) {
	signals := []signal.RawSignal{
		{Kind: "low-lottery-risk", FilePath: "main.go", Confidence: 0.60},
	}
	BoostColocatedSignals(signals)

	if got := signals[0].Confidence; got != 0.60 {
		t.Errorf("lottery confidence = %v, want 0.60 (no self-boost)", got)
	}
}
