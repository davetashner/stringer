// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package baseline

import "os"

// rename is the function used to atomically move files.
// Override in tests to simulate rename failures.
var rename = os.Rename
