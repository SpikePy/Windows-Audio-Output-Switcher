// Package icons bundles the application icon into the switcher binary so
// it stays a single self-contained .exe. icon.ico is also used by CI as
// the source for both .exe files' own icon resource (see
// .github/workflows/release.yml).
package icons

import _ "embed"

//go:embed icon.ico
var Icon []byte
