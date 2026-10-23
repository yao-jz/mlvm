// fileio.c - reads a file through the VM's in-memory file system.
//
// MLVM has no host filesystem and no block device. Files are staged into the emulated
// address space at load time (`--load-file`), each at a 64 GiB-aligned slot starting at
// 0x2000_0000_0000_0000, and openat/read/lseek/close are served straight out of emulated
// memory. That is how a multi-hundred-megabyte model checkpoint gets in front of an
// inference program without any I/O syscall ever leaving the VM.
//
// File contents are part of merkleized memory. Reads are legal POSIX short reads capped at
// 32 bytes so every source and destination access has a bounded Merkle proof. The manifest
// and descriptor cursors are committed separately, in a file tree and a descriptor tree;
// fast Go, slow Go and RISCV.sol authenticate openat, read, fcntl, lseek and close against
// them. Files are read-only, at most 1,024 can be staged, descriptors run from 100 to 1023,
// and names are at most 255 bytes. See docs/SYSCALLS.md#in-memory-file-system.
//
// Build:  make examples
// Run:    ./scripts/run-elf.sh examples/build/fileio examples/data/sample.txt

#include <stdio.h>
#include <string.h>

int main(int argc, char **argv) {
    const char *path = (argc > 1) ? argv[1] : "examples/data/sample.txt";

    FILE *f = fopen(path, "r");
    if (!f) {
        printf("could not open %s\n", path);
        return 1;
    }

    char buf[256];
    int lines = 0;
    unsigned long bytes = 0, checksum = 0;
    while (fgets(buf, sizeof buf, f)) {
        lines++;
        size_t n = strlen(buf);
        bytes += n;
        for (size_t i = 0; i < n; i++) {
            checksum = checksum * 131u + (unsigned char)buf[i];
        }
        if (lines <= 3) {
            printf("line %d: %s", lines, buf);
        }
    }
    fclose(f);

    printf("---\n");
    printf("path     = %s\n", path);
    printf("lines    = %d\n", lines);
    printf("bytes    = %lu\n", bytes);
    printf("checksum = %lu\n", checksum);
    return 0;
}
