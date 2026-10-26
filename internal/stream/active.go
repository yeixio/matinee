package stream

// Active is how many transcodes are running, so background work can wait
// until nobody is watching.
func (t *Transcoder) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}
