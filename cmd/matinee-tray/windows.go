//go:build windows

package main

import (
	"errors"
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

var findWindow = windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowW")

// waitForTaskbar waits until the taskbar is there to hold the icon. At
// sign-in Windows can start Matinee before Explorer has made the taskbar,
// and an icon added before then fails.
func waitForTaskbar() {
	class, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	for {
		if h, _, _ := findWindow.Call(uintptr(unsafe.Pointer(class)), 0); h != 0 {
			return
		}
		time.Sleep(2 * time.Second)
	}
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
