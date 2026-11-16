package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Where a video can be cut: the times of its keyframes, in seconds, as ffmpeg
// reports them. A stream made while a title plays (stream.go) is cut only
// there, so every piece starts with a full picture and a jump lands exactly.
//
// The times come from the file's own index, which is a couple of small reads:
// an MKV's Cues, an MP4's sample tables. A file with neither is read through
// once with ffprobe, which takes minutes on mint's disk, so that answer is kept
// in DataDir/keys.

const maxKeyCache = 2000

var (
	keyMu    sync.Mutex
	keyCache = map[string]keyEntry{}
	keyBusy  = map[string]bool{} // files being read through by ffprobe
	keySeq   int64

	keyProbeTimeout = 30 * time.Minute
	keyProbe        = probeKeyframes
)

type keyEntry struct {
	Size  int64     `json:"size"`
	Mtime int64     `json:"mtime"`
	Keys  []float64 `json:"keys"`
	used  int64
}

var (
	errKeysPending = errors.New("still reading the file's keyframes")
	ErrNoKeyframes = errors.New("this file has no keyframes to cut at")
)

// keyframes returns the file's keyframe times. errKeysPending means a slow
// read was started (or is running): ask again later.
func keyframes(path string) ([]float64, error) {
	size, mtime, ok := fileKey(path)
	if !ok {
		return nil, os.ErrNotExist
	}
	keyMu.Lock()
	if e, ok := keyCache[path]; ok && e.Size == size && e.Mtime == mtime {
		keySeq++
		e.used = keySeq
		keyCache[path] = e
		keyMu.Unlock()
		if len(e.Keys) < 2 {
			return nil, ErrNoKeyframes
		}
		return e.Keys, nil
	}
	busy := keyBusy[path]
	keyMu.Unlock()
	if busy {
		return nil, errKeysPending
	}
	keep := func(keys []float64) {
		keyMu.Lock()
		keySeq++
		keyCache[path] = keyEntry{Size: size, Mtime: mtime, Keys: keys, used: keySeq}
		for len(keyCache) > maxKeyCache {
			var oldestKey string
			var oldestUsed int64
			first := true
			for k, v := range keyCache {
				if first || v.used < oldestUsed {
					oldestKey, oldestUsed, first = k, v.used, false
				}
			}
			delete(keyCache, oldestKey)
		}
		keyMu.Unlock()
	}
	var keys []float64
	var err error
	switch ext(path) {
	case "mkv", "webm":
		keys, err = mkvKeyframes(path)
	case "mp4", "m4v", "mov":
		keys, err = mp4Keyframes(path)
	default:
		err = errors.New("no index to read")
	}
	if err == nil && len(keys) > 1 {
		keep(keys)
		return keys, nil
	}
	// no usable index: the saved answer of an earlier slow read, or start one
	saved := filepath.Join(DataDir, "keys", pathID(path)+".json")
	if b, err := os.ReadFile(saved); err == nil {
		var e keyEntry
		if json.Unmarshal(b, &e) == nil && e.Size == size && e.Mtime == mtime && len(e.Keys) > 1 {
			keep(e.Keys)
			return e.Keys, nil
		}
	}
	keyMu.Lock()
	keyBusy[path] = true
	keyMu.Unlock()
	go func() {
		defer func() {
			keyMu.Lock()
			delete(keyBusy, path)
			keyMu.Unlock()
		}()
		keys, err := keyProbe(path)
		if errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if err != nil || len(keys) < 2 {
			keep(nil) // can't be cut: don't read it through again until it changes
			return
		}
		keep(keys)
		os.MkdirAll(filepath.Dir(saved), 0o755)
		if b, err := json.Marshal(keyEntry{Size: size, Mtime: mtime, Keys: keys}); err == nil {
			os.WriteFile(saved, b, 0o644)
		}
	}()
	return nil, errKeysPending
}

// probeKeyframes reads the whole file's packets: the slow way, for files without an index.
func probeKeyframes(path string) ([]float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyProbeTimeout)
	defer cancel()
	cmd := lowPrio(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var keys []float64
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		t, flags, _ := strings.Cut(sc.Text(), ",")
		if !strings.HasPrefix(flags, "K") {
			continue
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			keys = append(keys, f)
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Float64s(keys)
	return keys, nil
}

// ---------- MKV: the Cues ----------

const (
	mkvSegment   = 0x18538067
	mkvSeekHead  = 0x114D9B74
	mkvSeek      = 0x4DBB
	mkvSeekID    = 0x53AB
	mkvSeekPos   = 0x53AC
	mkvInfo      = 0x1549A966
	mkvTimeScale = 0x2AD7B1
	mkvTracks    = 0x1654AE6B
	mkvTrack     = 0xAE
	mkvTrackNum  = 0xD7
	mkvTrackType = 0x83
	mkvCues      = 0x1C53BB6B
	mkvCuePoint  = 0xBB
	mkvCueTime   = 0xB3
	mkvCuePos    = 0xB7
	mkvCueTrack  = 0xF7
	mkvCluster   = 0x1F43B675
)

// ebmlHead reads an element's id and size from b. size is -1 when unknown.
func ebmlHead(b []byte) (id uint32, size int64, n int, ok bool) {
	if len(b) < 2 {
		return
	}
	l := 1
	for m := byte(0x80); l <= 4 && b[0]&m == 0; m >>= 1 {
		l++
	}
	if l > 4 || len(b) < l+1 {
		return
	}
	for i := 0; i < l; i++ {
		id = id<<8 | uint32(b[i])
	}
	s := 1
	for m := byte(0x80); s <= 8 && b[l]&m == 0; m >>= 1 {
		s++
	}
	if s > 8 || len(b) < l+s {
		return
	}
	v := uint64(b[l]) & (0xFF >> s)
	unknown := v == uint64(0xFF>>s)
	for i := 1; i < s; i++ {
		v = v<<8 | uint64(b[l+i])
		unknown = unknown && b[l+i] == 0xFF
	}
	size = int64(v)
	if unknown {
		size = -1
	}
	return id, size, l + s, true
}

func ebmlUint(b []byte) (v uint64) {
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return
}

// ebmlEach walks the elements inside b.
func ebmlEach(b []byte, fn func(id uint32, data []byte)) {
	for len(b) > 0 {
		id, size, n, ok := ebmlHead(b)
		if !ok || size < 0 || int64(n)+size > int64(len(b)) {
			return
		}
		fn(id, b[n:int64(n)+size])
		b = b[int64(n)+size:]
	}
}

func mkvKeyframes(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 16)
	readAt := func(off int64) (id uint32, size int64, n int, err error) {
		k, err := f.ReadAt(head, off)
		if k < 2 {
			return 0, 0, 0, err
		}
		id, size, n, ok := ebmlHead(head[:k])
		if !ok {
			return 0, 0, 0, errors.New("not an MKV")
		}
		return id, size, n, nil
	}
	body := func(off int64, size int64) ([]byte, error) {
		if size < 0 || size > 256<<20 {
			return nil, errors.New("MKV element too big")
		}
		b := make([]byte, size)
		_, err := io.ReadFull(io.NewSectionReader(f, off, size), b)
		return b, err
	}
	// the EBML header, then the Segment
	id, size, n, err := readAt(0)
	if err != nil || id != 0x1A45DFA3 || size < 0 {
		return nil, errors.New("not an MKV")
	}
	off := int64(n) + size
	id, _, n, err = readAt(off)
	if err != nil || id != mkvSegment {
		return nil, errors.New("no MKV segment")
	}
	seg := off + int64(n)
	scale, vtrack := uint64(1000000), uint64(0)
	cuesAt := int64(-1)
	var cues []byte
	// the top of the segment: SeekHead, Info and Tracks come before the first Cluster
	for off = seg; ; {
		id, size, n, err = readAt(off)
		if err != nil || size < 0 || id == mkvCluster {
			break
		}
		switch id {
		case mkvSeekHead, mkvInfo, mkvTracks, mkvCues:
			b, err := body(off+int64(n), size)
			if err != nil {
				return nil, err
			}
			switch id {
			case mkvSeekHead:
				ebmlEach(b, func(id uint32, seek []byte) {
					if id != mkvSeek {
						return
					}
					var what uint64
					pos := int64(-1)
					ebmlEach(seek, func(id uint32, d []byte) {
						switch id {
						case mkvSeekID:
							what = ebmlUint(d)
						case mkvSeekPos:
							pos = int64(ebmlUint(d))
						}
					})
					if what == mkvCues && pos >= 0 {
						cuesAt = seg + pos
					}
				})
			case mkvInfo:
				ebmlEach(b, func(id uint32, d []byte) {
					if id == mkvTimeScale {
						scale = ebmlUint(d)
					}
				})
			case mkvTracks:
				ebmlEach(b, func(id uint32, tr []byte) {
					if id != mkvTrack {
						return
					}
					var num, kind uint64
					ebmlEach(tr, func(id uint32, d []byte) {
						switch id {
						case mkvTrackNum:
							num = ebmlUint(d)
						case mkvTrackType:
							kind = ebmlUint(d)
						}
					})
					if kind == 1 && vtrack == 0 {
						vtrack = num
					}
				})
			case mkvCues:
				cues = b
			}
		}
		off += int64(n) + size
	}
	if cues == nil {
		if cuesAt < 0 {
			return nil, errors.New("MKV has no cues")
		}
		id, size, n, err = readAt(cuesAt)
		if err != nil || id != mkvCues {
			return nil, errors.New("MKV cues not where the file says")
		}
		if cues, err = body(cuesAt+int64(n), size); err != nil {
			return nil, err
		}
	}
	var keys []float64
	ebmlEach(cues, func(id uint32, cp []byte) {
		if id != mkvCuePoint {
			return
		}
		var t uint64
		mine := false
		ebmlEach(cp, func(id uint32, d []byte) {
			switch id {
			case mkvCueTime:
				t = ebmlUint(d)
			case mkvCuePos:
				ebmlEach(d, func(id uint32, d []byte) {
					if id == mkvCueTrack && (vtrack == 0 || ebmlUint(d) == vtrack) {
						mine = true
					}
				})
			}
		})
		if mine {
			keys = append(keys, float64(t)*float64(scale)/1e9)
		}
	})
	sort.Float64s(keys)
	return dedupe(keys), nil
}

func dedupe(keys []float64) []float64 {
	out := keys[:0]
	for i, k := range keys {
		if i == 0 || k-out[len(out)-1] > 0.001 {
			out = append(out, k)
		}
	}
	return out
}

// ---------- MP4: the sample tables ----------

// mp4Boxes walks the boxes inside b.
func mp4Boxes(b []byte, fn func(kind string, data []byte)) {
	for len(b) >= 8 {
		size := int64(binary.BigEndian.Uint32(b))
		kind := string(b[4:8])
		head := int64(8)
		if size == 1 && len(b) >= 16 {
			size, head = int64(binary.BigEndian.Uint64(b[8:])), 16
		} else if size == 0 {
			size = int64(len(b))
		}
		if size < head || size > int64(len(b)) {
			return
		}
		fn(kind, b[head:size])
		b = b[size:]
	}
}

// mp4Find returns the first box of that kind inside b, following a path.
func mp4Find(b []byte, path ...string) []byte {
	for _, want := range path {
		var found []byte
		mp4Boxes(b, func(kind string, data []byte) {
			if kind == want && found == nil {
				found = data
			}
		})
		if found == nil {
			return nil
		}
		b = found
	}
	return b
}

func mp4Keyframes(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// find the moov box among the file's top boxes
	var moov []byte
	head := make([]byte, 16)
	for off := int64(0); off < fi.Size(); {
		if _, err := f.ReadAt(head, off); err != nil {
			break
		}
		size, hl := int64(binary.BigEndian.Uint32(head)), int64(8)
		if size == 1 {
			size, hl = int64(binary.BigEndian.Uint64(head[8:])), 16
		} else if size == 0 {
			size = fi.Size() - off
		}
		if size < hl {
			break
		}
		if string(head[4:8]) == "moov" {
			if size > 512<<20 {
				return nil, errors.New("MP4 index too big")
			}
			moov = make([]byte, size-hl)
			if _, err := io.ReadFull(io.NewSectionReader(f, off+hl, size-hl), moov); err != nil {
				return nil, err
			}
			break
		}
		off += size
	}
	if moov == nil {
		return nil, errors.New("MP4 has no index")
	}
	if mp4Find(moov, "mvex") != nil {
		return nil, errors.New("fragmented MP4") // its samples aren't in the moov
	}
	movieScale := uint32(1000)
	if mvhd := mp4Find(moov, "mvhd"); len(mvhd) >= 20 {
		if mvhd[0] == 1 && len(mvhd) >= 32 {
			movieScale = binary.BigEndian.Uint32(mvhd[20:])
		} else {
			movieScale = binary.BigEndian.Uint32(mvhd[12:])
		}
	}
	var keys []float64
	mp4Boxes(moov, func(kind string, trak []byte) {
		if kind != "trak" || keys != nil {
			return
		}
		mdia := mp4Find(trak, "mdia")
		if hdlr := mp4Find(mdia, "hdlr"); len(hdlr) < 12 || string(hdlr[8:12]) != "vide" {
			return
		}
		mdhd := mp4Find(mdia, "mdhd")
		if len(mdhd) < 20 {
			return
		}
		scale := binary.BigEndian.Uint32(mdhd[12:])
		if mdhd[0] == 1 && len(mdhd) >= 32 {
			scale = binary.BigEndian.Uint32(mdhd[20:])
		}
		stbl := mp4Find(mdia, "minf", "stbl")
		stts, ctts, stss := mp4Find(stbl, "stts"), mp4Find(stbl, "ctts"), mp4Find(stbl, "stss")
		if scale == 0 || len(stts) < 8 {
			return
		}
		// the edit list: where the track's own clock starts on the film's
		var mediaStart, delay int64
		if elst := mp4Find(trak, "edts", "elst"); len(elst) >= 8 {
			n, v1 := int(binary.BigEndian.Uint32(elst[4:])), elst[0] == 1
			p := elst[8:]
			for i := 0; i < n; i++ {
				var dur, mt int64
				if v1 {
					if len(p) < 20 {
						break
					}
					dur, mt, p = int64(binary.BigEndian.Uint64(p)), int64(binary.BigEndian.Uint64(p[8:])), p[20:]
				} else {
					if len(p) < 12 {
						break
					}
					dur, mt, p = int64(binary.BigEndian.Uint32(p)), int64(int32(binary.BigEndian.Uint32(p[4:]))), p[12:]
				}
				if mt < 0 { // an empty edit: the track starts late
					delay += dur
					continue
				}
				mediaStart = mt
				break
			}
		}
		// each sample's decode time, from the runs of equal lengths
		var dts []int64
		p := stts[8:]
		var t int64
		for n := int(binary.BigEndian.Uint32(stts[4:])); n > 0 && len(p) >= 8; n, p = n-1, p[8:] {
			count, delta := int(binary.BigEndian.Uint32(p)), int64(binary.BigEndian.Uint32(p[4:]))
			if len(dts)+count > 20_000_000 {
				return
			}
			for i := 0; i < count; i++ {
				dts = append(dts, t)
				t += delta
			}
		}
		if len(dts) < 2 { // a cover picture, not the film
			return
		}
		// how much later than its decode time each sample shows
		offs := make([]int32, 0, len(dts))
		if len(ctts) >= 8 {
			p := ctts[8:]
			for n := int(binary.BigEndian.Uint32(ctts[4:])); n > 0 && len(p) >= 8 && len(offs) < len(dts); n, p = n-1, p[8:] {
				count, off := int(binary.BigEndian.Uint32(p)), int32(binary.BigEndian.Uint32(p[4:]))
				for i := 0; i < count && len(offs) < len(dts); i++ {
					offs = append(offs, off)
				}
			}
		}
		at := func(i int) float64 {
			pts := dts[i]
			if i < len(offs) {
				pts += int64(offs[i])
			}
			return float64(pts-mediaStart)/float64(scale) + float64(delay)/float64(movieScale)
		}
		if len(stss) >= 8 {
			p := stss[8:]
			for n := int(binary.BigEndian.Uint32(stss[4:])); n > 0 && len(p) >= 4; n, p = n-1, p[4:] {
				if i := int(binary.BigEndian.Uint32(p)) - 1; i >= 0 && i < len(dts) {
					keys = append(keys, at(i))
				}
			}
		} else { // no list: every sample is a keyframe
			for i := range dts {
				keys = append(keys, at(i))
			}
		}
	})
	if len(keys) == 0 {
		return nil, errors.New("MP4 has no video samples")
	}
	sort.Float64s(keys)
	return dedupe(keys), nil
}
