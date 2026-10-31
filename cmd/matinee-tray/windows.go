//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "Matinee"

	mbInfo  = windows.MB_OK | windows.MB_ICONINFORMATION
	mbError = windows.MB_OK | windows.MB_ICONERROR
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	findWindow       = user32.NewProc("FindWindowW")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	shellNotifyIcon  = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	errTaskbarAbsent = errors.New("no taskbar yet")
)

// waitForTaskbar waits until the notification area takes icons. At sign-in
// Windows can start Matinee before Explorer is ready for them, and an icon
// added then fails for good, so this adds and removes a blank one until it
// works, for up to five minutes.
func waitForTaskbar(log func(error)) {
	var last error
	for range 150 {
		err := tryNotifyIcon()
		if err == nil {
			return
		}
		if last == nil || err.Error() != last.Error() {
			log(err)
		}
		last = err
		time.Sleep(2 * time.Second)
	}
}

func tryNotifyIcon() error {
	tray, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	if h, _, _ := findWindow.Call(uintptr(unsafe.Pointer(tray)), 0); h == 0 {
		return errTaskbarAbsent
	}
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		return err
	}
	defer destroyWindow.Call(hwnd)
	// NOTIFYICONDATAW, as far as the fields an empty icon needs.
	var nid struct {
		Size           uint32
		Wnd            windows.Handle
		ID, Flags, Msg uint32
		Icon           windows.Handle
		Tip            [128]uint16
		State, Mask    uint32
		Info           [256]uint16
		Timeout, Ver   uint32
		InfoTitle      [64]uint16
		InfoFlags      uint32
		GUID           windows.GUID
		BalloonIcon    windows.Handle
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = windows.Handle(hwnd)
	nid.ID = 0x4d54
	if r, _, err := shellNotifyIcon.Call(0 /* NIM_ADD */, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return fmt.Errorf("the notification area doesn't take icons yet: %w", err)
	}
	shellNotifyIcon.Call(2 /* NIM_DELETE */, uintptr(unsafe.Pointer(&nid)))
	return nil
}

// singleInstance is false when Matinee is already running for this user.
func singleInstance() bool {
	name, _ := windows.UTF16PtrFromString(`Local\MatineeTray`)
	_, err := windows.CreateMutex(nil, false, name)
	// The handle stays open until the app exits, which holds the name.
	return !errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}

// hideWindow keeps the server's console window from showing.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func startsAtLogin() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

// setStartAtLogin adds Matinee to, or removes it from, the programs Windows
// starts when this user signs in (Settings → Apps → Startup lists it).
func setStartAtLogin(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, `"`+exe+`" --background`)
}

func openURL(url string) { shellOpen(url, "") }

// openFile opens a file in the app Windows uses for it (Notepad for a .log
// or .env, unless someone picked another).
func openFile(path string) {
	if shellOpen(path, "") != nil {
		shellOpen("notepad.exe", `"`+path+`"`)
	}
}

func shellOpen(file, args string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	f, _ := windows.UTF16PtrFromString(file)
	var a *uint16
	if args != "" {
		a, _ = windows.UTF16PtrFromString(args)
	}
	return windows.ShellExecute(0, verb, f, a, nil, windows.SW_SHOWNORMAL)
}

// runInstaller starts the installer, which asks Windows for permission to
// install (exec.Command can't start a program that needs it).
func runInstaller(path, args string) error { return shellOpen(path, args) }

func messageBox(title, text string, flags uint32) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, flags|windows.MB_SETFOREGROUND)
}

func askYesNo(title, text string) bool {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _ := windows.MessageBox(0, m, t, windows.MB_YESNO|windows.MB_ICONQUESTION|windows.MB_SETFOREGROUND)
	return r == 6 // IDYES
}
