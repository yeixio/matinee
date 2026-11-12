package gamestream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pion/rtp"
)

// encoder is one FFmpeg turning raw frames or sound into RTP packets, sent
// to a UDP port on localhost that we read them from. FFmpeg cuts the stream
// into packets itself, so each frame goes out the moment it's encoded.
type encoder struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	conn   *net.UDPConn
	stderr bytes.Buffer
}

// scaleFor is how many times bigger to make an emulator's small picture
// before encoding, pixel for pixel, so its pixels stay sharp when the
// player shows it on a big screen: as many times as fit in 720 lines and
// 1280 columns.
func scaleFor(w, h int) int {
	k := 720 / h
	if 1280/w < k {
		k = 1280 / w
	}
	return max(k, 1)
}

func listenRTP() (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	conn.SetReadBuffer(4 << 20)
	return conn, nil
}

func startEncoder(ctx context.Context, ffmpeg string, args []string) (*encoder, error) {
	conn, err := listenRTP()
	if err != nil {
		return nil, err
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	args = append(args, "-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", port))
	e := &encoder{conn: conn}
	e.cmd = exec.CommandContext(ctx, ffmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, args...)...)
	e.cmd.Stderr = &limitedWriter{w: &e.stderr, n: 16 << 10}
	if e.in, err = e.cmd.StdinPipe(); err != nil {
		conn.Close()
		return nil, err
	}
	// -nostdin only stops FFmpeg reading keys; pipe:0 is still the input.
	if err := e.cmd.Start(); err != nil {
		conn.Close()
		return nil, err
	}
	return e, nil
}

// startVideo encodes info-sized bgr0 frames as H.264 for low delay: no
// B-frames, nothing held back to look ahead, a keyframe every second so a
// lost packet spoils at most a second, and a small buffer so a busy scene
// can't queue frames up.
func startVideo(ctx context.Context, ffmpeg string, info Info) (*encoder, error) {
	k := scaleFor(info.Width, info.Height)
	w, h := info.Width*k, info.Height*k
	fps := strconv.FormatFloat(info.FPS, 'f', 4, 64)
	gop := strconv.Itoa(int(info.FPS + 0.5))
	args := []string{
		"-f", "rawvideo", "-pix_fmt", "bgr0", "-video_size", fmt.Sprintf("%dx%d", info.Width, info.Height),
		"-framerate", fps, "-i", "pipe:0",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=neighbor,format=yuv420p", w&^1, h&^1),
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "baseline",
		"-crf", "18", "-maxrate", "12M", "-bufsize", "500k",
		"-g", gop, "-keyint_min", gop, "-bf", "0",
		"-payload_type", "96", "-ssrc", "1",
	}
	return startEncoder(ctx, ffmpeg, args)
}

// startAudio encodes 16-bit stereo sound as Opus in 10 ms packets.
func startAudio(ctx context.Context, ffmpeg string, info Info) (*encoder, error) {
	rate := int(info.SampleRate + 0.5)
	if rate <= 0 {
		rate = 48000
	}
	args := []string{
		"-f", "s16le", "-ar", strconv.Itoa(rate), "-ac", "2", "-i", "pipe:0",
		"-af", "aresample=48000",
		"-c:a", "libopus", "-application", "lowdelay", "-frame_duration", "10", "-b:a", "96k",
		"-payload_type", "111", "-ssrc", "2",
	}
	return startEncoder(ctx, ffmpeg, args)
}

func (e *encoder) Write(b []byte) error {
	_, err := e.in.Write(b)
	if err != nil {
		return fmt.Errorf("encoder: %w: %s", err, strings.TrimSpace(e.stderr.String()))
	}
	return nil
}

// forward reads FFmpeg's packets and hands each to send until the encoder
// stops.
func (e *encoder) forward(send func(*rtp.Packet) error) {
	buf := make([]byte, 1600)
	for {
		n, _, err := e.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var p rtp.Packet
		if p.Unmarshal(buf[:n]) != nil {
			continue
		}
		if send(&p) != nil {
			return
		}
	}
}

func (e *encoder) Close() {
	e.in.Close()
	done := make(chan struct{})
	go func() { e.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		e.cmd.Process.Kill()
		<-done
	}
	e.conn.Close()
}
