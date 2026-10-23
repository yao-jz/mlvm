package fast

// Clone returns a copy of the machine state: memory (shared copy-on-write, see
// Memory.Clone), staged-file metadata, registers and counters. The copy and the original
// evolve independently, and both encode the same witness until one of them steps.
//
// Clone is what makes checkpointed replay possible: a dispute party keeps a few copies of
// its machine and answers "what is your state hash at step n" by stepping forward from
// the nearest one instead of from the program's first instruction.
func (state *VMState) Clone() *VMState {
	out := *state
	out.Memory = state.Memory.Clone()
	out.FileSystem = state.FileSystem.Clone()
	if state.LastHint != nil {
		out.LastHint = append(state.LastHint[:0:0], state.LastHint...)
	}
	if state.OpCount.Other != nil {
		out.OpCount.Other = make(map[string]uint64, len(state.OpCount.Other))
		for k, v := range state.OpCount.Other {
			out.OpCount.Other[k] = v
		}
	}
	return &out
}

// Clone returns a copy of memory that shares page data with the original until either
// side writes: both sides then copy a shared page, with its node cache, on its first
// write (copy-on-write). A checkpoint of a 300 MB model therefore costs only the map of
// page pointers and the cached upper-tree nodes; weights that are never written stay
// shared by every copy. A Memory and its clones must not be used concurrently, because
// the node caches of shared pages are filled lazily when a root is computed.
func (m *Memory) Clone() *Memory {
	out := &Memory{
		nodes:        make(map[uint64]*[32]byte, len(m.nodes)),
		pages:        make(map[uint64]*CachedPage, len(m.pages)),
		lastPageKeys: [2]uint64{^uint64(0), ^uint64(0)},
		epoch:        nextMemoryEpoch(),
	}
	// Cached nodes are never written through their pointers, only replaced, so the two
	// maps can share them.
	for k, v := range m.nodes {
		out.nodes[k] = v
	}
	for k, p := range m.pages {
		out.pages[k] = p
	}
	// The original no longer owns the shared pages either.
	m.epoch = nextMemoryEpoch()
	return out
}

// Clone deep-copies the staged-file manifest and descriptor table.
func (fs *FileSystem) Clone() *FileSystem {
	if fs == nil {
		return nil
	}
	out := *fs
	out.files = make(map[string]*FileInfo, len(fs.files))
	for k, v := range fs.files {
		file := *v
		out.files[k] = &file
	}
	out.descriptor2Name = make(map[uint64]string, len(fs.descriptor2Name))
	for k, v := range fs.descriptor2Name {
		out.descriptor2Name[k] = v
	}
	out.descriptor2Off = make(map[uint64]uint64, len(fs.descriptor2Off))
	for k, v := range fs.descriptor2Off {
		out.descriptor2Off[k] = v
	}
	return &out
}
