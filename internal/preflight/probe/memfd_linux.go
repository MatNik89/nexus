//go:build linux

package probe

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	mfdCloexec      = 0x1
	mfdAllowSealing = 0x2
	fAddSeals       = 1033
	fGetSeals       = 1034
	sealSeal        = 0x1
	sealShrink      = 0x2
	sealGrow        = 0x4
	sealWrite       = 0x8
	allSeals        = sealSeal | sealShrink | sealGrow | sealWrite
)

// memfdWithContent creates an anonymous in-memory file holding exactly buf,
// SEALED against write/grow/shrink/further-sealing (Phase-0 r2 codex #6):
// after Prepare, NOTHING — not even this process — can alter the pinned
// bytes. Positioned at offset 0, suitable for bwrap --ro-bind-data.
func memfdWithContent(name string, buf []byte) (*os.File, error) {
	nameB, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	fd, _, errno := syscall.Syscall(syscall.SYS_MEMFD_CREATE, uintptr(unsafe.Pointer(nameB)), mfdCloexec|mfdAllowSealing, 0)
	if errno != 0 {
		return nil, fmt.Errorf("memfd_create: %w", errno)
	}
	f := os.NewFile(fd, "memfd:"+name)
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, err
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), fAddSeals, allSeals); errno != 0 {
		f.Close()
		return nil, fmt.Errorf("sealing memfd: %w", errno)
	}
	got, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), fGetSeals, 0)
	if errno != 0 || got&allSeals != allSeals {
		f.Close()
		return nil, fmt.Errorf("memfd seals not verified (got %#x)", got)
	}
	return f, nil
}
