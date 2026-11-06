package update

import "sync/atomic"

var restartTo atomic.Value // string

// RestartWith marks that the server, once it has stopped, should start exe
// in its place (see Exec).
func RestartWith(exe string) { restartTo.Store(exe) }

// RestartPath is the executable the server should start once it has
// stopped, or empty to simply exit.
func RestartPath() string {
	s, _ := restartTo.Load().(string)
	return s
}
