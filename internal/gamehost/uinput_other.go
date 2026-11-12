//go:build !linux

package gamehost

import "errors"

type pad struct{ state padState }

func newPad(int) (*pad, error) {
	return nil, errors.New("virtual gamepads need Linux for now")
}

func (p *pad) send([]event) error { return nil }
func (p *pad) close()             {}

func gamePIDs(int) []int { return nil }
func stopGame(int)       {}
