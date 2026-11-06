//go:build !unix

package main

import "errors"

// restart isn't needed: only Unix servers replace their own binary.
func restart() error { return errors.New("restarting into an update isn't supported here") }
