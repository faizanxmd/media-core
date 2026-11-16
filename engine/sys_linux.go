//go:build linux

package main

import "syscall"

// fileAlloc: "falloc" reserves a download's space up front at no cost on ext4, xfs and btrfs (no fragmentation
// from 16 parallel pieces); elsewhere it can be slow or unsupported, so aria2 just writes.
func fileAlloc(dir string) string {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return "none"
	}
	switch st.Type {
	case 0xEF53, 0x58465342, 0x9123683E: // ext2/3/4, xfs, btrfs
		return "falloc"
	}
	return "none"
}
