//go:build linux

package e2e

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// openPTY returns the master side of a new pseudo-terminal and the path of
// its slave. Linux: unlockpt is TIOCSPTLCK(0), ptsname is TIOCGPTN.
func openPTY() (*os.File, string, error) {
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	var unlock int32
	if err := ioctl(fd, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		syscall.Close(fd)
		return nil, "", fmt.Errorf("unlockpt: %w", err)
	}
	var n uint32
	if err := ioctl(fd, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		syscall.Close(fd)
		return nil, "", fmt.Errorf("ptsname: %w", err)
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), "/dev/pts/" + strconv.FormatUint(uint64(n), 10), nil
}

// ioctl takes the argument as an unsafe.Pointer, converted to uintptr only in
// the Syscall expression itself, so the garbage collector keeps tracking it.
func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}
