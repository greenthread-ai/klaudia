//go:build darwin

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY returns the master side of a new pseudo-terminal and the path of
// its slave. macOS: grantpt/unlockpt/ptsname are the TIOCPTY* ioctls.
func openPTY() (*os.File, string, error) {
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	if err := ioctl(fd, syscall.TIOCPTYGRANT, nil); err != nil {
		syscall.Close(fd)
		return nil, "", fmt.Errorf("grantpt: %w", err)
	}
	if err := ioctl(fd, syscall.TIOCPTYUNLK, nil); err != nil {
		syscall.Close(fd)
		return nil, "", fmt.Errorf("unlockpt: %w", err)
	}
	var name [128]byte
	if err := ioctl(fd, syscall.TIOCPTYGNAME, unsafe.Pointer(&name[0])); err != nil {
		syscall.Close(fd)
		return nil, "", fmt.Errorf("ptsname: %w", err)
	}
	n := bytes.IndexByte(name[:], 0)
	if n < 0 {
		n = len(name)
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), string(name[:n]), nil
}

// ioctl takes the argument as an unsafe.Pointer, converted to uintptr only in
// the Syscall expression itself, so the garbage collector keeps tracking it.
func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}
