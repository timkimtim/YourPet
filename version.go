// Package yourpet exposes the version of the desktop application.
package yourpet

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version returns the semantic version without the release tag's v prefix.
func Version() string {
	return strings.TrimSpace(version)
}
