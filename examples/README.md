# Stringer Examples

This directory contains sample configurations and usage scripts for stringer.

## Sample Configurations

### [`go-project.yaml`](go-project.yaml)

Configuration for a Go project. Enables all collectors with sensible defaults for Go codebases.

### [`web-project.yaml`](web-project.yaml)

Configuration for a JavaScript/TypeScript web project. Focuses on TODO scanning, git history, and vulnerability detection.

### [`monorepo-minimal.yaml`](monorepo-minimal.yaml)

Minimal configuration for large repositories. Uses conservative limits to keep scan times fast.

## Usage Scripts

### [`scan-and-import.sh`](scan-and-import.sh)

End-to-end workflow: scan a repo, review output, and import into beads.
