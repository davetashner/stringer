// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestExtractCSharpImports(t *testing.T) {
	lines := []string{
		`using System;`,
		`using System.Collections.Generic;`,
		`using MediaBrowser.Controller.Library;`,
		`global using Jellyfin.Data.Enums;`,
		`using static MediaBrowser.Model.Helpers.PathHelper;`,
		`using Alias = Emby.Server.Implementations.Widget;`,
		`using (var stream = File.OpenRead(path))`, // resource statement, not a directive
		`    using var scope = provider.CreateScope();`,
		`using Jellyfin.Api.Nowhere.Deep;`, // parent is one segment up, not two
	}
	allModules := map[string]bool{
		"MediaBrowser.Controller.Library": true,
		"Jellyfin.Data.Enums":             true,
		"MediaBrowser.Model.Helpers":      true,
		"Emby.Server.Implementations":     true,
		"Jellyfin.Api":                    true,
	}

	got := extractCSharpImports(lines, "Jellyfin.Api/Controllers/ItemsController.cs", "", allModules)
	assert.Equal(t, []string{
		"MediaBrowser.Controller.Library",
		"Jellyfin.Data.Enums",
		"MediaBrowser.Model.Helpers",
		"Emby.Server.Implementations",
	}, got)
}

func TestCSharpNamespace(t *testing.T) {
	assert.Equal(t, "Demo.Services", csharpNamespace([]string{"using System;", "", "namespace Demo.Services;", "public class A { }"}))
	assert.Equal(t, "Demo.Services", csharpNamespace([]string{"namespace Demo.Services", "{", "}"}))
	assert.Equal(t, "Demo", csharpNamespace([]string{"namespace Demo {", "namespace Nested {"}), "first declaration wins")
	assert.Equal(t, "", csharpNamespace([]string{"global using System;", "Console.WriteLine();"}))
	assert.Equal(t, "", csharpNamespace([]string{"// namespace Commented.Out;"}))
}

func TestModuleForFile_CSharpFallback(t *testing.T) {
	assert.Equal(t, "Jellyfin.Api.Controllers", moduleForFile("Jellyfin.Api/Controllers/ItemsController.cs", ".cs"))
	assert.Equal(t, ".", moduleForFile("Program.cs", ".cs"))
}

func TestCouplingCollect_CSharpCircular(t *testing.T) {
	dir := t.TempDir()

	// Block-scoped namespace in a directory that does not match its name.
	writeCouplingTestFile(t, dir, "src/Library/LibraryManager.cs", `using System;
using Demo.Providers;

namespace Demo.Library
{
    public class LibraryManager
    {
        public void Refresh(IProvider p) { }
    }
}
`)
	// File-scoped namespace importing back into Demo.Library.
	writeCouplingTestFile(t, dir, "src/Providers/Provider.cs", `using Demo.Library;

namespace Demo.Providers;

public interface IProvider
{
    void Attach(LibraryManager m);
}
`)
	// An entry point with no namespace declaration: directory fallback.
	writeCouplingTestFile(t, dir, "src/Host/Program.cs", `using Demo.Library;
using Demo.Providers;

var m = new LibraryManager();
`)

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	cycles := filterByKind(signals, "circular-dependency")
	require.Len(t, cycles, 1, "expected one Demo.Library <-> Demo.Providers cycle")
	assert.Equal(t, "Circular dependency: Demo.Library → Demo.Providers → Demo.Library", cycles[0].Title)
	assert.Equal(t, "Demo.Library", cycles[0].FilePath)
	assert.InDelta(t, 0.80, cycles[0].Confidence, 0.001)

	m, ok := c.Metrics().(*CouplingMetrics)
	require.True(t, ok)
	assert.Equal(t, 3, m.FilesScanned)
	assert.Equal(t, 3, m.ModulesFound, "two declared namespaces plus the src.Host directory fallback")
	assert.Equal(t, 1, m.CircularDeps)
}

func TestCouplingCollect_CSharpEntryPointExempt(t *testing.T) {
	dir := t.TempDir()

	// Sixteen leaf namespaces and one composition root that imports them
	// all; nothing imports the root, so its fan-out is exempt (DR-025).
	var usings string
	for i := 0; i < 16; i++ {
		ns := "Demo.Leaf" + string(rune('A'+i))
		usings += "using " + ns + ";\n"
		writeCouplingTestFile(t, dir, "src/"+ns+"/Thing.cs", "namespace "+ns+";\npublic class Thing { }\n")
	}
	writeCouplingTestFile(t, dir, "src/Demo.Host/Startup.cs", usings+"\nnamespace Demo.Host;\npublic class Startup { }\n")

	c := &CouplingCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	assert.Empty(t, filterByKind(signals, "high-coupling"))
	m, ok := c.Metrics().(*CouplingMetrics)
	require.True(t, ok)
	assert.Equal(t, 1, m.ExemptedAggregators)
	assert.Equal(t, 0, m.HighCouplingCount)
}
