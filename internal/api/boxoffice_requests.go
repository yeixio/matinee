package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/store"
)

// Requests with Box Office: when the app a title goes to is a Box Office
// that takes requests ("requests" in its boxOfficeFeatures), asking for a
// title, from Discover or from search, files it on Box Office's Requests
// page under the person's Matinee account, the one they'd sign in to Box
// Office's request site with. Box Office's rules decide it there: who's
// approved automatically, and weekly limits. People Matinee lets add titles
// without an admin (its admins, or everyone, by the request setting) have
// theirs added straight away.
//
// Matinee keeps its own copy of each request, with Box Office's id, so My
// requests, where titles stand on Discover, and who's told when a title
// arrives work as before. It reads where they stand whenever it reads Box
// Office's notifications, tells the person when one's approved or
// declined, and passes on what its admins decide here. Without such a Box
// Office, requests work exactly as before.

const featureRequests = "requests"

// requestDesk is the Box Office that takes requests for a kind of title,
// if there is one.
func (s *Server) requestDesk(ctx context.Context, kind string) (*arr.Client, bool) {
	app, ok := appFor(kind)
	if !ok {
		return nil, false
	}
	return s.boxOfficeFor(ctx, app, featureRequests)
}

// accountName is the username of the account a user is, or is a profile of.
func (s *Server) accountName(ctx context.Context, u store.User) (string, error) {
	if u.AccountID == 0 {
		return u.Username, nil
	}
	a, err := s.Store.UserByID(ctx, u.AccountID)
	return a.Username, err
}

// requestState is a Box Office request's state, as Matinee keeps it.
func requestState(r arr.BoxOfficeRequest) string {
	switch r.Status {
	case "approved":
		return store.RequestApproved
	case "declined":
		return store.RequestDeclined
	}
	return store.RequestPending
}

// boxOfficeRule is Box Office saying no to something, in words for the
// person who asked, such as a weekly limit.
func boxOfficeRule(err error) (string, bool) {
	var ae *arr.Error
	if errors.As(err, &ae) && ae.Status == http.StatusUnprocessableEntity && ae.Reason != "" {
		return ae.Reason, true
	}
	return "", false
}

// fileWithBoxOffice files a request for a title on Box Office's Requests
// page, and keeps Matinee's copy. A failure is answered on w.
func (s *Server) fileWithBoxOffice(w http.ResponseWriter, r *http.Request, auto bool, desk *arr.Client, kind string, t arr.Title, profileID int) (store.Request, bool) {
	ctx, u := r.Context(), currentUser(r)
	app, _ := appFor(kind)
	name, err := s.accountName(ctx, u)
	if err != nil {
		s.serverError(w, err)
		return store.Request{}, false
	}
	br, err := desk.BoxOfficeAsk(ctx, name, t.TMDBID, auto, profileID)
	if msg, ok := boxOfficeRule(err); ok {
		writeError(w, http.StatusBadRequest, msg)
		return store.Request{}, false
	}
	if err != nil {
		s.arrError(w, r, app, err)
		return store.Request{}, false
	}
	req, err := s.Store.CreateRequest(ctx, store.Request{
		Kind: kind, TMDBID: t.TMDBID, TVDBID: t.TVDBID, Title: t.Title, Year: t.Year, Poster: t.Poster,
		QualityProfileID: profileID, UserID: u.ID, State: requestState(br), BoxOfficeID: br.ID,
	})
	if err != nil {
		s.serverError(w, err)
		return store.Request{}, false
	}
	if req.State == store.RequestApproved {
		s.arrCache.clear() // it may be coming soon now
	}
	return req, true
}

// refreshFromBoxOffice reads where one request stands on Box Office.
func (s *Server) refreshFromBoxOffice(ctx context.Context, cl *arr.Client, req store.Request) (store.Request, error) {
	got, err := cl.BoxOfficeRequests(ctx, []int64{req.BoxOfficeID})
	if err != nil {
		return req, err
	}
	state := store.RequestDeclined // gone from Box Office
	if len(got) > 0 {
		state = requestState(got[0])
	}
	if err := s.Store.UpdateRequest(ctx, req.ID, state, "", req.QualityProfileID); err != nil {
		return req, err
	}
	return s.Store.Request(ctx, req.ID)
}

// approveWithBoxOffice passes an admin's approval here on to Box Office.
// As with Radarr and Sonarr, a failure is kept on the request.
func (s *Server) approveWithBoxOffice(ctx context.Context, req store.Request, profile int) (store.Request, error) {
	app, _ := appFor(req.Kind)
	cl, _, err := s.arrClient(ctx, app)
	if err == nil {
		_, err = cl.BoxOfficeApprove(ctx, req.BoxOfficeID, profile)
		if _, rule := boxOfficeRule(err); rule {
			// Decided on Box Office already, most likely.
			return s.refreshFromBoxOffice(ctx, cl, req)
		}
	}
	state, msg := store.RequestApproved, ""
	if err != nil {
		state, msg = store.RequestFailed, err.Error()
		if errors.Is(err, errNotConnected) {
			msg = app.Name() + " isn't connected"
		}
		s.Log.Warn("approve request on Box Office", "title", req.Title, "err", err)
	} else {
		s.arrCache.clear()
	}
	if err := s.Store.UpdateRequest(ctx, req.ID, state, msg, profile); err != nil {
		return req, err
	}
	return s.Store.Request(ctx, req.ID)
}

// declineWithBoxOffice passes an admin's decline here on to Box Office.
func (s *Server) declineWithBoxOffice(w http.ResponseWriter, r *http.Request, req store.Request) {
	ctx := r.Context()
	app, _ := appFor(req.Kind)
	cl, _, err := s.arrClient(ctx, app)
	if err == nil {
		_, err = cl.BoxOfficeDecline(ctx, req.BoxOfficeID, "")
		if _, rule := boxOfficeRule(err); rule {
			req, err = s.refreshFromBoxOffice(ctx, cl, req)
			if err == nil && req.State != store.RequestDeclined {
				writeError(w, http.StatusConflict, "already approved")
				return
			}
		}
	}
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	if err := s.Store.UpdateRequest(ctx, req.ID, store.RequestDeclined, "", req.QualityProfileID); err != nil {
		s.serverError(w, err)
		return
	}
	req.State = store.RequestDeclined
	writeJSON(w, http.StatusOK, requestView{Request: req})
}

// withdrawFromBoxOffice takes a request someone deleted here off Box
// Office's page, while it's still waiting there.
func (s *Server) withdrawFromBoxOffice(ctx context.Context, req store.Request) {
	if req.BoxOfficeID == 0 || req.State != store.RequestPending {
		return
	}
	app, _ := appFor(req.Kind)
	cl, _, err := s.arrClient(ctx, app)
	if err == nil {
		err = cl.BoxOfficeWithdraw(ctx, req.BoxOfficeID)
	}
	if err != nil {
		s.Log.Warn("take a request back from Box Office", "title", req.Title, "err", err)
	}
}

// syncBoxOfficeRequests catches up with what admins decided on Box
// Office's Requests page, and tells the people who asked.
func (s *Server) syncBoxOfficeRequests(ctx context.Context) {
	for _, kind := range []string{"movie", "show"} {
		cl, ok := s.requestDesk(ctx, kind)
		if !ok {
			continue
		}
		if err := s.syncRequests(ctx, cl, kind); err != nil {
			s.Log.Warn("couldn't read where requests stand on Box Office", "kind", kind, "err", err)
		}
	}
}

func (s *Server) syncRequests(ctx context.Context, cl *arr.Client, kind string) error {
	reqs, err := s.Store.BoxOfficeRequests(ctx, kind)
	if err != nil || len(reqs) == 0 {
		return err
	}
	ids := make([]int64, len(reqs))
	for i, r := range reqs {
		ids[i] = r.BoxOfficeID
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	got, err := cl.BoxOfficeRequests(callCtx, ids)
	cancel()
	if err != nil {
		return err
	}
	byID := map[int64]arr.BoxOfficeRequest{}
	for _, g := range got {
		byID[g.ID] = g
	}
	changed := false
	for _, req := range reqs {
		g, ok := byID[req.BoxOfficeID]
		state := requestState(g)
		if !ok {
			// Taken off Box Office's page: it won't be decided now.
			state = store.RequestDeclined
		}
		if state == store.RequestPending {
			continue
		}
		if err := s.Store.UpdateRequest(ctx, req.ID, state, "", req.QualityProfileID); err != nil {
			return err
		}
		changed = true
		if ok {
			if err := s.tellDecided(ctx, req, state, g.Reply); err != nil {
				return err
			}
		}
	}
	if changed {
		s.arrCache.clear()
		s.discoverCache.clear()
	}
	return nil
}

// tellDecided tells the person who asked that an admin approved or
// declined their request on Box Office.
func (s *Server) tellDecided(ctx context.Context, req store.Request, state, reply string) error {
	title, event := "Your request for "+req.Title+" was approved", "request.approved"
	if state == store.RequestDeclined {
		title, event = "Your request for "+req.Title+" wasn't approved", "request.declined"
	}
	n := store.Notification{
		UserID: req.UserID, Source: "request:" + req.ID + ":" + state, Event: event, Level: store.NotificationInfo,
		Title: title, Body: strings.TrimSpace(reply), Kind: req.Kind, TMDBID: req.TMDBID, TVDBID: req.TVDBID, Poster: req.Poster,
		CreatedAt: time.Now().Unix(),
	}
	saved, added, err := s.Store.AddNotification(ctx, n)
	if err != nil {
		return err
	}
	if added == store.AddedNew {
		s.pushNotification(ctx, req.UserID, s.notificationView(ctx, saved))
	}
	return nil
}
