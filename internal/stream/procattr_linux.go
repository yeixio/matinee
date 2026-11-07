package stream

import "syscall"

// procAttr has Linux stop FFmpeg if the server dies without stopping it
// (killed, or out of memory), so no transcode outlives it.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
