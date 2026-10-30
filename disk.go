package main

import "syscall"

func disk() (free, total int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(Media, &st) != nil {
		return 0, 0
	}
	return int64(st.Bavail) * st.Bsize, int64(st.Blocks) * st.Bsize
}

func freeGB() int {
	f, _ := disk()
	return int(f / 1e9)
}
