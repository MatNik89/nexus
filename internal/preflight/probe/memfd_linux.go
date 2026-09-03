//go:build linux

package probe

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// memfdWithContent creates an anonymous in-memory file holding exactly buf,
// positioned at offset 0, suitable for bwrap --ro-bind-data. The content is
// decoupled from any host path: post-Prepare swaps and truncations are inert.
func memfdWithContent(name string, buf []byte) (*os.File, error) {
	nameB, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	fd, _, errno := syscall.Syscall(syscall.SYS_MEMFD_CREATE, uintptr(unsafe.Pointer(nameB)), 0, 0)
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
	return f, nil
}
