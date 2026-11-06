package update

import "errors"

// Exec isn't offered on Windows, where the tray app installs updates.
func Exec(exe string) error {
	return errors.New("restarting into a new version isn't supported on Windows")
}
