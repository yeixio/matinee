package api

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subsync"
	"github.com/yeixio/matinee/internal/subtitle"
)

// Subtitles that show too early, too late or drift are lined up with the
// video's audio (see internal/subsync). Subtitle files (next to the video,
// or found online) are synced on their own the first time someone plays
// the video with them, unless an admin turned that off; anyone grown up
// can sync any text track from the player, or undo a sync. What a sync
// found is kept per video and track, and the track is served with its
// times moved from then on, to every app.

const (
	settingSubtitleSync = "subtitleSync" // "off" stops automatic syncing
	syncTimeout         = 20 * time.Minute
	// minSyncShiftMs is the smallest move worth making: less is as good as in sync.
	minSyncShiftMs = 100
)

// Sync states, as in subtitle.Sync.
const (
	syncSyncing = "syncing"
	syncSynced  = "synced"
	syncInSync  = "inSync"
	syncUnsure  = "unsure"
	syncFailed  = "failed"
	syncOff     = "off"
)

type syncJob struct {
	item   int64
	track  string
	user   int64
	manual bool
}

// subSyncs runs syncs one at a time in the background, the ones people
// asked for first.
type subSyncs struct {
	once    sync.Once
	mu      sync.Mutex
	pending map[string]bool // item|track, queued or running
	manual  chan syncJob
	auto    chan syncJob
}

func syncJobKey(item int64, track string) string { return fmt.Sprintf("%d|%s", item, track) }

func (s *Server) startSyncs() {
	s.syncs.once.Do(func() {
		s.syncs.pending = map[string]bool{}
		s.syncs.manual = make(chan syncJob, 64)
		s.syncs.auto = make(chan syncJob, 256)
		ctx := s.BackgroundCtx
		if ctx == nil {
			ctx = context.Background()
		}
		go s.syncWorker(ctx)
	})
}

func (s *Server) syncWorker(ctx context.Context) {
	for {
		var j syncJob
		select {
		case j = <-s.syncs.manual:
		default:
			select {
			case <-ctx.Done():
				return
			case j = <-s.syncs.manual:
			case j = <-s.syncs.auto:
			}
		}
		jctx, cancel := context.WithTimeout(ctx, syncTimeout)
		if err := s.runSync(jctx, j); err != nil && ctx.Err() == nil {
			s.Log.Warn("subtitle sync", "item", j.item, "track", j.track, "err", err)
		}
		cancel()
		s.syncs.mu.Lock()
		delete(s.syncs.pending, syncJobKey(j.item, j.track))
		s.syncs.mu.Unlock()
	}
}

// queueSync asks for a track to be synced, and reports whether it's queued
// or running now.
func (s *Server) queueSync(j syncJob) bool {
	s.startSyncs()
	k := syncJobKey(j.item, j.track)
	s.syncs.mu.Lock()
	defer s.syncs.mu.Unlock()
	if s.syncs.pending[k] {
		return true
	}
	ch := s.syncs.auto
	if j.manual {
		ch = s.syncs.manual
	}
	select {
	case ch <- j:
		s.syncs.pending[k] = true
		return true
	default:
		return false // a long queue: it's asked for again next time
	}
}

func (s *Server) syncPending(item int64, track string) bool {
	s.syncs.mu.Lock()
	defer s.syncs.mu.Unlock()
	return s.syncs.pending[syncJobKey(item, track)]
}

// syncKey is a track's file and the video as they are now: a sync of
// either as they were before no longer counts.
func syncKey(it store.Item, t subtitle.Track) string {
	stamp := func(p string) string {
		info, err := statOnShare(p)
		if err != nil {
			return "?"
		}
		return fmt.Sprintf("%d.%d", info.Size(), info.ModTime().Unix())
	}
	sub := "embedded"
	switch {
	case t.Found:
		sub = "found" // found files never change; a new one has a new id
	case t.External:
		sub = stamp(t.Path)
	}
	return stamp(it.Path) + "|" + sub
}

// canSync says whether a track has times to line up.
func canSync(t subtitle.Track) bool { return !t.BurnIn }

// autoSyncs says whether subtitle files are synced without anyone asking.
func (s *Server) autoSyncs(ctx context.Context) bool {
	v, _ := s.Store.Setting(ctx, settingSubtitleSync)
	return v != "off"
}

// applySyncs fills in each track's sync and the shift it's served with.
func (s *Server) applySyncs(ctx context.Context, it store.Item, tracks []subtitle.Track) {
	if len(tracks) == 0 {
		return
	}
	rows, err := s.Store.SubtitleSyncs(ctx, it.ID)
	if err != nil {
		s.Log.Warn("subtitle syncs", "item", it.ID, "err", err)
	}
	byTrack := map[string]store.SubtitleSync{}
	for _, r := range rows {
		byTrack[r.Track] = r
	}
	for i := range tracks {
		t := &tracks[i]
		if !canSync(*t) {
			continue
		}
		if r, ok := byTrack[t.ID]; ok && r.Key == syncKey(it, *t) {
			t.Sync = &subtitle.Sync{State: r.State, Auto: r.Auto, Reference: r.Reference}
			if r.State == syncSynced {
				t.Shift = subsync.Shift{OffsetMs: r.OffsetMs, Rate: r.Rate}
				if t.Shift.Rate == 1 {
					t.Shift.Rate = 0
				}
				t.Sync.OffsetMs, t.Sync.Rate = t.Shift.OffsetMs, t.Shift.Rate
			}
		}
		if s.syncPending(it.ID, t.ID) {
			if t.Sync == nil {
				t.Sync = &subtitle.Sync{}
			}
			t.Sync.State = syncSyncing
		}
	}
}

// autoSync queues the subtitle files among tracks that were never synced
// as they are now. only, when set, limits it to that track.
func (s *Server) autoSync(ctx context.Context, user int64, it store.Item, tracks []subtitle.Track, only string) {
	if !s.autoSyncs(ctx) {
		return
	}
	for i := range tracks {
		t := &tracks[i]
		if !t.External || !canSync(*t) || t.Sync != nil || only != "" && t.ID != only {
			continue
		}
		if s.queueSync(syncJob{item: it.ID, track: t.ID, user: user}) {
			t.Sync = &subtitle.Sync{State: syncSyncing}
		}
	}
}

// runSync lines one track up and keeps what it found.
func (s *Server) runSync(ctx context.Context, j syncJob) error {
	it, err := s.Store.Item(ctx, j.user, j.item)
	if err != nil {
		return err
	}
	tracks := s.subtitleTracks(ctx, it)
	t, ok := subtitle.Find(tracks, j.track)
	if !ok || !canSync(t) {
		return nil
	}
	key := syncKey(it, t)
	if !j.manual {
		if r, err := s.Store.SubtitleSyncFor(ctx, it.ID, t.ID); err == nil && r.Key == key {
			return nil // synced (or undone) meanwhile
		}
	}
	row := store.SubtitleSync{ItemID: it.ID, Track: t.ID, Key: key, Auto: !j.manual}
	save := func(state string, err error) error {
		row.State = state
		if serr := s.Store.SetSubtitleSync(context.WithoutCancel(ctx), row); serr != nil {
			return serr
		}
		return err
	}
	if t.Found {
		if err := s.ensureFoundFile(ctx, it, t); err != nil {
			return save(syncFailed, err)
		}
	}
	data, err := s.Subtitles.VTT(it.Path, t, tracks)
	if err != nil {
		return save(syncFailed, err)
	}
	cues := subsync.ParseVTT(data)
	ref, refName, err := s.syncReference(ctx, it, t, tracks)
	if err != nil {
		if ctx.Err() != nil {
			return err // a restart or a timeout: try again another time
		}
		return save(syncFailed, err)
	}
	row.Reference = refName
	res, err := subsync.Align(ref, cues)
	if err != nil {
		return save(syncFailed, err)
	}
	row.OffsetMs, row.Rate, row.Score = res.OffsetMs, res.Rate, res.Score
	state := syncSynced
	switch {
	case !res.Sure:
		state = syncUnsure
	case res.Rate == 0 && abs64(res.OffsetMs) < minSyncShiftMs:
		state = syncInSync
	}
	s.Log.Info("subtitle sync", "item", it.ID, "track", t.ID, "state", state, "offsetMs", res.OffsetMs, "rate", res.Rate, "score", res.Score, "reference", refName)
	return save(state, nil)
}

// syncReference is what a track is lined up with: another subtitle track
// inside the video when one is at hand (its times are right, and lines
// match lines better than speech does), else the speech in the audio.
func (s *Server) syncReference(ctx context.Context, it store.Item, t subtitle.Track, tracks []subtitle.Track) (subsync.Reference, string, error) {
	if t.External {
		var best []subsync.Cue
		bestSpoken := 0
		for _, o := range tracks {
			if o.External || o.BurnIn || o.Forced || !s.Subtitles.HasVTT(it.Path, o) {
				continue
			}
			data, err := s.Subtitles.VTT(it.Path, o, tracks)
			if err != nil {
				continue
			}
			cues := subsync.ParseVTT(data)
			if n := spoken(cues); n > bestSpoken {
				best, bestSpoken = cues, n
			}
		}
		if bestSpoken >= 100 {
			data, _ := s.Subtitles.VTT(it.Path, t, tracks)
			if bestSpoken*10 >= spoken(subsync.ParseVTT(data))*4 {
				return subsync.CueReference(best), "subtitles", nil
			}
		}
	}
	sp, err := s.speech(ctx, it, t.Language)
	if err != nil {
		return nil, "", err
	}
	return subsync.SpeechReference(sp), "audio", nil
}

func spoken(cues []subsync.Cue) int {
	n := 0
	for _, c := range cues {
		if !c.Sound {
			n++
		}
	}
	return n
}

// speech maps where people talk in a video, listening to the audio in the
// subtitles' language when the video has it. Maps are kept with the
// converted subtitles, so a video's other tracks sync at once.
func (s *Server) speech(ctx context.Context, it store.Item, language string) (subsync.Speech, error) {
	a := subsync.Audio{}
	if raw, _, ok, err := s.Store.MediaInfo(ctx, it.ID, probe.MediaInfoVersion); err == nil && ok {
		var mi probe.MediaInfo
		if json.Unmarshal([]byte(raw), &mi) == nil {
			if as, ok := pickSyncAudio(mi.Audio, language); ok {
				a = subsync.Audio{Stream: as.Index, Center: as.Channels >= 5}
			}
		}
	}
	info, err := os.Stat(it.Path)
	if err != nil {
		return nil, err
	}
	var duration int64
	if it.DurationMs != nil {
		duration = *it.DurationMs
	}
	windows := subsync.Windows(duration)
	sum := sha1.Sum(fmt.Appendf(nil, "%s|%d|%d|%d|%v|%v", it.Path, info.Size(), info.ModTime().UnixNano(), a.Stream, a.Center, windows))
	cache := filepath.Join(s.Subtitles.Dir, "speech", hex.EncodeToString(sum[:])+".bin")
	if b, err := os.ReadFile(cache); err == nil && len(b) > 0 {
		return subsync.Speech(b), nil
	}
	sp, err := subsync.Listen(ctx, s.Subtitles.FFmpeg, it.Path, a, windows, probe.LowerPriority)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err == nil {
		if os.WriteFile(cache+".tmp", sp, 0o644) == nil {
			os.Rename(cache+".tmp", cache)
		}
	}
	return sp, nil
}

// pickSyncAudio picks the audio to listen to: the main track in the
// subtitles' language, else the default, else the first; never a
// commentary or audio description, whose talking is somewhere else.
func pickSyncAudio(streams []probe.AudioStream, language string) (probe.AudioStream, bool) {
	var main []probe.AudioStream
	for _, a := range streams {
		if !a.Commentary && !a.Descriptive {
			main = append(main, a)
		}
	}
	if len(main) == 0 {
		return probe.AudioStream{}, false
	}
	if language != "" {
		for _, a := range main {
			if subtitle.LanguageCode(a.Language) == language {
				return a, true
			}
		}
	}
	for _, a := range main {
		if a.Default {
			return a, true
		}
	}
	return main[0], true
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// ---- endpoints ----

// syncTrack finds the track a sync request is about.
func (s *Server) syncTrack(w http.ResponseWriter, r *http.Request) (store.Item, subtitle.Track, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, subtitle.Track{}, false
	}
	if !it.Playable || it.VideoCodec == "" {
		writeError(w, http.StatusBadRequest, "not a video")
		return it, subtitle.Track{}, false
	}
	t, ok := subtitle.Find(s.subtitleTracks(r.Context(), it), r.PathValue("track"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such subtitle track")
		return it, t, false
	}
	if !canSync(t) {
		writeError(w, http.StatusUnprocessableEntity, "picture subtitles can't be synced")
		return it, t, false
	}
	return it, t, true
}

func syncView(t subtitle.Track) subtitle.Sync {
	if t.Sync == nil {
		return subtitle.Sync{State: ""}
	}
	return *t.Sync
}

// getSubtitleSync says where a track's sync is: players ask again every
// few seconds while it's syncing.
func (s *Server) getSubtitleSync(w http.ResponseWriter, r *http.Request) {
	_, t, ok := s.syncTrack(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, syncView(t))
}

// syncSubtitle lines a track up with the audio now, whatever was done
// before.
func (s *Server) syncSubtitle(w http.ResponseWriter, r *http.Request) {
	it, t, ok := s.syncTrack(w, r)
	if !ok {
		return
	}
	if !s.queueSync(syncJob{item: it.ID, track: t.ID, user: currentUser(r).ID, manual: true}) {
		writeError(w, http.StatusServiceUnavailable, "too many subtitles are syncing; try again in a minute")
		return
	}
	v := syncView(t)
	v.State = syncSyncing
	writeJSON(w, http.StatusAccepted, v)
}

// unsyncSubtitle undoes a track's sync: it's shown as written, and
// automatic syncing leaves it alone until the file changes.
func (s *Server) unsyncSubtitle(w http.ResponseWriter, r *http.Request) {
	it, t, ok := s.syncTrack(w, r)
	if !ok {
		return
	}
	if err := s.Store.SetSubtitleSync(r.Context(), store.SubtitleSync{ItemID: it.ID, Track: t.ID, Key: syncKey(it, t), State: syncOff}); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subtitle.Sync{State: syncOff})
}
