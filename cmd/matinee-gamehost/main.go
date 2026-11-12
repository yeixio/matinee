// Command matinee-gamehost runs beside Steam and lets Matinee play its
// games: see internal/gamehost. Every flag can also be set by an
// environment variable, named in its help.
package main

import (
	"cmp"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/yeixio/matinee/internal/gamehost"
)

func env(name, def string) string { return cmp.Or(os.Getenv(name), def) }

func main() {
	home, _ := os.UserHomeDir()
	listen := flag.String("listen", env("MATINEE_GAMEHOST_LISTEN", ":7174"), "address to serve Matinee on (MATINEE_GAMEHOST_LISTEN)")
	token := flag.String("token", env("MATINEE_GAMEHOST_TOKEN", ""), "token Matinee must send (MATINEE_GAMEHOST_TOKEN)")
	steamHome := flag.String("home", env("MATINEE_GAMEHOST_HOME", home), "the home folder Steam keeps its files in (MATINEE_GAMEHOST_HOME)")
	display := flag.String("display", env("DISPLAY", ":0"), "X display the games show on (DISPLAY)")
	ffmpeg := flag.String("ffmpeg", env("MATINEE_GAMEHOST_FFMPEG", "ffmpeg"), "FFmpeg (MATINEE_GAMEHOST_FFMPEG)")
	vaapi := flag.String("vaapi", env("MATINEE_GAMEHOST_VAAPI", "/dev/dri/renderD128"), "GPU render node to encode on; empty encodes on the CPU (MATINEE_GAMEHOST_VAAPI)")
	audio := flag.String("audio", env("MATINEE_GAMEHOST_AUDIO", ""), "PulseAudio source to capture; empty is the default output's monitor (MATINEE_GAMEHOST_AUDIO)")
	steam := flag.String("steam", env("MATINEE_GAMEHOST_STEAM", "steam"), "Steam's command (MATINEE_GAMEHOST_STEAM)")
	bitrate, _ := strconv.Atoi(env("MATINEE_GAMEHOST_BITRATE", "20"))
	flag.IntVar(&bitrate, "bitrate", bitrate, "picture bitrate in megabits a second (MATINEE_GAMEHOST_BITRATE)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *vaapi != "" {
		if _, err := os.Stat(*vaapi); err != nil {
			log.Warn("no GPU to encode on; encoding on the CPU", "device", *vaapi)
			*vaapi = ""
		}
	}
	// Steam may not have made its folder yet on a first start.
	var root string
	for {
		r, err := gamehost.SteamRoot(*steamHome)
		if err == nil {
			root = r
			break
		}
		log.Info("waiting for Steam", "err", err)
		time.Sleep(10 * time.Second)
	}
	if *token == "" {
		log.Warn("no token set: anyone on the network can start games here")
	}
	h := &gamehost.Host{SteamRoot: root, Token: *token, Display: *display, FFmpeg: *ffmpeg, VAAPI: *vaapi,
		AudioSource: *audio, Bitrate: bitrate, Steam: *steam, Log: log}
	log.Info("game host ready", "listen", *listen, "steam", root, "display", *display, "gpu", *vaapi)
	srv := &http.Server{Addr: *listen, Handler: h.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
