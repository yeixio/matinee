package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// PCM is the audio ServePCM sends: 16-bit little-endian stereo at 44.1 kHz,
// the format AirPlay speakers and Air-Mixer use, so they can play it
// without decoding anything.
const (
	PCMRate     = 44100
	PCMChannels = 2
)

// ServePCM decodes an item's first audio stream with FFmpeg and sends it as
// an endless WAV, from start on. It's for speakers that can't decode the
// file themselves (see GET /items/{id}/audio). Files on the server seek
// straight to start; podcast episodes and radio stations are fetched with
// client (Remote, or Home for a station an admin added from the home
// network).
// The response ends when the audio does, or when the listener leaves.
func ServePCM(w http.ResponseWriter, r *http.Request, ffmpeg string, it store.Item, src string, client *http.Client, start time.Duration) {
	args := []string{"-nostdin", "-v", "error"}
	var in io.ReadCloser
	if store.IsRemote(src) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, src, nil)
		if err != nil {
			http.Error(w, "file unavailable", http.StatusNotFound)
			return
		}
		req.Header.Set("User-Agent", remoteUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "the audio couldn't be reached", http.StatusBadGateway)
			return
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			http.Error(w, "the audio couldn't be reached", http.StatusBadGateway)
			return
		}
		in = resp.Body
		defer in.Close()
		args = append(args, "-i", "pipe:0")
		if start > 0 {
			// Piped input can't seek; read up to start and drop it.
			args = append(args, "-ss", seconds(start))
		}
	} else {
		if start > 0 {
			args = append(args, "-ss", seconds(start))
		}
		args = append(args, "-i", src)
	}
	args = append(args, "-map", "0:a:0", "-vn", "-sn", "-dn",
		"-ac", strconv.Itoa(PCMChannels), "-ar", strconv.Itoa(PCMRate),
		"-acodec", "pcm_s16le", "-f", "s16le", "pipe:1")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	if in != nil {
		cmd.Stdin = in
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "the audio couldn't be decoded", http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "the audio couldn't be decoded", http.StatusInternalServerError)
		return
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	h := w.Header()
	h.Set("Content-Type", "audio/wav")
	h.Set("Cache-Control", "no-store")
	if it.DurationMs != nil && *it.DurationMs > 0 {
		h.Set("X-Duration-Ms", strconv.FormatInt(*it.DurationMs, 10))
	}
	h.Set("X-Start-Ms", strconv.FormatInt(start.Milliseconds(), 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(WAVHeader(PCMRate, PCMChannels)); err != nil {
		return
	}
	f, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := out.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if f != nil {
				f.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// WAVHeader is the header of a 16-bit PCM WAV of unknown length: the sizes
// say "as long as it goes", which players of live WAV streams expect.
func WAVHeader(rate, channels int) []byte {
	const unknown = 0xFFFFFFFF
	b := make([]byte, 44)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], unknown)
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], uint16(channels))
	binary.LittleEndian.PutUint32(b[24:], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(b[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], unknown)
	return b
}

func seconds(d time.Duration) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(d.Seconds(), 'f', 3, 64), "0"), ".")
}

// ServeAAC encodes an endless WAV, such as an Air-Mixer output's, to AAC
// (ADTS) as it comes, for players that can't play WAV streams. It's the
// radio.Converter for stations that stream PCM.
func ServeAAC(w http.ResponseWriter, r *http.Request, ffmpeg string, body io.Reader) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error",
		"-f", "wav", "-i", "pipe:0",
		"-vn", "-c:a", "aac", "-b:a", "256k", "-f", "adts", "pipe:1")
	cmd.Stdin = body
	out, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "the station couldn't be converted", http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "the station couldn't be converted", http.StatusInternalServerError)
		return
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()
	h := w.Header()
	h.Set("Content-Type", "audio/aac")
	h.Set("Cache-Control", "no-store")
	h.Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	f, _ := w.(http.Flusher)
	buf := make([]byte, 16<<10)
	for {
		n, err := out.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if f != nil {
				f.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
