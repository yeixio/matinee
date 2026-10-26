package probe

import "golang.org/x/sys/windows"

// lowerPriority puts a process in Windows' background mode, which lowers its
// CPU, disk and memory priority.
func lowerPriority(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.SetPriorityClass(h, windows.IDLE_PRIORITY_CLASS)
}
