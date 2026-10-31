// Command matinee-tray is the Windows app that runs the Matinee server: an
// icon in the notification area that starts and stops it, opens it in the
// browser, starts it at sign-in, shows its log and installs updates.
// scripts/package-windows.sh builds it into the installer, next to
// matinee.exe and FFmpeg.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/yeixio/matinee/internal/version"
)

var (
	//go:embed icons/on.ico
	iconOn []byte
	//go:embed icons/off.ico
	iconOff []byte
)

const (
	guideURL    = "https://matinee.yeix.io/docs/getting-started/windows/"
	stopTimeout = 15 * time.Second
	maxLogSize  = 10 << 20
)

// app is the tray icon and the server it runs.
type app struct {
	installDir string // where the installer put matinee.exe
	dataDir    string // %LOCALAPPDATA%\Matinee: database, settings, logs
	logPath    string
	envPath    string
	url        string // where this computer opens the server
	server     *Server
	log        io.Writer

	mu     sync.Mutex
	update *Release // a newer release found by the last check
}

func main() {
	background := flag.Bool("background", false, "start without opening Matinee in the browser (how it starts at sign-in)")
	flag.Parse()

	exe, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	local, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		fatal(err)
	}
	a := &app{installDir: filepath.Dir(exe), dataDir: filepath.Join(local, "Matinee")}
	a.logPath = filepath.Join(a.dataDir, "Logs", "matinee.log")
	a.envPath = filepath.Join(a.dataDir, "matinee.env")
	firstRun := !exists(filepath.Join(a.dataDir, "matinee.db"))

	if err := os.MkdirAll(filepath.Dir(a.logPath), 0o755); err != nil {
		fatal(err)
	}
	if !exists(a.envPath) {
		os.WriteFile(a.envPath, []byte(envFileTemplate), 0o644)
	}
	env := readEnvFile(a.envPath)
	a.url = localURL(lookupEnv(env, "MATINEE_ADDR"))

	// A second start (from the Start menu, say) opens the one running.
	if !singleInstance() {
		if !*background {
			openURL(a.url)
		}
		return
	}

	logFile, err := openLog(a.logPath)
	if err != nil {
		fatal(err)
	}
	a.log = logFile
	ffmpeg := filepath.Join(a.installDir, "ffmpeg")
	a.server = &Server{
		Exe: filepath.Join(a.installDir, "matinee.exe"),
		Args: []string{
			"-data", a.dataDir,
			"-ffmpeg", filepath.Join(ffmpeg, "ffmpeg.exe"),
			"-ffprobe", filepath.Join(ffmpeg, "ffprobe.exe"),
		},
		Env:          env,
		Log:          logFile,
		HealthURL:    a.url + "/api/v1/health",
		Prepare:      hideWindow,
		RestartDelay: 3 * time.Second,
	}
	fmt.Fprintf(a.log, "tray: Matinee %s starting, data in %s\n", version.Version, a.dataDir)
	// The tray library reports its own trouble through the log package.
	log.SetOutput(logFile)
	log.SetPrefix("tray: ")

	// The server starts before the icon, so it runs even if the
	// notification area isn't there (no Explorer, or a broken shell).
	if err := a.server.Start(); err != nil {
		fmt.Fprintln(a.log, "tray:", err)
	}
	// Matinee starts at sign-in unless someone turns that off.
	if firstRun {
		if err := setStartAtLogin(true); err != nil {
			fmt.Fprintln(a.log, "tray: start at sign-in:", err)
		}
	}
	waitForTaskbar()

	systray.Run(func() { a.ready(firstRun, *background) }, func() {
		a.server.Stop(stopTimeout)
		fmt.Fprintln(a.log, "tray: quit")
		logFile.Close()
	})
}

func (a *app) ready(firstRun, background bool) {
	fmt.Fprintln(a.log, "tray: icon ready")
	systray.SetIcon(iconOff)
	systray.SetTooltip("Matinee")

	status := systray.AddMenuItem("Matinee is starting…", "")
	status.Disable()
	open := systray.AddMenuItem("Open Matinee", "Open Matinee in your browser")
	systray.AddSeparator()
	startStop := systray.AddMenuItem("Stop server", "")
	restart := systray.AddMenuItem("Restart server", "")
	systray.AddSeparator()
	logs := systray.AddMenuItem("Show logs", "Open the server's log")
	settings := systray.AddMenuItem("Server settings", "Open matinee.env, the server's settings")
	help := systray.AddMenuItem("Help adding folders and network shares", "")
	systray.AddSeparator()
	login := systray.AddMenuItemCheckbox("Start at sign-in", "Start Matinee when you sign in to Windows", false)
	update := systray.AddMenuItem("Check for updates", "")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Matinee", "Stop the server and close Matinee")

	if startsAtLogin() {
		login.Check()
	}

	if firstRun {
		go messageBox("Welcome to Matinee",
			"Matinee is running. Its ticket icon sits in the notification area at the bottom right of the screen, next to the clock.\n\n"+
				"Your browser opens Matinee next, to create your account and add your movies, shows and music. "+
				"Pick folders on this PC, or type a network share such as \\\\nas\\media.\n\n"+
				"Matinee keeps running in the background and starts when you sign in. Right-click the icon to stop it or quit.",
			mbInfo)
	}
	if firstRun || !background {
		go a.openWhenUp()
	}

	go a.watch(status, startStop, restart)
	go a.checkForUpdates(update, false)
	go func() {
		for range time.Tick(24 * time.Hour) {
			a.checkForUpdates(update, false)
		}
	}()

	go func() {
		for {
			select {
			case <-open.ClickedCh:
				openURL(a.url)
			case <-startStop.ClickedCh:
				if a.server.Wanted() {
					go a.server.Stop(stopTimeout)
				} else if err := a.server.Start(); err != nil {
					messageBox("Matinee", "The server didn't start: "+err.Error(), mbError)
				}
			case <-restart.ClickedCh:
				go a.server.Restart(stopTimeout)
			case <-logs.ClickedCh:
				openFile(a.logPath)
			case <-settings.ClickedCh:
				openFile(a.envPath)
			case <-help.ClickedCh:
				openURL(guideURL)
			case <-login.ClickedCh:
				on := !login.Checked()
				if err := setStartAtLogin(on); err != nil {
					messageBox("Matinee", "Couldn't change starting at sign-in: "+err.Error(), mbError)
					continue
				}
				if on {
					login.Check()
				} else {
					login.Uncheck()
				}
			case <-update.ClickedCh:
				a.mu.Lock()
				rel := a.update
				a.mu.Unlock()
				if rel != nil {
					go a.installUpdate(*rel)
				} else {
					go a.checkForUpdates(update, true)
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// watch keeps the icon and the status line up to date.
func (a *app) watch(status, startStop, restart *systray.MenuItem) {
	last := State(-1)
	for {
		st, ver := a.server.State(context.Background())
		if st != last {
			last = st
			switch st {
			case Running:
				systray.SetIcon(iconOn)
				systray.SetTooltip("Matinee is running")
				status.SetTitle("Matinee " + ver + " is running")
			case Starting:
				systray.SetIcon(iconOff)
				systray.SetTooltip("Matinee is starting")
				status.SetTitle("Matinee is starting…")
			case Stopped:
				systray.SetIcon(iconOff)
				systray.SetTooltip("Matinee is stopped")
				status.SetTitle("Matinee is stopped")
			case Failed:
				systray.SetIcon(iconOff)
				systray.SetTooltip("Matinee stopped")
				status.SetTitle("Matinee stopped: see the logs")
			}
			if st == Running || st == Starting {
				startStop.SetTitle("Stop server")
				restart.Enable()
			} else {
				startStop.SetTitle("Start server")
				restart.Disable()
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// openWhenUp opens Matinee in the browser once the server answers.
func (a *app) openWhenUp() {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if st, _ := a.server.State(context.Background()); st == Running {
			openURL(a.url)
			return
		}
		time.Sleep(time.Second)
	}
}

func (a *app) checkForUpdates(item *systray.MenuItem, asked bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, ok, err := LatestRelease(ctx, http.DefaultClient, releasesURL, version.Version, arch())
	switch {
	case err != nil:
		fmt.Fprintln(a.log, "tray: checking for updates:", err)
		if asked {
			messageBox("Matinee", "Couldn't check for updates: "+err.Error(), mbError)
		}
	case ok:
		a.mu.Lock()
		a.update = &rel
		a.mu.Unlock()
		item.SetTitle("Install Matinee " + rel.Version)
		systray.SetTooltip("Matinee " + rel.Version + " is available")
		if asked {
			go a.installUpdate(rel)
		}
	case asked:
		messageBox("Matinee", "Matinee "+version.Version+" is the newest version.", mbInfo)
	}
}

func (a *app) installUpdate(rel Release) {
	if !askYesNo("Update Matinee",
		"Matinee "+rel.Version+" is available (this is "+version.Version+").\n\n"+
			"Install it now? The server stops for a minute while it updates, then starts again.") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "matinee-update")
	if err != nil {
		messageBox("Matinee", "Couldn't download the update: "+err.Error(), mbError)
		return
	}
	path, err := Download(ctx, http.DefaultClient, rel, dir)
	if err != nil {
		fmt.Fprintln(a.log, "tray: update:", err)
		messageBox("Matinee", "Couldn't download the update: "+err.Error(), mbError)
		return
	}
	fmt.Fprintln(a.log, "tray: installing", rel.Installer)
	a.server.Stop(stopTimeout)
	// /S installs without questions; /update starts Matinee again after.
	// Windows asks for permission to install, as it did the first time.
	if err := runInstaller(path, "/S /update"); err != nil {
		fmt.Fprintln(a.log, "tray: update:", err)
		a.server.Start()
		return
	}
	systray.Quit()
}

// openLog opens the log for adding to, starting a new one when it's big.
func openLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func arch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}

func fatal(err error) {
	messageBox("Matinee", err.Error(), mbError)
	os.Exit(1)
}
