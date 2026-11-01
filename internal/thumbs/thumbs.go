// Package thumbs makes the pictures the players show above the seek bar
// while someone scrubs: a small frame every few seconds of each video,
// packed into JPEG sheets of Across by Down frames so a player loads a
// handful of images rather than hundreds.
//
// FFmpeg reads only the video's keyframes, at the lowest CPU and disk
// priority. For most files it reads the file once, front to back; for
// files so big that would mean reading many megabytes per picture (4K
// remuxes on a network share), it jumps to each point instead. HDR video
// is tone mapped, so its pictures don't look washed out.
//
// The sheets live in the data folder, one folder per video, and the
// database says how they're laid out (see store.Thumbnails).
package thumbs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Version goes up when the pictures change enough that videos should get
// new ones.
const Version = 1

// Sizes and timings, variables so tests can change them.
var (
	Width  = 240 // of one picture; the height follows the video's shape
	Across = 10  // pictures across one sheet
	Down   = 10  // and down
	// One picture every interval: about this many for the whole video, but
	// never more often than every minInterval or less than every maxInterval.
	pictures    = 200
	minInterval = 2 * time.Second
	maxInterval = 10 * time.Second
	quality     = 70
	// Above this many bytes of file per picture, jumping to each point
	// reads less than reading the whole file.
	seekAbove     = int64(8 << 20)
	fileTimeout   = 30 * time.Minute // to read a whole file
	frameTimeout  = 30 * time.Second // to jump to one point
	retryTimedOut = time.Hour
	batch         = 200
)

// Mode is when pictures are made, an admin setting.
type Mode string

const (
	// All makes pictures of every video in the background, after the media
	// analysis. The default.
	All Mode = "all"
	// OnPlay makes a video's pictures the first time someone plays it.
	OnPlay Mode = "onPlay"
	// Off makes none, and players show no pictures.
	Off Mode = "off"
)

const settingMode = "thumbnails"

// Interval is how far apart a video's pictures are.
func Interval(durationMs int64) time.Duration {
	d := (time.Duration(durationMs) * time.Millisecond / time.Duration(pictures)).Round(time.Second)
	return min(max(d, minInterval), maxInterval)
}

// Generator makes the pictures and keeps track of them.
type Generator struct {
	Store  *store.Store
	FFmpeg string
	Dir    string
	Log    *slog.Logger
	// LowPriority is called with each FFmpeg's process id.
	LowPriority func(pid int)

	mu         sync.Mutex
	mode       Mode
	loaded     bool
	retryAfter map[int64]time.Time
	making     map[int64]*sync.Mutex
	requested  map[int64]bool
	requests   chan int64
}

func (g *Generator) init() {
	if g.requests == nil {
		g.retryAfter = map[int64]time.Time{}
		g.making = map[int64]*sync.Mutex{}
		g.requested = map[int64]bool{}
		g.requests = make(chan int64, 64)
	}
}

// Mode returns the admin's setting, All unless they changed it.
func (g *Generator) Mode(ctx context.Context) (Mode, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.loaded {
		v, err := g.Store.Setting(ctx, settingMode)
		if err != nil {
			return All, err
		}
		g.mode, g.loaded = Mode(v), true
		if g.mode != OnPlay && g.mode != Off {
			g.mode = All
		}
	}
	return g.mode, nil
}

func (g *Generator) SetMode(ctx context.Context, m Mode) error {
	if m != All && m != OnPlay && m != Off {
		return fmt.Errorf("thumbnails must be %s, %s or %s", All, OnPlay, Off)
	}
	if err := g.Store.SetSetting(ctx, settingMode, string(m)); err != nil {
		return err
	}
	g.mu.Lock()
	g.mode, g.loaded = m, true
	g.mu.Unlock()
	return nil
}

// Key is what a video's pictures are stored under, see store.MediaKey.
func Key(f store.ThumbnailFile) string { return store.MediaKey(Version, f.Size, f.Mtime) }

// Done says whether a video's pictures are made, as it is now, and still
// there: a data folder that was emptied, as on a server whose data folder
// isn't kept, means making them again.
func (g *Generator) Done(f store.ThumbnailFile) bool {
	if f.Key != Key(f) {
		return false
	}
	if f.Count == 0 {
		return true // couldn't be read; tried again when the file changes
	}
	_, err := os.Stat(g.SheetPath(f.ID, 0))
	return err == nil
}

// SheetPath is where a video's nth sheet is.
func (g *Generator) SheetPath(id int64, n int) string {
	return filepath.Join(g.Dir, strconv.FormatInt(id, 10), strconv.Itoa(n)+".jpg")
}

// Request has a video's pictures made soon, ahead of the background pass,
// because someone is playing it. It reports whether they're on their way.
func (g *Generator) Request(ctx context.Context, id int64) bool {
	if mode, _ := g.Mode(ctx); mode == Off {
		return false
	}
	g.mu.Lock()
	g.init()
	if g.requested[id] {
		g.mu.Unlock()
		return true
	}
	if t, ok := g.retryAfter[id]; ok && time.Now().Before(t) {
		g.mu.Unlock()
		return false
	}
	select {
	case g.requests <- id:
		g.requested[id] = true
	default:
		g.mu.Unlock()
		return false // plenty to do already
	}
	g.mu.Unlock()
	return true
}

// Serve makes the pictures Request asks for, one video at a time, until
// ctx ends.
func (g *Generator) Serve(ctx context.Context) {
	g.mu.Lock()
	g.init()
	g.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-g.requests:
			f, err := g.Store.ThumbnailFile(ctx, id)
			if err == nil && !g.Done(f) {
				g.Make(ctx, f)
			}
			g.mu.Lock()
			delete(g.requested, id)
			g.mu.Unlock()
		}
	}
}

// Pass makes the pictures of every video that needs them, in id order.
// Before each it calls next, which returns false to stop the pass (paused,
// or shutting down). It returns how many videos it made pictures of.
func (g *Generator) Pass(ctx context.Context, next func(f store.ThumbnailFile) bool) (int, error) {
	g.mu.Lock()
	g.init()
	g.mu.Unlock()
	g.Clean(ctx)
	n := 0
	var after int64
	for {
		files, err := g.Store.ThumbnailFiles(ctx, after, batch)
		if err != nil || len(files) == 0 {
			return n, err
		}
		for _, f := range files {
			after = f.ID
			if g.Done(f) || g.retryLater(f.ID) {
				continue
			}
			if !next(f) || ctx.Err() != nil {
				return n, ctx.Err()
			}
			if g.Make(ctx, f) {
				n++
			}
		}
	}
}

// Clean deletes the pictures of videos that are gone, and anything left
// over from making pictures when the server stopped.
func (g *Generator) Clean(ctx context.Context) {
	entries, err := os.ReadDir(g.Dir)
	if err != nil {
		return
	}
	ids, err := g.Store.ThumbnailIDs(ctx)
	if err != nil {
		return
	}
	for _, e := range entries {
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !ids[id] {
			g.mu.Lock()
			busy := g.making[id] != nil
			g.mu.Unlock()
			if !busy {
				os.RemoveAll(filepath.Join(g.Dir, e.Name()))
			}
		}
	}
}

func (g *Generator) retryLater(id int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.retryAfter[id]
	if ok && time.Now().After(t) {
		delete(g.retryAfter, id)
		return false
	}
	return ok
}

// lock keeps two makers off one video: the background pass and someone
// pressing play.
func (g *Generator) lock(id int64) func() {
	g.mu.Lock()
	g.init()
	m, ok := g.making[id]
	if !ok {
		m = &sync.Mutex{}
		g.making[id] = m
	}
	g.mu.Unlock()
	m.Lock()
	return func() {
		m.Unlock()
		g.mu.Lock()
		delete(g.making, id)
		g.mu.Unlock()
	}
}

// Make makes one video's pictures and stores them. It reports whether it
// stored anything.
func (g *Generator) Make(ctx context.Context, f store.ThumbnailFile) bool {
	unlock := g.lock(f.ID)
	defer unlock()
	if cur, err := g.Store.ThumbnailFile(ctx, f.ID); err != nil || g.Done(cur) {
		return false // gone, or made while this waited
	} else {
		f = cur
	}
	t, err := g.make(ctx, f)
	switch {
	case ctx.Err() != nil:
		return false
	case errors.Is(err, context.DeadlineExceeded):
		g.Log.Warn("thumbnails: ffmpeg timed out", "path", f.Path)
		g.mu.Lock()
		g.retryAfter[f.ID] = time.Now().Add(retryTimedOut)
		g.mu.Unlock()
		return false
	case err != nil:
		g.Log.Warn("thumbnails: can't read video", "path", f.Path, "err", err)
		t = store.Thumbnails{} // stored as unreadable, until the file changes
	}
	if err := g.Store.SetThumbnails(ctx, f.ID, t, Key(f)); err != nil {
		g.Log.Warn("thumbnails: save", "item", f.ID, "err", err)
		return false
	}
	return true
}

// make reads the video's frames and writes its sheets.
func (g *Generator) make(ctx context.Context, f store.ThumbnailFile) (store.Thumbnails, error) {
	if f.DurationMs <= 0 {
		return store.Thumbnails{}, errors.New("its length isn't known")
	}
	interval := Interval(f.DurationMs)
	count := int((time.Duration(f.DurationMs)*time.Millisecond + interval - 1) / interval)
	// Write next to the old ones, then swap, so a player never gets half a set.
	dir := filepath.Join(g.Dir, strconv.FormatInt(f.ID, 10))
	tmp := dir + ".new"
	try := func(tonemap string) (store.Thumbnails, error) {
		os.RemoveAll(tmp)
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return store.Thumbnails{}, err
		}
		sh := &sheets{dir: tmp, count: count}
		var err error
		if f.Size/int64(count) > seekAbove {
			err = g.jumping(ctx, f.Path, interval, count, tonemap, sh.add)
		} else {
			err = g.reading(ctx, f.Path, interval, count, tonemap, sh.add)
		}
		if err == nil {
			err = sh.finish()
		}
		if err != nil {
			return store.Thumbnails{}, err
		}
		return store.Thumbnails{IntervalMs: interval.Milliseconds(), Width: sh.w, Height: sh.h, Columns: Across, Rows: Down, Count: count}, nil
	}
	tonemap := toneMap(f.MediaInfo)
	t, err := try(tonemap)
	if err != nil && tonemap != "" && ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded) {
		t, err = try("") // an FFmpeg without zscale
	}
	if err != nil {
		os.RemoveAll(tmp)
		return store.Thumbnails{}, err
	}
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return store.Thumbnails{}, err
	}
	return t, nil
}

// sheets lays the pictures out as they come, left to right and then down,
// and writes each sheet as soon as it's full, so a long video's frames
// aren't all held at once.
type sheets struct {
	dir   string
	count int
	w, h  int
	n     int // pictures so far
	img   *image.RGBA
	last  *frame
}

func (s *sheets) add(fr *frame) error {
	if s.n >= s.count {
		return nil
	}
	if s.img == nil {
		if s.w == 0 {
			s.w, s.h = fr.w, fr.h
		}
		s.img = image.NewRGBA(image.Rect(0, 0, Across*s.w, Down*s.h))
	}
	k := s.n % (Across * Down)
	x0, y0 := (k%Across)*s.w, (k/Across)*s.h
	for y := range min(fr.h, s.h) {
		row := fr.rgb[y*fr.w*3:]
		o := s.img.PixOffset(x0, y0+y)
		for x := range min(fr.w, s.w) {
			s.img.Pix[o+x*4] = row[x*3]
			s.img.Pix[o+x*4+1] = row[x*3+1]
			s.img.Pix[o+x*4+2] = row[x*3+2]
			s.img.Pix[o+x*4+3] = 255
		}
	}
	s.last = fr
	s.n++
	if s.n%(Across*Down) == 0 || s.n == s.count {
		return s.flush()
	}
	return nil
}

func (s *sheets) flush() error {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, s.img, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	sheet := (s.n - 1) / (Across * Down)
	s.img = nil
	return os.WriteFile(filepath.Join(s.dir, strconv.Itoa(sheet)+".jpg"), b.Bytes(), 0o644)
}

// finish fills in pictures missing at the end (the file's last keyframe
// came early) with the last one.
func (s *sheets) finish() error {
	if s.last == nil {
		return errors.New("no pictures came out")
	}
	for s.n < s.count {
		if err := s.add(s.last); err != nil {
			return err
		}
	}
	return nil
}

// toneMap returns the FFmpeg filters that bring an HDR video's colours
// into SDR, or "" for SDR video (and for Dolby Vision without an HDR10 or
// HLG layer, which these filters can't read).
func toneMap(mediaInfo string) string {
	var mi struct {
		Video []struct {
			HDR     string `json:"hdr"`
			HDRBase string `json:"hdrBase"`
		} `json:"video"`
	}
	if mediaInfo == "" || json.Unmarshal([]byte(mediaInfo), &mi) != nil || len(mi.Video) == 0 {
		return ""
	}
	hdr := mi.Video[0].HDR
	if hdr == "dolby-vision" {
		hdr = mi.Video[0].HDRBase
	}
	var transfer string
	switch hdr {
	case "hdr10", "hdr10+":
		transfer = "smpte2084"
	case "hlg":
		transfer = "arib-std-b67"
	default:
		return ""
	}
	return "zscale=tin=" + transfer + ":min=bt2020nc:pin=bt2020:t=linear:npl=100,format=gbrpf32le,zscale=p=bt709," +
		"tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
}

// filters makes each picture Width wide, as the video is shown (a DVD's
// pixels aren't square).
func filters(tonemap string) string {
	f := "scale=" + strconv.Itoa(Width) + ":trunc(ow/dar/2)*2,setsar=1"
	if tonemap != "" {
		f += "," + tonemap
	}
	return f
}

// reading reads the whole file once, decoding only its keyframes, and
// keeps a frame every interval.
func (g *Generator) reading(ctx context.Context, path string, interval time.Duration, count int, tonemap string, add func(*frame) error) error {
	ctx, cancel := context.WithTimeout(ctx, fileTimeout)
	defer cancel()
	fps := "fps=fps=1000/" + strconv.FormatInt(interval.Milliseconds(), 10)
	args := []string{"-nostdin", "-v", "error", "-skip_frame", "nokey", "-i", path, "-map", "0:V:0", "-an", "-sn", "-dn",
		"-vf", fps + "," + filters(tonemap), "-frames:v", strconv.Itoa(count), "-f", "image2pipe", "-c:v", "ppm", "-"}
	n := 0
	err := g.ffmpeg(ctx, args, func(r *bufio.Reader) error {
		for n < count {
			fr, err := readPPM(r)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := add(fr); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	// A file that's damaged near its end still has pictures for most of it.
	if err != nil && (n < count/2 || ctx.Err() != nil) {
		return err
	}
	return nil
}

// jumping jumps to each point in turn and takes the keyframe there, for
// files where that reads much less than reading them through.
func (g *Generator) jumping(ctx context.Context, path string, interval time.Duration, count int, tonemap string, add func(*frame) error) error {
	var last *frame
	failed, lead := 0, 0
	for i := range count {
		at := time.Duration(i) * interval
		fctx, cancel := context.WithTimeout(ctx, frameTimeout)
		args := []string{"-nostdin", "-v", "error", "-noaccurate_seek", "-ss", strconv.FormatFloat(at.Seconds(), 'f', 3, 64),
			"-skip_frame", "nokey", "-i", path, "-map", "0:V:0", "-an", "-sn", "-dn",
			"-vf", filters(tonemap), "-frames:v", "1", "-f", "image2pipe", "-c:v", "ppm", "-"}
		var fr *frame
		err := g.ffmpeg(fctx, args, func(r *bufio.Reader) error {
			var err error
			fr, err = readPPM(r)
			return err
		})
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || fr == nil {
			failed++
			if errors.Is(err, context.DeadlineExceeded) || (last == nil && failed >= 3) {
				if err == nil || err == io.EOF {
					err = errors.New("no pictures came out")
				}
				return err
			}
			if last == nil {
				lead++ // the first one that comes stands in for this one too
				continue
			}
			fr = last // past the last keyframe: the one before will do
		}
		for range lead + 1 {
			if err := add(fr); err != nil {
				return err
			}
		}
		lead = 0
		last = fr
	}
	return nil
}

// ffmpeg runs FFmpeg at low priority and hands its output to read.
func (g *Generator) ffmpeg(ctx context.Context, args []string, read func(*bufio.Reader) error) error {
	cmd := exec.CommandContext(ctx, g.FFmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	if g.LowPriority != nil {
		g.LowPriority(cmd.Process.Pid)
	}
	readErr := read(bufio.NewReaderSize(out, 256<<10))
	io.Copy(io.Discard, out)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			msg, _, _ = strings.Cut(msg, "\n")
			return fmt.Errorf("ffmpeg: %w: %s", waitErr, msg)
		}
		return fmt.Errorf("ffmpeg: %w", waitErr)
	}
	return nil
}

// frame is one picture, as 8-bit RGB.
type frame struct {
	w, h int
	rgb  []byte
}

// readPPM reads one binary PPM image (P6, 8 bits), as FFmpeg's ppm
// encoder writes them. It returns io.EOF when there are no more.
func readPPM(r *bufio.Reader) (*frame, error) {
	var magic string
	var w, h, maxv int
	if _, err := fmt.Fscan(r, &magic, &w, &h, &maxv); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	if magic != "P6" || maxv != 255 || w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, fmt.Errorf("unexpected picture: %s %dx%d %d", magic, w, h, maxv)
	}
	if _, err := r.ReadByte(); err != nil { // the one whitespace after the header
		return nil, err
	}
	fr := &frame{w: w, h: h, rgb: make([]byte, w*h*3)}
	if _, err := io.ReadFull(r, fr.rgb); err != nil {
		return nil, err
	}
	return fr, nil
}
