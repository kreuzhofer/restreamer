//go:build !darwin && !linux

package relay

import "os"

// Hosts without a supported change-time identity must verify content on scan.
func sourceFileIdentity(os.FileInfo) string { return "" }
