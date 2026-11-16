package main

import "syscall"

func disk() (free, total int64) { return diskAt(Media) }

// diskAt is the free and total space of the filesystem holding path.
func diskAt(path string) (free, total int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, 0
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize)
}

func freeGB() int {
	f, _ := disk()
	return int(f / 1e9)
}
