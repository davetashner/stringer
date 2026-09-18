// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"strings"
)

// csharpNeverDeadNames are C# members the runtime, the compiler or a host
// framework calls by convention, so an absent in-repo reference says
// nothing about whether they are used.
var csharpNeverDeadNames = map[string]bool{
	"Main":              true, // entry point
	"Dispose":           true, // IDisposable
	"DisposeAsync":      true, // IAsyncDisposable
	"Finalize":          true, // destructor
	"ToString":          true,
	"Equals":            true,
	"GetHashCode":       true,
	"Configure":         true, // ASP.NET Core Startup convention
	"ConfigureServices": true,
	"Program":           true, // host entry-point classes
	"Startup":           true,
}

// csharpFrameworkAttrs are attribute names whose presence on a declaration
// means a framework discovers it by reflection: test runners (xUnit, NUnit,
// MSTest, BenchmarkDotNet) and ASP.NET Core routing.
var csharpFrameworkAttrs = map[string]bool{
	// Test frameworks.
	"Test": true, "TestCase": true, "TestCaseSource": true, "TestFixture": true,
	"SetUp": true, "TearDown": true, "OneTimeSetUp": true, "OneTimeTearDown": true,
	"Fact": true, "Theory": true,
	"TestMethod": true, "TestClass": true, "TestInitialize": true, "TestCleanup": true,
	"ClassInitialize": true, "ClassCleanup": true,
	"Benchmark": true, "GlobalSetup": true, "GlobalCleanup": true,
	// ASP.NET Core.
	"HttpGet": true, "HttpPost": true, "HttpPut": true, "HttpDelete": true,
	"HttpPatch": true, "HttpHead": true, "HttpOptions": true,
	"Route": true, "ApiController": true, "Controller": true,
}

// csharpAttrName captures each attribute name in an attribute line such as
// `[HttpGet("{id}")]`, `[Fact, Trait("x", "y")]` or `[TestAttribute]`.
var csharpAttrName = regexp.MustCompile(`[\[,]\s*(?:\w+:\s*)?(\w+)`)

// csharpAttrLookback bounds how many lines above a declaration are
// inspected for attributes.
const csharpAttrLookback = 10

// csharpPrivate matches the `private` modifier as a whole word.
var csharpPrivate = regexp.MustCompile(`\bprivate\b`)

// csharpNeverDead reports whether the C# symbol declared on lines[idx]
// should never be flagged: either its name is a runtime/framework
// convention, or an attribute block directly above it (attributes, blank
// lines and comments only) names a test or routing attribute.
func csharpNeverDead(name string, lines []string, idx int) bool {
	if csharpNeverDeadNames[name] {
		return true
	}
	for j := idx - 1; j >= 0 && j >= idx-csharpAttrLookback; j-- {
		trimmed := strings.TrimSpace(lines[j])
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "//"):
			continue
		case strings.HasPrefix(trimmed, "["):
			if csharpLineHasFrameworkAttr(trimmed) {
				return true
			}
		default:
			return false
		}
	}
	return false
}

// csharpLineHasFrameworkAttr reports whether an attribute line names any
// framework attribute, accepting the optional `Attribute` suffix.
func csharpLineHasFrameworkAttr(line string) bool {
	for _, m := range csharpAttrName.FindAllStringSubmatch(line, -1) {
		attr := strings.TrimSuffix(m[1], "Attribute")
		if csharpFrameworkAttrs[attr] {
			return true
		}
	}
	return false
}

// csharpExported reports whether a C# declaration line is visible outside
// its type. Only `private` members are treated as unexported; internal and
// protected members can still be reached from elsewhere in the assembly or
// from subclasses, so they keep the lower public-symbol confidence.
func csharpExported(line string) bool {
	return !csharpPrivate.MatchString(line)
}
