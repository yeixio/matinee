//go:build !linux

package stream

import "syscall"

func procAttr() *syscall.SysProcAttr { return nil }
