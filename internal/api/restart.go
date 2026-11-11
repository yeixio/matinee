package api

import "net/http"

// Settings → Server health → Restart: an admin restarting the server by
// hand, such as after changing its environment or when something is stuck.

// restartAnswer is POST /admin/restart's answer: the run that's stopping,
// so the page can tell once a new one (another bootId on /health) is up.
type restartAnswer struct {
	BootID string `json:"bootId"`
}

// canRestart is whether this server can restart itself (Server.Restart).
func (s *Server) canRestart() bool { return s.Restart != nil }

// restartServer stops the server the way a deploy does (players hear it's
// restarting and pick up where they were) and starts it again. Asking twice
// restarts it once.
func (s *Server) restartServer(w http.ResponseWriter, r *http.Request) {
	if !s.canRestart() {
		writeError(w, http.StatusConflict, "this server can't restart itself; restart it the way it was started")
		return
	}
	if s.restarting.CompareAndSwap(false, true) {
		s.Log.Info("restarting at an admin's request", "by", currentUser(r).Username)
		// After this answer is on its way: shutting down waits for it.
		go s.Restart()
	}
	writeJSON(w, http.StatusAccepted, restartAnswer{BootID: bootID})
}
