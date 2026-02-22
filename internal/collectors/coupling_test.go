// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

// --- Import extraction tests ---

func TestExtractGoImports_Single(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`import "github.com/example/proj/internal/config"`,
		`import "fmt"`,
	}
	allModules := map[string]bool{"internal/config": true}
	got := extractGoImports(lines, "cmd/main.go", "github.com/example/proj", allModules)

	if len(got) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(got), got)
	}
	if got[0] != "internal/config" {
		t.Errorf("expected internal/config, got %q", got[0])
	}
}

func TestExtractGoImports_Group(t *testing.T) {
	lines := []string{
		`package main`,
		``,
		`import (`,
		`	"fmt"`,
		`	"github.com/example/proj/internal/config"`,
		`	svc "github.com/example/proj/internal/service"`,
		`	"os"`,
		`)`,
	}
	allModules := map[string]bool{
		"internal/config":  true,
		"internal/service": true,
	}
	got := extractGoImports(lines, "cmd/main.go", "github.com/example/proj", allModules)

	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
	sort.Strings(got)
	if got[0] != "internal/config" || got[1] != "internal/service" {
		t.Errorf("unexpected imports: %v", got)
	}
}

func TestExtractGoImports_ExternalFiltered(t *testing.T) {
	lines := []string{
		`import "github.com/other/lib"`,
		`import "fmt"`,
	}
	allModules := map[string]bool{}
	got := extractGoImports(lines, "main.go", "github.com/example/proj", allModules)

	if len(got) != 0 {
		t.Errorf("expected 0 imports (external filtered), got %d: %v", len(got), got)
	}
}

func TestExtractJSImports_ImportFrom(t *testing.T) {
	lines := []string{
		`import { foo } from './utils'`,
		`import bar from '../lib/helper'`,
		`import React from 'react'`, // external, should be filtered
	}
	allModules := map[string]bool{
		"src/utils":  true,
		"lib/helper": true,
	}
	got := extractJSImports(lines, "src/app.js", "", allModules)

	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
}

func TestExtractJSImports_Require(t *testing.T) {
	lines := []string{
		`const utils = require('./utils')`,
		`const lodash = require('lodash')`, // external
	}
	allModules := map[string]bool{
		"src/utils": true,
	}
	got := extractJSImports(lines, "src/app.js", "", allModules)

	if len(got) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(got), got)
	}
	if got[0] != "src/utils" {
		t.Errorf("expected src/utils, got %q", got[0])
	}
}

func TestExtractJSImports_ExternalFiltered(t *testing.T) {
	lines := []string{
		`import React from 'react'`,
		`import express from 'express'`,
		`const _ = require('lodash')`,
	}
	allModules := map[string]bool{}
	got := extractJSImports(lines, "src/app.js", "", allModules)

	if len(got) != 0 {
		t.Errorf("expected 0 imports (bare specifiers), got %d: %v", len(got), got)
	}
}

func TestExtractPythonImports(t *testing.T) {
	lines := []string{
		`import myapp.models`,
		`from myapp.utils import helper`,
		`import os`,            // external, won't match allModules
		`from sys import argv`, // external
	}
	allModules := map[string]bool{
		"myapp.models": true,
		"myapp.utils":  true,
	}
	got := extractPythonImports(lines, "myapp/views.py", "", allModules)

	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
	sort.Strings(got)
	if got[0] != "myapp.models" || got[1] != "myapp.utils" {
		t.Errorf("unexpected imports: %v", got)
	}
}

func TestExtractPythonImports_ParentModule(t *testing.T) {
	lines := []string{
		`from myapp.models.user import User`,
	}
	allModules := map[string]bool{
		"myapp.models": true, // parent exists, child doesn't
	}
	got := extractPythonImports(lines, "myapp/views.py", "", allModules)

	if len(got) != 1 {
		t.Fatalf("expected 1 import (parent match), got %d: %v", len(got), got)
	}
	if got[0] != "myapp.models" {
		t.Errorf("expected myapp.models, got %q", got[0])
	}
}

func TestExtractJavaImports(t *testing.T) {
	lines := []string{
		`import com.example.service.UserService;`,
		`import static com.example.utils.Helper;`,
		`import java.util.List;`, // won't match allModules
	}
	allModules := map[string]bool{
		"com.example.service": true,
		"com.example.utils":   true,
	}
	got := extractJavaImports(lines, "com/example/app/App.java", "", allModules)

	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
}

func TestExtractRustImports(t *testing.T) {
	lines := []string{
		`use crate::config::Settings;`,
		`pub use crate::db;`,
		`use std::io;`, // external
	}
	allModules := map[string]bool{
		"config": true,
		"db":     true,
	}
	got := extractRustImports(lines, "src/main.rs", "", allModules)

	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
}

func TestExtractRubyImports(t *testing.T) {
	lines := []string{
		`require_relative './models/user'`,
		`require 'json'`, // not require_relative, filtered
	}
	allModules := map[string]bool{
		"lib/models/user": true,
	}
	got := extractRubyImports(lines, "lib/app.rb", "", allModules)

	if len(got) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(got), got)
	}
	if got[0] != "lib/models/user" {
		t.Errorf("expected lib/models/user, got %q", got[0])
	}
}

func TestExtractPHPImports(t *testing.T) {
	lines := []string{
		`use App\Models\User;`,
		`use App\Services\PaymentService;`,
	}
	allModules := map[string]bool{
		`App\Models`:   true,
		`App\Services`: true,
	}
	got := extractPHPImports(lines, "src/Controller.php", "", allModules)

	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
}

func TestExtractCImports(t *testing.T) {
	lines := []string{
		`#include "utils/helper.h"`,
		`#include <stdio.h>`, // system header, not matched by regex
	}
	allModules := map[string]bool{
		"utils/helper.h": true,
	}
	got := extractCImports(lines, "src/main.c", "", allModules)

	if len(got) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(got), got)
	}
	if got[0] != "utils/helper.h" {
		t.Errorf("expected utils/helper.h, got %q", got[0])
	}
}

// --- Module identity tests ---

func TestModuleForFile(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		ext     string
		want    string
	}{
		{"Go package dir", "internal/config/config.go", ".go", "internal/config"},
		{"Go root package", "main.go", ".go", "."},
		{"JS file", "src/utils.js", ".js", "src/utils"},
		{"TS file", "src/app.ts", ".ts", "src/app"},
		{"Python dotted", "myapp/models.py", ".py", "myapp.models"},
		{"Java package", "com/example/service/User.java", ".java", "com.example.service"},
		{"Rust module", "src/config.rs", ".rs", "config"},
		{"Rust main", "src/main.rs", ".rs", "crate"},
		{"Rust lib", "src/lib.rs", ".rs", "crate"},
		{"Ruby file", "lib/models/user.rb", ".rb", "lib/models/user"},
		{"C header", "utils/helper.h", ".h", "utils/helper.h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := moduleForFile(tt.relPath, tt.ext)
			if got != tt.want {
				t.Errorf("moduleForFile(%q, %q) = %q, want %q", tt.relPath, tt.ext, got, tt.want)
			}
		})
	}
}

// --- Tarjan's SCC tests ---

func TestTarjanSCC_SimpleCycle(t *testing.T) {
	graph := importGraph{
		"A": {"B"},
		"B": {"A"},
	}
	sccs := tarjanSCC(graph)

	if len(sccs) != 1 {
		t.Fatalf("expected 1 SCC, got %d", len(sccs))
	}
	if len(sccs[0]) != 2 {
		t.Fatalf("expected SCC of size 2, got %d", len(sccs[0]))
	}
}

func TestTarjanSCC_ThreeNodeCycle(t *testing.T) {
	graph := importGraph{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}
	sccs := tarjanSCC(graph)

	if len(sccs) != 1 {
		t.Fatalf("expected 1 SCC, got %d", len(sccs))
	}
	if len(sccs[0]) != 3 {
		t.Fatalf("expected SCC of size 3, got %d", len(sccs[0]))
	}
}

func TestTarjanSCC_NoCycle(t *testing.T) {
	graph := importGraph{
		"A": {"B"},
		"B": {"C"},
		"C": nil,
	}
	sccs := tarjanSCC(graph)

	if len(sccs) != 0 {
		t.Errorf("expected 0 SCCs for acyclic graph, got %d", len(sccs))
	}
}

func TestTarjanSCC_SelfLoop(t *testing.T) {
	// Self-loops create SCC of size 1, which we filter out.
	graph := importGraph{
		"A": {"A"},
		"B": nil,
	}
	sccs := tarjanSCC(graph)

	if len(sccs) != 0 {
		t.Errorf("expected 0 SCCs (self-loops filtered), got %d", len(sccs))
	}
}

func TestTarjanSCC_DisconnectedComponents(t *testing.T) {
	graph := importGraph{
		"A": {"B"},
		"B": {"A"},
		"C": {"D"},
		"D": {"C"},
		"E": nil, // isolated node
	}
	sccs := tarjanSCC(graph)

	if len(sccs) != 2 {
		t.Fatalf("expected 2 SCCs, got %d", len(sccs))
	}
}

func TestTarjanSCC_MultipleCyclesSharedNode(t *testing.T) {
	// A → B → A and B → C → B form one large SCC {A, B, C}.
	graph := importGraph{
		"A": {"B"},
		"B": {"A", "C"},
		"C": {"B"},
	}
	sccs := tarjanSCC(graph)

	if len(sccs) != 1 {
		t.Fatalf("expected 1 SCC (overlapping cycles merge), got %d", len(sccs))
	}
	if len(sccs[0]) != 3 {
		t.Fatalf("expected SCC of size 3, got %d", len(sccs[0]))
	}
}

// --- Fan-out tests ---

func TestFanOutModules(t *testing.T) {
	graph := importGraph{
		"hub": {"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"},
		"a":   {"b"},
		"b":   nil,
	}
	result := fanOutModules(graph, 10)

	if len(result) != 1 {
		t.Fatalf("expected 1 high-fan-out module, got %d", len(result))
	}
	if count, ok := result["hub"]; !ok || count != 11 {
		t.Errorf("expected hub with count 11, got %v", result)
	}
}

func TestFanOutModules_BelowThreshold(t *testing.T) {
	graph := importGraph{
		"a": {"b", "c"},
		"b": {"c"},
	}
	result := fanOutModules(graph, 10)

	if len(result) != 0 {
		t.Errorf("expected 0 results below threshold, got %d", len(result))
	}
}

func TestFanOutModules_DeduplicatesImports(t *testing.T) {
	graph := importGraph{
		"a": {"b", "b", "b", "c"}, // b listed 3 times, only counts once
	}
	result := fanOutModules(graph, 3)

	if len(result) != 0 {
		t.Errorf("expected 0 results (deduplicated count is 2), got %d", len(result))
	}
}

// --- Confidence scoring tests ---

func TestCycleConfidence(t *testing.T) {
	tests := []struct {
		cycleLen int
		want     float64
	}{
		{2, 0.80},
		{3, 0.75},
		{4, 0.70},
		{5, 0.70},
		{10, 0.70},
	}
	for _, tt := range tests {
		got := cycleConfidence(tt.cycleLen)
		if got != tt.want {
			t.Errorf("cycleConfidence(%d) = %f, want %f", tt.cycleLen, got, tt.want)
		}
	}
}

func TestFanOutConfidence(t *testing.T) {
	tests := []struct {
		count int
		want  float64
	}{
		{9, 0.0}, // below threshold
		{10, 0.40},
		{12, 0.475},
		{14, 0.55},
		{15, 0.55},
		{17, 0.625},
		{19, 0.70},
		{20, 0.70},
		{30, 0.70},
	}
	for _, tt := range tests {
		got := fanOutConfidence(tt.count)
		diff := got - tt.want
		if diff < -0.001 || diff > 0.001 {
			t.Errorf("fanOutConfidence(%d) = %f, want %f", tt.count, got, tt.want)
		}
	}
}

// --- Full Collect() integration tests ---

func TestCouplingCollect_CircularGo(t *testing.T) {
	// Create a temp repo with two Go packages that import each other.
	dir := t.TempDir()

	modPath := "github.com/test/circular"

	// go.mod
	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	// Package A imports package B.
	writeCouplingTestFile(t, dir, "pkga/a.go", `package pkga

import "`+modPath+`/pkgb"

func A() { pkgb.B() }
`)

	// Package B imports package A.
	writeCouplingTestFile(t, dir, "pkgb/b.go", `package pkgb

import "`+modPath+`/pkga"

func B() { pkga.A() }
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var cycleSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "circular-dependency" {
			cycleSignals = append(cycleSignals, s)
		}
	}

	if len(cycleSignals) != 1 {
		t.Fatalf("expected 1 circular-dependency signal, got %d (all signals: %v)", len(cycleSignals), signals)
	}

	sig := cycleSignals[0]
	if sig.Source != "coupling" {
		t.Errorf("expected source 'coupling', got %q", sig.Source)
	}
	if sig.Confidence != 0.80 {
		t.Errorf("expected confidence 0.80, got %f", sig.Confidence)
	}
	if !strings.Contains(sig.Title, "pkga") || !strings.Contains(sig.Title, "pkgb") {
		t.Errorf("expected title to mention both packages, got %q", sig.Title)
	}
}

func TestCouplingCollect_HighCoupling(t *testing.T) {
	// Create a temp repo with one Go package importing many others.
	dir := t.TempDir()

	modPath := "github.com/test/highfan"

	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	// Create 12 leaf packages.
	for i := 0; i < 12; i++ {
		pkgName := string(rune('a' + i))
		writeCouplingTestFile(t, dir, "pkg"+pkgName+"/"+pkgName+".go",
			"package pkg"+pkgName+"\n\nfunc Do() {}\n")
	}

	// Create hub package that imports all 12.
	var imports strings.Builder
	imports.WriteString("package hub\n\nimport (\n")
	for i := 0; i < 12; i++ {
		pkgName := string(rune('a' + i))
		imports.WriteString("\t\"" + modPath + "/pkg" + pkgName + "\"\n")
	}
	imports.WriteString(")\n\nfunc Hub() {\n")
	for i := 0; i < 12; i++ {
		pkgName := string(rune('a' + i))
		imports.WriteString("\tpkg" + pkgName + ".Do()\n")
	}
	imports.WriteString("}\n")

	writeCouplingTestFile(t, dir, "hub/hub.go", imports.String())

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var couplingSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "high-coupling" {
			couplingSignals = append(couplingSignals, s)
		}
	}

	if len(couplingSignals) != 1 {
		t.Fatalf("expected 1 high-coupling signal, got %d", len(couplingSignals))
	}

	sig := couplingSignals[0]
	if !strings.Contains(sig.Title, "hub") {
		t.Errorf("expected title to mention hub module, got %q", sig.Title)
	}
	if sig.Confidence < 0.40 {
		t.Errorf("expected confidence >= 0.40, got %f", sig.Confidence)
	}
}

func TestCouplingCollect_AcyclicGraph(t *testing.T) {
	// Clean repo with no cycles and no high fan-out → no signals.
	dir := t.TempDir()

	modPath := "github.com/test/clean"

	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	writeCouplingTestFile(t, dir, "pkga/a.go", `package pkga

func A() {}
`)

	writeCouplingTestFile(t, dir, "pkgb/b.go", `package pkgb

import "`+modPath+`/pkga"

func B() { pkga.A() }
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) != 0 {
		t.Errorf("expected 0 signals for acyclic graph, got %d: %v", len(signals), signals)
	}
}

func TestCouplingCollect_MinConfidence(t *testing.T) {
	// Create a cycle but set MinConfidence above cycle confidence.
	dir := t.TempDir()

	modPath := "github.com/test/minconf"

	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	writeCouplingTestFile(t, dir, "pkga/a.go", `package pkga

import "`+modPath+`/pkgb"

func A() { pkgb.B() }
`)

	writeCouplingTestFile(t, dir, "pkgb/b.go", `package pkgb

import "`+modPath+`/pkga"

func B() { pkga.A() }
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinConfidence: 0.90, // higher than any cycle confidence
	})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) != 0 {
		t.Errorf("expected 0 signals with MinConfidence 0.90, got %d", len(signals))
	}
}

func TestCouplingCollect_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeCouplingTestFile(t, dir, "main.go", "package main\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := &CouplingCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestCouplingCollect_Metrics(t *testing.T) {
	dir := t.TempDir()

	modPath := "github.com/test/metrics"
	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	writeCouplingTestFile(t, dir, "pkga/a.go", `package pkga

import "`+modPath+`/pkgb"

func A() { pkgb.B() }
`)
	writeCouplingTestFile(t, dir, "pkgb/b.go", `package pkgb

import "`+modPath+`/pkga"

func B() { pkga.A() }
`)

	c := &CouplingCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	m := c.Metrics().(*CouplingMetrics)
	if m.FilesScanned != 2 {
		t.Errorf("expected 2 files scanned, got %d", m.FilesScanned)
	}
	if m.ModulesFound != 2 {
		t.Errorf("expected 2 modules found, got %d", m.ModulesFound)
	}
	if m.CircularDeps != 1 {
		t.Errorf("expected 1 circular dep, got %d", m.CircularDeps)
	}
	if m.SkippedCapExceeded {
		t.Error("expected SkippedCapExceeded=false")
	}
}

func TestCouplingCollect_JSCircular(t *testing.T) {
	dir := t.TempDir()

	writeCouplingTestFile(t, dir, "src/a.js", `import { b } from './b'
export function a() { return b() }
`)
	writeCouplingTestFile(t, dir, "src/b.js", `import { a } from './a'
export function b() { return a() }
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var cycleSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "circular-dependency" {
			cycleSignals = append(cycleSignals, s)
		}
	}

	if len(cycleSignals) != 1 {
		t.Fatalf("expected 1 circular-dependency signal for JS, got %d", len(cycleSignals))
	}
}

func TestCouplingCollect_PythonCircular(t *testing.T) {
	dir := t.TempDir()

	writeCouplingTestFile(t, dir, "myapp/models.py", `from myapp.views import render

class Model:
    pass
`)
	writeCouplingTestFile(t, dir, "myapp/views.py", `from myapp.models import Model

def render():
    pass
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var cycleSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "circular-dependency" {
			cycleSignals = append(cycleSignals, s)
		}
	}

	if len(cycleSignals) != 1 {
		t.Fatalf("expected 1 circular-dependency signal for Python, got %d", len(cycleSignals))
	}
}

func TestCouplingCollect_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	modPath := "github.com/test/exclude"
	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	writeCouplingTestFile(t, dir, "pkga/a.go", `package pkga

import "`+modPath+`/pkgb"

func A() { pkgb.B() }
`)
	writeCouplingTestFile(t, dir, "pkgb/b.go", `package pkgb

import "`+modPath+`/pkga"

func B() { pkga.A() }
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		ExcludePatterns: []string{"pkgb/**"},
	})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// With pkgb excluded, there's no cycle.
	for _, s := range signals {
		if s.Kind == "circular-dependency" {
			t.Errorf("expected no circular-dependency signal when pkgb excluded, got: %v", s)
		}
	}
}

func TestCouplingCollect_SignalTags(t *testing.T) {
	dir := t.TempDir()

	modPath := "github.com/test/tags"
	writeCouplingTestFile(t, dir, "go.mod", "module "+modPath+"\n\ngo 1.21\n")

	writeCouplingTestFile(t, dir, "pkga/a.go", `package pkga

import "`+modPath+`/pkgb"

func A() { pkgb.B() }
`)
	writeCouplingTestFile(t, dir, "pkgb/b.go", `package pkgb

import "`+modPath+`/pkga"

func B() { pkga.A() }
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) == 0 {
		t.Fatal("expected at least 1 signal")
	}

	for _, s := range signals {
		hasArch := false
		hasCoupling := false
		for _, tag := range s.Tags {
			if tag == "architecture" {
				hasArch = true
			}
			if tag == "coupling" {
				hasCoupling = true
			}
		}
		if !hasArch || !hasCoupling {
			t.Errorf("expected tags [architecture, coupling], got %v", s.Tags)
		}
	}
}

func TestCouplingCollect_EmptyRepo(t *testing.T) {
	dir := t.TempDir()

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) != 0 {
		t.Errorf("expected 0 signals for empty repo, got %d", len(signals))
	}
}

func TestCouplingCollect_Name(t *testing.T) {
	c := &CouplingCollector{}
	if c.Name() != "coupling" {
		t.Errorf("expected name 'coupling', got %q", c.Name())
	}
}

// --- Build cycle signal tests ---

func TestBuildCycleSignal_Components(t *testing.T) {
	sig := buildCycleSignal([]string{"A", "B", "C"}, 0.0)
	if sig == nil {
		t.Fatal("expected non-nil signal")
	}
	if sig.Kind != "circular-dependency" {
		t.Errorf("expected kind circular-dependency, got %q", sig.Kind)
	}
	if !strings.Contains(sig.Title, "A → B → C → A") {
		t.Errorf("expected cycle path in title, got %q", sig.Title)
	}
	if sig.Confidence != 0.75 {
		t.Errorf("expected confidence 0.75 for 3-node cycle, got %f", sig.Confidence)
	}
}

func TestBuildCycleSignal_FilteredByMinConfidence(t *testing.T) {
	sig := buildCycleSignal([]string{"A", "B"}, 0.90)
	if sig != nil {
		t.Error("expected nil signal when minConfidence exceeds cycle confidence")
	}
}

func TestBuildFanOutSignal_Components(t *testing.T) {
	sig := buildFanOutSignal("hub", 15, 0.0)
	if sig == nil {
		t.Fatal("expected non-nil signal")
	}
	if sig.Kind != "high-coupling" {
		t.Errorf("expected kind high-coupling, got %q", sig.Kind)
	}
	if !strings.Contains(sig.Title, "hub") {
		t.Errorf("expected module name in title, got %q", sig.Title)
	}
	if !strings.Contains(sig.Title, "15") {
		t.Errorf("expected import count in title, got %q", sig.Title)
	}
}

func TestBuildFanOutSignal_FilteredByMinConfidence(t *testing.T) {
	sig := buildFanOutSignal("hub", 10, 0.90)
	if sig != nil {
		t.Error("expected nil signal when minConfidence exceeds fan-out confidence")
	}
}

// --- readGoModulePath test ---

func TestReadGoModulePath(t *testing.T) {
	dir := t.TempDir()
	writeCouplingTestFile(t, dir, "go.mod", "module github.com/test/proj\n\ngo 1.21\n")

	got := readGoModulePath(dir)
	if got != "github.com/test/proj" {
		t.Errorf("expected github.com/test/proj, got %q", got)
	}
}

func TestReadGoModulePath_Missing(t *testing.T) {
	dir := t.TempDir()
	got := readGoModulePath(dir)
	if got != "" {
		t.Errorf("expected empty string for missing go.mod, got %q", got)
	}
}

// writeCouplingTestFile creates a file in the temp dir with the given content.
func writeCouplingTestFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
