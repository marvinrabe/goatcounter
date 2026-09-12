//go:build !windows && !appengine && !plan9 && !js && !wasip1 && !wasi

package maxminddb

import (
	"errors"
	"os"
	"syscall"
)

type mmapENODEVError struct{}

func (mmapENODEVError) Error() string {
	return "mmap: the underlying filesystem of the specified file does not support memory mapping"
}

func (mmapENODEVError) Is(target error) bool {
	return target == errors.ErrUnsupported
}

func mmap(fd, length int) (data []byte, err error) {
	data, err = syscall.Mmap(fd, 0, length, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		if err == syscall.ENODEV {
			return nil, mmapENODEVError{}
		}
		return nil, os.NewSyscallError("mmap", err)
	}
	return data, nil
}

func munmap(b []byte) (err error) {
	if err = syscall.Munmap(b); err != nil {
		return os.NewSyscallError("munmap", err)
	}
	return nil
}
