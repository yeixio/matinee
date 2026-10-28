//go:build !wasip1

package plugin

import "errors"

// Outside Matinee (in a plugin's own tests, say) there's no server to ask.

func logAt(level uint32, msg string) {}

func fetch(req Request) (*Response, error) {
	return nil, errors.New("Fetch only works inside Matinee")
}
