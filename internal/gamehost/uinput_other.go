//go:build !linux

package gamehost

import "errors"

type device struct{}

func newDevice(deviceSpec) (*device, error) {
	return nil, errors.New("virtual controllers, keyboards and mice need Linux for now")
}

func (d *device) send([]event) error { return nil }
func (d *device) close()             {}

func gamePIDs(int) []int { return nil }
func stopGame(int)       {}
