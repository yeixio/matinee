//go:build !wasip1

package plugin

import "errors"

// Outside Matinee (in a plugin's own tests, say) there's no server to ask.
// Package plugintest sets these to answer fetches and collect log lines.
var (
	TestFetch func(Request) (*Response, error)
	TestLog   func(level int, msg string)
)

func logAt(level uint32, msg string) {
	if TestLog != nil {
		TestLog(int(level), msg)
	}
}

func fetch(req Request) (*Response, error) {
	if TestFetch != nil {
		return TestFetch(req)
	}
	return nil, errors.New("Fetch only works inside Matinee; in tests, answer it with plugintest.Network")
}
