package fast

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
)

const (
	// FileRegionSize reserves a separate 64 GiB virtual-address region for each staged
	// file. The memory is sparse, so only the actual bytes allocate pages.
	FileRegionSize = 1 << 36
	// DefaultFileBase is the first staged-file region.
	DefaultFileBase = 0x2000_0000_0000_0000
	// MaxStagedFileNameLength bounds openat pathname disclosure. The trailing NUL is
	// separate, so the proof touches at most nine 32-byte memory leaves at any alignment.
	MaxStagedFileNameLength = 255
	// MaxStagedFileReadSize bounds one staged-file read transition. Linux permits short
	// reads, and 32 bytes fit in at most two source and two destination Merkle leaves.
	MaxStagedFileReadSize = 32

	firstStagedFileDescriptor = 100
	fileSystemEncodingVersion = 2
)

// FileInfo represents the metadata for a file in the virtual file system.
type FileInfo struct {
	Name string `json:"name"` // File name, matched verbatim by openat.
	Size uint64 `json:"size"` // Size of the file in bytes.
	Addr uint64 `json:"addr"` // Starting address in committed memory.
}

// OpenFile is the serializable part of an open staged-file descriptor.
type OpenFile struct {
	FD     uint64 `json:"fd"`
	Name   string `json:"name"`
	Offset uint64 `json:"offset"`
}

// FileSystem represents an in-memory file system within the VM.
type FileSystem struct {
	files           map[string]*FileInfo
	descriptor2Name map[uint64]string // staged-file descriptor to file name
	descriptor2Off  map[uint64]uint64 // file descriptor to file offset
	fileNum         uint64            // starts with 100
	fileRoot        common.Hash       // cached immutable-file Merkle root
	descriptorRoot  common.Hash       // cached open-descriptor Merkle root
	fileRootValid   bool
	descriptorValid bool
}

// Has reports whether a file has been staged into emulated memory under this exact name.
// Paths are compared verbatim - there is no path resolution, no cwd and no symlinks -
// so the guest must open a file under the same string it was staged with.
func (fs *FileSystem) Has(name string) bool {
	_, ok := fs.files[name]
	return ok
}

func (fs *FileSystem) File(name string) (*FileInfo, bool) {
	file, ok := fs.files[name]
	if !ok {
		return nil, false
	}
	copy := *file
	return &copy, true
}

func (fs *FileSystem) Descriptor(fd uint64) (name string, offset uint64, ok bool) {
	name, ok = fs.descriptor2Name[fd]
	if ok {
		offset = fs.descriptor2Off[fd]
	}
	return
}

func (fs *FileSystem) NextFD() uint64 { return fs.fileNum }

func (fs *FileSystem) Open(name string) (uint64, error) {
	if _, ok := fs.files[name]; !ok {
		return 0, fmt.Errorf("file %q does not exist", name)
	}
	if fs.fileNum > MaxStagedFD {
		return 0, fmt.Errorf("staged-file descriptor limit %d reached", MaxStagedFD)
	}
	fd := fs.fileNum
	fs.descriptor2Name[fd] = name
	fs.descriptor2Off[fd] = 0
	fs.fileNum++
	fs.descriptorValid = false
	return fd, nil
}

func (fs *FileSystem) SetDescriptorOffset(fd, offset uint64) error {
	name, ok := fs.descriptor2Name[fd]
	if !ok {
		return fmt.Errorf("file descriptor %d does not exist", fd)
	}
	file := fs.files[name]
	if offset > file.Size {
		return fmt.Errorf("file descriptor %d offset %d exceeds file size %d", fd, offset, file.Size)
	}
	fs.descriptor2Off[fd] = offset
	fs.descriptorValid = false
	return nil
}

// NewFileSystem creates a new instance of an in-memory file system.
func NewFileSystem() *FileSystem {
	return &FileSystem{
		files:           make(map[string]*FileInfo),
		descriptor2Name: make(map[uint64]string),
		descriptor2Off:  make(map[uint64]uint64),
		fileNum:         firstStagedFileDescriptor,
	}
}

type fileSystemJSON struct {
	Version     uint64     `json:"version"`
	Files       []FileInfo `json:"files"`
	Descriptors []OpenFile `json:"descriptors"`
	NextFD      uint64     `json:"nextFd"`
}

// MarshalJSON persists the metadata that used to disappear because FileSystem's maps are
// intentionally private. Sorted arrays make snapshots reproducible and reviewable.
func (fs *FileSystem) MarshalJSON() ([]byte, error) {
	wire := fileSystemJSON{
		Version:     fileSystemEncodingVersion,
		Files:       make([]FileInfo, 0),
		Descriptors: make([]OpenFile, 0),
		NextFD:      firstStagedFileDescriptor,
	}
	if fs != nil {
		wire.NextFD = fs.fileNum
		for _, file := range fs.files {
			wire.Files = append(wire.Files, *file)
		}
		for fd, name := range fs.descriptor2Name {
			wire.Descriptors = append(wire.Descriptors, OpenFile{
				FD: fd, Name: name, Offset: fs.descriptor2Off[fd],
			})
		}
	}
	sort.Slice(wire.Files, func(i, j int) bool { return wire.Files[i].Name < wire.Files[j].Name })
	sort.Slice(wire.Descriptors, func(i, j int) bool { return wire.Descriptors[i].FD < wire.Descriptors[j].FD })
	return json.Marshal(wire)
}

// UnmarshalJSON restores and validates a staged-file manifest and its open descriptors.
func (fs *FileSystem) UnmarshalJSON(data []byte) error {
	var wire fileSystemJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Version != fileSystemEncodingVersion {
		return fmt.Errorf("unsupported file-system encoding version %d", wire.Version)
	}
	if wire.NextFD == 0 {
		wire.NextFD = firstStagedFileDescriptor
	}
	if wire.NextFD < firstStagedFileDescriptor {
		return fmt.Errorf("next staged-file descriptor %d is below %d", wire.NextFD, firstStagedFileDescriptor)
	}
	if wire.NextFD > MaxStagedFD+1 {
		return fmt.Errorf("next staged-file descriptor %d exceeds %d", wire.NextFD, MaxStagedFD+1)
	}
	if len(wire.Files) > MaxStagedFiles {
		return fmt.Errorf("snapshot has %d staged files; maximum is %d", len(wire.Files), MaxStagedFiles)
	}

	restored := NewFileSystem()
	restored.fileNum = wire.NextFD
	for i := range wire.Files {
		file := wire.Files[i]
		if file.Name == "" {
			return fmt.Errorf("staged file name must not be empty")
		}
		if len(file.Name) > MaxStagedFileNameLength {
			return fmt.Errorf("staged file name %q is %d bytes; maximum is %d", file.Name, len(file.Name), MaxStagedFileNameLength)
		}
		if isKernelPseudoFile(file.Name) {
			return fmt.Errorf("cannot restore reserved kernel pseudo-file %q", file.Name)
		}
		if file.Size > FileRegionSize {
			return fmt.Errorf("staged file %q is %d bytes; maximum is %d", file.Name, file.Size, FileRegionSize)
		}
		if file.Addr > ^uint64(0)-file.Size {
			return fmt.Errorf("staged file %q address range overflows uint64", file.Name)
		}
		if _, duplicate := restored.files[file.Name]; duplicate {
			return fmt.Errorf("duplicate staged file %q", file.Name)
		}
		copy := file
		restored.files[file.Name] = &copy
	}

	for _, descriptor := range wire.Descriptors {
		if descriptor.FD < firstStagedFileDescriptor {
			return fmt.Errorf("staged-file descriptor %d is below %d", descriptor.FD, firstStagedFileDescriptor)
		}
		if descriptor.FD >= wire.NextFD {
			return fmt.Errorf("staged-file descriptor %d is not below nextFd %d", descriptor.FD, wire.NextFD)
		}
		if descriptor.FD > MaxStagedFD {
			return fmt.Errorf("staged-file descriptor %d exceeds %d", descriptor.FD, MaxStagedFD)
		}
		if _, duplicate := restored.descriptor2Name[descriptor.FD]; duplicate {
			return fmt.Errorf("duplicate staged-file descriptor %d", descriptor.FD)
		}
		file, exists := restored.files[descriptor.Name]
		if !exists {
			return fmt.Errorf("descriptor %d refers to unstaged file %q", descriptor.FD, descriptor.Name)
		}
		if descriptor.Offset > file.Size {
			return fmt.Errorf("descriptor %d offset %d exceeds file %q size %d", descriptor.FD, descriptor.Offset, descriptor.Name, file.Size)
		}
		restored.descriptor2Name[descriptor.FD] = descriptor.Name
		restored.descriptor2Off[descriptor.FD] = descriptor.Offset
	}

	*fs = *restored
	return nil
}

// Commitment returns a canonical digest of all host-side metadata that can affect staged
// file syscalls. File bytes themselves are already covered by the memory root.
func (fs *FileSystem) Commitment(filePointer uint64) common.Hash {
	if fs == nil {
		fs = NewFileSystem()
	}
	fileRoot, descriptorRoot := fs.authenticationRoots()
	return fileSystemRoot(filePointer, fs.fileNum, uint64(len(fs.files)), fileRoot, descriptorRoot)
}

// AddFile adds a new file to the file system with the given name and size, allocating it in memory.
func (fs *FileSystem) AddFile(name string, size uint64, state *VMState) error {
	if name == "" {
		return fmt.Errorf("staged file name must not be empty")
	}
	if len(name) > MaxStagedFileNameLength {
		return fmt.Errorf("staged file name %q is %d bytes; maximum is %d", name, len(name), MaxStagedFileNameLength)
	}
	if isKernelPseudoFile(name) {
		return fmt.Errorf("cannot stage reserved kernel pseudo-file %q", name)
	}
	if _, exists := fs.files[name]; exists {
		return fmt.Errorf("file %q is already staged", name)
	}
	if len(fs.files) >= MaxStagedFiles {
		return fmt.Errorf("cannot stage more than %d files", MaxStagedFiles)
	}
	if size > FileRegionSize {
		return fmt.Errorf("file %q is %d bytes; maximum is %d", name, size, FileRegionSize)
	}
	if state.FilePointer > ^uint64(0)-FileRegionSize {
		return fmt.Errorf("staged-file address space exhausted at %#x", state.FilePointer)
	}
	tracef("adding file %s with size %d at address 0x%x\n", name, size, state.FilePointer)

	startAddr := state.FilePointer
	state.FilePointer += FileRegionSize

	fs.files[name] = &FileInfo{
		Name: name,
		Size: size,
		Addr: startAddr,
	}
	fs.fileRootValid = false
	return nil
}

// CloseFile closes a file descriptor.
func (fs *FileSystem) CloseFile(fd uint64) error {
	_, exists := fs.descriptor2Name[fd]
	if !exists {
		return fmt.Errorf("file descriptor %d does not exist", fd)
	}
	delete(fs.descriptor2Name, fd)
	delete(fs.descriptor2Off, fd)
	fs.descriptorValid = false
	return nil
}

// RemoveFile removes a file from the file system.
func (fs *FileSystem) RemoveFile(name string) error {
	if _, exists := fs.files[name]; !exists {
		return fmt.Errorf("file %q does not exist", name)
	}
	for fd, descriptorName := range fs.descriptor2Name {
		if descriptorName == name {
			return fmt.Errorf("file %q is still open as descriptor %d", name, fd)
		}
	}
	delete(fs.files, name)
	fs.fileRootValid = false
	return nil
}
