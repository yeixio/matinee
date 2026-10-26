//go:build !linux

package scan

// networkFS can't tell on this system; set the watch mode to poll for
// folders on network shares.
func networkFS(path string) (string, bool) { return "", false }
