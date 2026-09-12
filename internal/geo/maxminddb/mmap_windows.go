//go:build windows && !appengine

package maxminddb

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// VirtualQuery isn't wrapped by the syscall package, so call it directly.
var procVirtualQuery = syscall.NewLazyDLL("kernel32.dll").NewProc("VirtualQuery")

// memoryBasicInformation mirrors the MEMORY_BASIC_INFORMATION struct filled in
// by VirtualQuery; only RegionSize is used here.
type memoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
}

func virtualQuery(addr uintptr, info *memoryBasicInformation) error {
	r, _, err := procVirtualQuery.Call(addr, uintptr(unsafe.Pointer(info)), unsafe.Sizeof(*info))
	if r == 0 {
		return err
	}
	return nil
}

// mmap maps a file into memory and returns a byte slice.
func mmap(fd, length int) ([]byte, error) {
	// Create a file mapping
	handle, err := syscall.CreateFileMapping(
		syscall.Handle(fd),
		nil,
		syscall.PAGE_READONLY,
		0,
		0,
		nil,
	)
	if err != nil {
		return nil, os.NewSyscallError("CreateFileMapping", err)
	}
	defer syscall.CloseHandle(handle)

	// Map the file into memory
	addrUintptr, err := syscall.MapViewOfFile(
		handle,
		syscall.FILE_MAP_READ,
		0,
		0,
		0,
	)
	if err != nil {
		return nil, os.NewSyscallError("MapViewOfFile", err)
	}

	// When there's not enough address space for the whole file (e.g. large
	// files on 32-bit systems), MapViewOfFile may return a partial mapping.
	// Query the region size and fail on partial mappings.
	var info memoryBasicInformation
	if err := virtualQuery(addrUintptr, &info); err != nil {
		_ = syscall.UnmapViewOfFile(addrUintptr)
		return nil, os.NewSyscallError("VirtualQuery", err)
	}
	if info.RegionSize < uintptr(length) {
		_ = syscall.UnmapViewOfFile(addrUintptr)
		return nil, errors.New("file too large")
	}

	// Workaround for unsafeptr check in go vet, see
	// https://github.com/golang/go/issues/58625
	addr := *(*unsafe.Pointer)(unsafe.Pointer(&addrUintptr))
	return unsafe.Slice((*byte)(addr), length), nil
}

// munmap unmaps a memory-mapped file and releases associated resources.
func munmap(b []byte) error {
	// Convert slice to base address and length
	data := unsafe.SliceData(b)
	addr := uintptr(unsafe.Pointer(data))

	// Unmap the memory
	if err := syscall.UnmapViewOfFile(addr); err != nil {
		return os.NewSyscallError("UnmapViewOfFile", err)
	}
	return nil
}
