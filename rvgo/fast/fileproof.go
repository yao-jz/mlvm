package fast

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	FileSystemTreeDepth = 10
	MaxStagedFiles      = 1 << FileSystemTreeDepth
	MaxStagedFD         = (1 << FileSystemTreeDepth) - 1

	// A proof contains five header words, four file words, ten file siblings, two
	// descriptor words and ten descriptor siblings. Word-aligned fields keep the Yul
	// decoder small and make malformed high bits easy to reject.
	FileSystemProofWords = 5 + 4 + FileSystemTreeDepth + 2 + FileSystemTreeDepth
	FileSystemProofSize  = FileSystemProofWords * 32
)

var (
	fileSystemRootDomain = []byte("MLVM-FILESYSTEM-V2\x00")
	fileLeafDomain       = []byte("MLVM-FILE-V2\x00")
	descriptorLeafDomain = []byte("MLVM-FD-V2\x00")
)

// FileSystemProof authenticates the immutable file selected by name and the mutable
// descriptor selected by fd against the 32-byte file-system root in the state witness.
// Empty descriptor fields prove that a slot is unused, which openat needs before inserting
// the next descriptor.
type FileSystemProof struct {
	FilePointer    uint64
	NextFD         uint64
	FileCount      uint64
	FileRoot       common.Hash
	DescriptorRoot common.Hash

	FileIndex uint64
	FileID    common.Hash
	FileSize  uint64
	FileAddr  uint64
	FileProof [FileSystemTreeDepth]common.Hash

	DescriptorFileID common.Hash
	DescriptorOffset uint64
	DescriptorProof  [FileSystemTreeDepth]common.Hash

	descriptorIndex uint64
}

func stagedFileID(name string) common.Hash {
	return crypto.Keccak256Hash([]byte(name))
}

// StagedFileID is the stable identifier committed for a staged path. Verifiers use it
// to bind an openat pathname disclosed through the memory proof to the file-tree leaf.
func StagedFileID(name string) common.Hash { return stagedFileID(name) }

func stagedFileLeaf(id common.Hash, size, addr uint64) common.Hash {
	data := append([]byte{}, fileLeafDomain...)
	data = append(data, id[:]...)
	data = binary.BigEndian.AppendUint64(data, size)
	data = binary.BigEndian.AppendUint64(data, addr)
	return crypto.Keccak256Hash(data)
}

func stagedDescriptorLeaf(id common.Hash, offset uint64) common.Hash {
	data := append([]byte{}, descriptorLeafDomain...)
	data = append(data, id[:]...)
	data = binary.BigEndian.AppendUint64(data, offset)
	return crypto.Keccak256Hash(data)
}

func fileSystemRoot(filePointer, nextFD, fileCount uint64, fileRoot, descriptorRoot common.Hash) common.Hash {
	data := append([]byte{}, fileSystemRootDomain...)
	data = binary.BigEndian.AppendUint64(data, filePointer)
	data = binary.BigEndian.AppendUint64(data, nextFD)
	data = binary.BigEndian.AppendUint64(data, fileCount)
	data = append(data, fileRoot[:]...)
	data = append(data, descriptorRoot[:]...)
	return crypto.Keccak256Hash(data)
}

func fixedMerkleRoot(leaves []common.Hash) common.Hash {
	level := make([]common.Hash, MaxStagedFiles)
	copy(level, leaves)
	for width := MaxStagedFiles; width > 1; width /= 2 {
		for i := 0; i < width; i += 2 {
			level[i/2] = HashPair(level[i], level[i+1])
		}
	}
	return level[0]
}

func fixedMerkleProof(leaves []common.Hash, index uint64) [FileSystemTreeDepth]common.Hash {
	if index >= MaxStagedFiles {
		panic(fmt.Sprintf("file-system proof index %d exceeds %d", index, MaxStagedFiles-1))
	}
	level := make([]common.Hash, MaxStagedFiles)
	copy(level, leaves)
	var proof [FileSystemTreeDepth]common.Hash
	path := index
	for depth, width := 0, MaxStagedFiles; width > 1; depth, width = depth+1, width/2 {
		proof[depth] = level[path^1]
		for i := 0; i < width; i += 2 {
			level[i/2] = HashPair(level[i], level[i+1])
		}
		path >>= 1
	}
	return proof
}

func rootFromFixedProof(leaf common.Hash, index uint64, proof [FileSystemTreeDepth]common.Hash) common.Hash {
	node := leaf
	for depth, sibling := range proof {
		if index&(1<<depth) == 0 {
			node = HashPair(node, sibling)
		} else {
			node = HashPair(sibling, node)
		}
	}
	return node
}

func (fs *FileSystem) fileAuthentication() (files []FileInfo, leaves []common.Hash, root common.Hash) {
	files = make([]FileInfo, 0, len(fs.files))
	for _, file := range fs.files {
		files = append(files, *file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	leaves = make([]common.Hash, MaxStagedFiles)
	for i, file := range files {
		leaves[i] = stagedFileLeaf(stagedFileID(file.Name), file.Size, file.Addr)
	}
	root = fixedMerkleRoot(leaves)
	fs.fileRoot = root
	fs.fileRootValid = true
	return
}

func (fs *FileSystem) descriptorAuthentication() (leaves []common.Hash, root common.Hash) {
	leaves = make([]common.Hash, MaxStagedFiles)
	for fd, name := range fs.descriptor2Name {
		leaves[fd] = stagedDescriptorLeaf(stagedFileID(name), fs.descriptor2Off[fd])
	}
	root = fixedMerkleRoot(leaves)
	fs.descriptorRoot = root
	fs.descriptorValid = true
	return
}

func (fs *FileSystem) authenticationRoots() (fileRoot, descriptorRoot common.Hash) {
	if fs.fileRootValid {
		fileRoot = fs.fileRoot
	} else {
		_, _, fileRoot = fs.fileAuthentication()
	}
	if fs.descriptorValid {
		descriptorRoot = fs.descriptorRoot
	} else {
		_, descriptorRoot = fs.descriptorAuthentication()
	}
	return
}

// Proof returns a fixed-size authentication proof for a file name and descriptor in the
// current pre-state. name may be empty when only descriptor non-membership is relevant.
func (fs *FileSystem) Proof(filePointer uint64, name string, fd uint64) (*FileSystemProof, error) {
	if fs == nil {
		return nil, fmt.Errorf("cannot prove a nil file system")
	}
	if fd > MaxStagedFD {
		return nil, fmt.Errorf("staged-file descriptor %d exceeds %d", fd, MaxStagedFD)
	}
	files, fileLeaves, fileRoot := fs.fileAuthentication()
	descriptorLeaves, descriptorRoot := fs.descriptorAuthentication()
	proof := &FileSystemProof{
		FilePointer:     filePointer,
		NextFD:          fs.fileNum,
		FileCount:       uint64(len(files)),
		FileRoot:        fileRoot,
		DescriptorRoot:  descriptorRoot,
		descriptorIndex: fd,
	}

	if name == "" {
		if descriptorName, ok := fs.descriptor2Name[fd]; ok {
			name = descriptorName
		} else if len(files) != 0 {
			// Descriptor non-membership does not depend on a particular file. Select a
			// real leaf so Root can still authenticate the immutable tree when index 0
			// is occupied.
			name = files[0].Name
		}
	}
	if name != "" {
		index := sort.Search(len(files), func(i int) bool { return files[i].Name >= name })
		if index == len(files) || files[index].Name != name {
			return nil, fmt.Errorf("cannot prove unstaged file %q", name)
		}
		file := files[index]
		proof.FileIndex = uint64(index)
		proof.FileID = stagedFileID(file.Name)
		proof.FileSize = file.Size
		proof.FileAddr = file.Addr
	}
	proof.FileProof = fixedMerkleProof(fileLeaves, proof.FileIndex)

	if descriptorName, ok := fs.descriptor2Name[fd]; ok {
		proof.DescriptorFileID = stagedFileID(descriptorName)
		proof.DescriptorOffset = fs.descriptor2Off[fd]
	}
	proof.DescriptorProof = fixedMerkleProof(descriptorLeaves, fd)
	return proof, nil
}

// Root reconstructs and validates the root committed by this proof.
func (proof *FileSystemProof) Root() (common.Hash, error) {
	if proof.FileCount > MaxStagedFiles {
		return common.Hash{}, fmt.Errorf("file count %d exceeds %d", proof.FileCount, MaxStagedFiles)
	}
	if proof.NextFD < firstStagedFileDescriptor || proof.NextFD > MaxStagedFD+1 {
		return common.Hash{}, fmt.Errorf("next descriptor %d is outside [%d,%d]", proof.NextFD, firstStagedFileDescriptor, MaxStagedFD+1)
	}
	if proof.FileIndex >= MaxStagedFiles {
		return common.Hash{}, fmt.Errorf("file index %d exceeds %d", proof.FileIndex, MaxStagedFiles-1)
	}
	var fileLeaf common.Hash
	if proof.FileID != (common.Hash{}) {
		if proof.FileIndex >= proof.FileCount {
			return common.Hash{}, fmt.Errorf("non-empty file index %d is outside file count %d", proof.FileIndex, proof.FileCount)
		}
		if proof.FileSize > FileRegionSize {
			return common.Hash{}, fmt.Errorf("staged file size %d exceeds %d", proof.FileSize, FileRegionSize)
		}
		if proof.FileAddr > ^uint64(0)-proof.FileSize {
			return common.Hash{}, fmt.Errorf("staged file address range overflows uint64")
		}
		fileLeaf = stagedFileLeaf(proof.FileID, proof.FileSize, proof.FileAddr)
	} else {
		if proof.FileSize != 0 || proof.FileAddr != 0 {
			return common.Hash{}, fmt.Errorf("empty file leaf carries non-zero metadata")
		}
		if proof.FileIndex < proof.FileCount {
			return common.Hash{}, fmt.Errorf("empty file index %d is inside file count %d", proof.FileIndex, proof.FileCount)
		}
	}
	if got := rootFromFixedProof(fileLeaf, proof.FileIndex, proof.FileProof); got != proof.FileRoot {
		return common.Hash{}, fmt.Errorf("invalid staged-file Merkle branch")
	}

	var descriptorLeaf common.Hash
	if proof.DescriptorFileID != (common.Hash{}) {
		descriptorLeaf = stagedDescriptorLeaf(proof.DescriptorFileID, proof.DescriptorOffset)
	} else if proof.DescriptorOffset != 0 {
		return common.Hash{}, fmt.Errorf("empty descriptor leaf carries a non-zero offset")
	}
	if got := rootFromFixedProof(descriptorLeaf, proofDescriptorIndex(proof), proof.DescriptorProof); got != proof.DescriptorRoot {
		return common.Hash{}, fmt.Errorf("invalid staged-file descriptor Merkle branch")
	}
	return fileSystemRoot(proof.FilePointer, proof.NextFD, proof.FileCount, proof.FileRoot, proof.DescriptorRoot), nil
}

// Verify binds the decoded proof to both the syscall-selected descriptor and the root in
// the pre-state witness.
func (proof *FileSystemProof) Verify(expectedRoot common.Hash, fd uint64) error {
	if fd > MaxStagedFD {
		return fmt.Errorf("staged-file descriptor %d exceeds %d", fd, MaxStagedFD)
	}
	proof.descriptorIndex = fd
	root, err := proof.Root()
	if err != nil {
		return err
	}
	if root != expectedRoot {
		return fmt.Errorf("file-system proof root %s does not match state root %s", root, expectedRoot)
	}
	return nil
}

// UpdatedRoot returns the authenticated root after replacing the selected descriptor leaf
// and, for openat, advancing nextFD. The immutable file tree and allocation cursor remain
// unchanged during execution.
func (proof *FileSystemProof) UpdatedRoot(fd, nextFD uint64, fileID common.Hash, offset uint64) (common.Hash, error) {
	if fd > MaxStagedFD {
		return common.Hash{}, fmt.Errorf("staged-file descriptor %d exceeds %d", fd, MaxStagedFD)
	}
	if nextFD < firstStagedFileDescriptor || nextFD > MaxStagedFD+1 {
		return common.Hash{}, fmt.Errorf("next descriptor %d is outside [%d,%d]", nextFD, firstStagedFileDescriptor, MaxStagedFD+1)
	}
	var leaf common.Hash
	if fileID != (common.Hash{}) {
		leaf = stagedDescriptorLeaf(fileID, offset)
	} else if offset != 0 {
		return common.Hash{}, fmt.Errorf("empty descriptor leaf cannot carry offset %d", offset)
	}
	descriptorRoot := rootFromFixedProof(leaf, fd, proof.DescriptorProof)
	return fileSystemRoot(proof.FilePointer, nextFD, proof.FileCount, proof.FileRoot, descriptorRoot), nil
}

// Descriptor index is supplied out of band by the syscall. It is encoded into the proof
// before verification so the same fixed wire format cannot be replayed for another fd.
// This private field is represented by the first eight bytes of descriptor proof word 0
// during DecodeFileSystemProof; Go-created proofs set it in Proof.
func proofDescriptorIndex(proof *FileSystemProof) uint64 { return proof.descriptorIndex }

func appendProofWord(out []byte, value uint64) []byte {
	var word [32]byte
	binary.BigEndian.PutUint64(word[24:], value)
	return append(out, word[:]...)
}

// descriptorIndex is not serialized: the executing syscall supplies it. Keeping it out of
// the wire format saves one word and ensures callers cannot choose a different path index.
func (proof *FileSystemProof) Encode() []byte {
	out := make([]byte, 0, FileSystemProofSize)
	out = appendProofWord(out, proof.FilePointer)
	out = appendProofWord(out, proof.NextFD)
	out = appendProofWord(out, proof.FileCount)
	out = append(out, proof.FileRoot[:]...)
	out = append(out, proof.DescriptorRoot[:]...)
	out = appendProofWord(out, proof.FileIndex)
	out = append(out, proof.FileID[:]...)
	out = appendProofWord(out, proof.FileSize)
	out = appendProofWord(out, proof.FileAddr)
	for _, sibling := range proof.FileProof {
		out = append(out, sibling[:]...)
	}
	out = append(out, proof.DescriptorFileID[:]...)
	out = appendProofWord(out, proof.DescriptorOffset)
	for _, sibling := range proof.DescriptorProof {
		out = append(out, sibling[:]...)
	}
	if len(out) != FileSystemProofSize {
		panic(fmt.Sprintf("file-system proof has %d bytes, expected %d", len(out), FileSystemProofSize))
	}
	return out
}

func decodeProofU64(word []byte) (uint64, error) {
	if len(word) != 32 {
		return 0, fmt.Errorf("invalid proof word length %d", len(word))
	}
	for _, b := range word[:24] {
		if b != 0 {
			return 0, fmt.Errorf("uint64 proof word has non-zero high bits")
		}
	}
	return binary.BigEndian.Uint64(word[24:]), nil
}

func DecodeFileSystemProof(data []byte, fd uint64) (*FileSystemProof, error) {
	if len(data) != FileSystemProofSize {
		return nil, fmt.Errorf("file-system proof has %d bytes, expected %d", len(data), FileSystemProofSize)
	}
	if fd > MaxStagedFD {
		return nil, fmt.Errorf("staged-file descriptor %d exceeds %d", fd, MaxStagedFD)
	}
	word := func(i int) []byte { return data[i*32 : (i+1)*32] }
	u64 := func(i int) (uint64, error) { return decodeProofU64(word(i)) }
	proof := &FileSystemProof{descriptorIndex: fd}
	var err error
	if proof.FilePointer, err = u64(0); err != nil {
		return nil, err
	}
	if proof.NextFD, err = u64(1); err != nil {
		return nil, err
	}
	if proof.FileCount, err = u64(2); err != nil {
		return nil, err
	}
	copy(proof.FileRoot[:], word(3))
	copy(proof.DescriptorRoot[:], word(4))
	if proof.FileIndex, err = u64(5); err != nil {
		return nil, err
	}
	copy(proof.FileID[:], word(6))
	if proof.FileSize, err = u64(7); err != nil {
		return nil, err
	}
	if proof.FileAddr, err = u64(8); err != nil {
		return nil, err
	}
	for i := range proof.FileProof {
		copy(proof.FileProof[i][:], word(9+i))
	}
	copy(proof.DescriptorFileID[:], word(19))
	if proof.DescriptorOffset, err = u64(20); err != nil {
		return nil, err
	}
	for i := range proof.DescriptorProof {
		copy(proof.DescriptorProof[i][:], word(21+i))
	}
	return proof, nil
}
