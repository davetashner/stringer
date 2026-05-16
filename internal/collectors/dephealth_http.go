// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"encoding/json"
	"io"
)

// maxRegistryResponseBytes caps the size of a dependency-registry JSON
// response body. A malicious or misconfigured registry could otherwise serve
// an arbitrarily large payload and exhaust memory — the per-ecosystem check
// caps mean up to ~50 such responses could be in flight over a scan.
const maxRegistryResponseBytes = 10 << 20 // 10 MiB

// decodeJSONLimited decodes JSON from body into v, reading at most
// maxRegistryResponseBytes. If the body exceeds the cap the decode fails with
// an unexpected-EOF error rather than allocating without bound.
func decodeJSONLimited(body io.Reader, v any) error {
	return json.NewDecoder(io.LimitReader(body, maxRegistryResponseBytes)).Decode(v)
}
