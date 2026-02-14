// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCsprojDeps_AttributeVersion(t *testing.T) {
	data := []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "NuGet", queries[0].Ecosystem)
	assert.Equal(t, "Newtonsoft.Json", queries[0].Name)
	assert.Equal(t, "13.0.1", queries[0].Version)
}

func TestParseCsprojDeps_ChildElementVersion(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="Foo">
      <Version>1.0.0</Version>
    </PackageReference>
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "Foo", queries[0].Name)
	assert.Equal(t, "1.0.0", queries[0].Version)
}

func TestParseCsprojDeps_MultipleDeps(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
    <PackageReference Include="Serilog" Version="3.1.0" />
    <PackageReference Include="Dapper" Version="2.1.35" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 3)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}
	assert.Equal(t, "13.0.1", names["Newtonsoft.Json"])
	assert.Equal(t, "3.1.0", names["Serilog"])
	assert.Equal(t, "2.1.35", names["Dapper"])
}

func TestParseCsprojDeps_MultipleItemGroups(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
  </ItemGroup>
  <ItemGroup>
    <PackageReference Include="Serilog" Version="3.1.0" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)
}

func TestParseCsprojDeps_EmptyVersion(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="Foo" Version="" />
    <PackageReference Include="Bar" Version="1.0.0" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "Bar", queries[0].Name)
}

func TestParseCsprojDeps_NoInclude(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Version="1.0.0" />
    <PackageReference Include="Bar" Version="2.0.0" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "Bar", queries[0].Name)
}

func TestParseCsprojDeps_NoPackageRefs(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <Reference Include="System.Web" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	assert.Nil(t, queries)
}

func TestParseCsprojDeps_EmptyProject(t *testing.T) {
	data := []byte(`<Project></Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	assert.Nil(t, queries)
}

func TestParseCsprojDeps_MalformedXML(t *testing.T) {
	data := []byte(`<Project><ItemGroup><PackageReference`)

	queries, err := parseCsprojDeps(data)
	assert.Error(t, err)
	assert.Nil(t, queries)
}

func TestParseCsprojDeps_RealWorld(t *testing.T) {
	data := []byte(`<Project Sdk="Microsoft.NET.Sdk.Web">

  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <Nullable>enable</Nullable>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>

  <ItemGroup>
    <PackageReference Include="Microsoft.AspNetCore.OpenApi" Version="8.0.0" />
    <PackageReference Include="Swashbuckle.AspNetCore" Version="6.4.0" />
  </ItemGroup>

  <ItemGroup>
    <PackageReference Include="Serilog.AspNetCore" Version="8.0.0" />
    <PackageReference Include="Npgsql.EntityFrameworkCore.PostgreSQL" Version="8.0.0" />
  </ItemGroup>

</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 4)

	names := make(map[string]bool)
	for _, q := range queries {
		names[q.Name] = true
		assert.Equal(t, "NuGet", q.Ecosystem)
	}
	assert.True(t, names["Microsoft.AspNetCore.OpenApi"])
	assert.True(t, names["Swashbuckle.AspNetCore"])
	assert.True(t, names["Serilog.AspNetCore"])
	assert.True(t, names["Npgsql.EntityFrameworkCore.PostgreSQL"])
}

func TestParseCsprojDeps_DuplicatePackage(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
  </ItemGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1, "duplicate package should be deduplicated")
	assert.Equal(t, "Newtonsoft.Json", queries[0].Name)
	assert.Equal(t, "13.0.1", queries[0].Version, "first occurrence wins")
}

func TestParseCsprojDeps_MixedVersionStyles(t *testing.T) {
	data := []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
    <PackageReference Include="Serilog">
      <Version>3.1.0</Version>
    </PackageReference>
  </ItemGroup>
</Project>`)

	queries, err := parseCsprojDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}
	assert.Equal(t, "13.0.1", names["Newtonsoft.Json"])
	assert.Equal(t, "3.1.0", names["Serilog"])
}
