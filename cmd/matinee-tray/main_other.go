//go:build !windows

// Command matinee-tray is the Windows app that runs the Matinee server from
// the notification area. On Linux and macOS, run the server itself.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "matinee-tray runs on Windows; run the matinee server here instead")
	os.Exit(1)
}
