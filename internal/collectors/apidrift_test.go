// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

func TestAPIDrift_Registration(t *testing.T) {
	c := collector.Get("apidrift")
	require.NotNil(t, c)
	assert.Equal(t, "apidrift", c.Name())
}

func TestAPIDrift_NoSpecFile(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
}
`)
	gitCommit(t, dir, "add handler")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)
	assert.Empty(t, signals, "no spec file means no signals")

	m := c.Metrics().(*APIDriftMetrics)
	assert.Equal(t, 0, m.SpecFilesFound)
}

func TestAPIDrift_YAMLSpec_UndocumentedRoute(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
	http.HandleFunc("/api/orders", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/orders")
	assert.Equal(t, "apidrift", undoc[0].Source)
	assert.Equal(t, 0.6, undoc[0].Confidence)
}

func TestAPIDrift_JSONSpec_UndocumentedRoute(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.json", `{
  "openapi": "3.0.0",
  "paths": {
    "/api/users": {
      "get": {}
    }
  }
}`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
	http.HandleFunc("/api/products", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/products")
}

func TestAPIDrift_UnimplementedRoute(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
  /api/billing:
    get:
      summary: Billing info
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	unimpl := filterByKind(signals, "unimplemented-route")
	require.Len(t, unimpl, 1)
	assert.Contains(t, unimpl[0].Title, "/api/billing")
	assert.Equal(t, 0.5, unimpl[0].Confidence)
}

func TestAPIDrift_ExpressRoutes(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/items:
    get:
      summary: List items
`)
	writeFile(t, dir, "server.js", `const app = require('express')();
app.get('/api/items', (req, res) => {});
app.post('/api/items/import', (req, res) => {});
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/items/import")
}

func TestAPIDrift_PythonRoutes(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/tasks:
    get:
      summary: List tasks
`)
	writeFile(t, dir, "app.py", `from flask import Flask
app = Flask(__name__)

@app.route('/api/tasks')
def list_tasks():
    pass

@app.get('/api/tasks/archive')
def archive():
    pass
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/tasks/archive")
}

func TestAPIDrift_StaleVersion(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /v2/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/v1/users", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-api-version")
	require.Len(t, stale, 1)
	assert.Contains(t, stale[0].Title, "/v1/")
	assert.Contains(t, stale[0].Title, "/v2/")
	assert.Equal(t, 0.7, stale[0].Confidence)
}

func TestAPIDrift_NoStaleWhenVersionsMatch(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /v2/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/v2/users", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-api-version")
	assert.Empty(t, stale)
}

func TestAPIDrift_RouteNormalization_ColonParams(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users/{id}:
    get:
      summary: Get user
`)
	// Gin-style :id param should match spec {id}.
	writeFile(t, dir, "main.go", `package main
func main() {
	r.GET("/api/users/:id", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	assert.Empty(t, undoc, "colon params should match OpenAPI {param} params")

	unimpl := filterByKind(signals, "unimplemented-route")
	assert.Empty(t, unimpl)
}

func TestAPIDrift_RouteNormalization_AngleParams(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/items/{id}:
    get:
      summary: Get item
`)
	// Flask-style <id> param should match spec {id}.
	writeFile(t, dir, "app.py", `
@app.get('/api/items/<id>')
def get_item(id):
    pass
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	assert.Empty(t, undoc, "angle bracket params should match OpenAPI {param} params")
}

func TestAPIDrift_RouteNormalization_TrailingSlash(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users/", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	assert.Empty(t, undoc, "trailing slash should be normalized away")
}

func TestAPIDrift_MinConfidenceFilter(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/orders", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitRoot:       dir,
		MinConfidence: 0.9,
	})
	require.NoError(t, err)
	assert.Empty(t, signals, "all signals should be filtered at min confidence 0.9")
}

func TestAPIDrift_ExcludePatterns(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "vendor/main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/hidden", nil)
}
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitRoot:         dir,
		ExcludePatterns: []string{"vendor"},
	})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	assert.Empty(t, undoc, "vendor routes should be excluded")
}

func TestAPIDrift_ContextCancellation(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", "package main\n")
	gitCommit(t, dir, "add files")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &APIDriftCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{GitRoot: dir})
	assert.Error(t, err)
}

func TestAPIDrift_Metrics(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
  /api/orders:
    get:
      summary: List orders
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
	http.HandleFunc("/api/health", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*APIDriftMetrics)
	require.True(t, ok)

	assert.Equal(t, 1, metrics.SpecFilesFound)
	assert.Equal(t, 2, metrics.RoutesInSpec)
	assert.Equal(t, 2, metrics.RoutesInCode)
	assert.Equal(t, 1, metrics.UndocumentedRoutes)
	assert.Equal(t, 1, metrics.UnimplementedRoutes)
}

func TestAPIDrift_NextJSFileRoutes(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "pages/api/users/index.ts", `export default function handler(req, res) {}`)
	writeFile(t, dir, "pages/api/orders.ts", `export default function handler(req, res) {}`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	// /api/users is in spec and in code (pages/api/users/index.ts).
	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/orders")
}

func TestAPIDrift_NoFalsePositives_NonRouteStrings(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	// String containing "/" but not a route registration.
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	msg := "see /api/docs for info"
	http.HandleFunc("/api/users", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	assert.Empty(t, undoc, "non-route strings should not produce signals")
}

func TestAPIDrift_DjangoPath(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "urls.py", `from django.urls import path
urlpatterns = [
    path('/api/users', views.users),
    path('/api/tasks', views.tasks),
]
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/tasks")
}

func TestAPIDrift_SpecInSubdir(t *testing.T) {
	dir := initAPIDriftRepo(t)

	writeFile(t, dir, "docs/openapi.yaml", `openapi: "3.0.0"
paths:
  /api/users:
    get:
      summary: List users
`)
	writeFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/api/users", nil)
	http.HandleFunc("/api/extra", nil)
}
`)
	gitCommit(t, dir, "add files")

	c := &APIDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	undoc := filterByKind(signals, "undocumented-route")
	require.Len(t, undoc, 1)
	assert.Contains(t, undoc[0].Title, "/api/extra")
}

// initAPIDriftRepo creates a temporary git repo with an initial commit.
func initAPIDriftRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runDocGit(t, dir, "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0o600))
	runDocGit(t, dir, "add", ".")
	runDocGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", "init")
	return dir
}
