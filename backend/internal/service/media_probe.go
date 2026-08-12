package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// MediaInfo 视频/图片文件元数据
type MediaInfo struct {
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Duration  float64 `json:"duration"` // 秒
	Size      int64   `json:"size"`
	MimeType  string  `json:"mime_type"`
	IsVideo   bool    `json:"is_video"`
	IsImage   bool    `json:"is_image"`
	Bitrate   int64   `json:"bitrate,omitempty"`
	VideoCodec string `json:"video_codec,omitempty"`
}

var errBoxNotFound = errors.New("box not found")

// ProbeMedia 通过 SFTP 读取文件头部/尾部字节解析 MP4/图片元数据
func (r *RemoteExec) ProbeMedia(p string) (*MediaInfo, error) {
	size, err := r.Size(p)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, errors.New("empty file")
	}
	ext := strings.ToLower(filepath.Ext(p))
	info := &MediaInfo{Size: size, MimeType: mimeTypeOf(p)}

	// 图片: 解析 PNG/JPEG 头
	switch ext {
	case ".png":
		info.IsImage = true
		return r.probePNG(p, info)
	case ".jpg", ".jpeg":
		info.IsImage = true
		return r.probeJPEG(p, info)
	case ".gif":
		info.IsImage = true
		return r.probeGIF(p, info)
	case ".webp":
		info.IsImage = true
		return r.probeWebP(p, info)
	case ".mp4", ".mov", ".m4v", ".webm", ".mkv":
		info.IsVideo = true
		return r.probeMP4(p, info)
	}
	return info, nil
}

// readAt 从远程文件随机读取
func (r *RemoteExec) readAt(p string, offset, n int64) ([]byte, error) {
	f, err := r.OpenSeek(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	got := 0
	for got < int(n) {
		m, err := f.Read(buf[got:])
		got += m
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return buf[:got], nil
}

// ---------- MP4 ----------

func (r *RemoteExec) probeMP4(p string, info *MediaInfo) (*MediaInfo, error) {
	size := info.Size
	// moov box 通常位于文件头部(faststart)或尾部; 各读取 4MB 窗口
	const window = 4 << 20
	var data []byte
	var base int64
	if size <= window {
		data, _ = r.readAt(p, 0, size)
		base = 0
	} else {
		head, _ := r.readAt(p, 0, window)
		// 先尝试头部解析
		if w, h, d, codec, bitrate, ok := parseMP4Boxes(head, 0); ok {
			info.Width, info.Height, info.Duration = w, h, d
			info.VideoCodec, info.Bitrate = codec, bitrate
			return info, nil
		}
		tail, _ := r.readAt(p, size-window, window)
		data = tail
		base = size - window
		if w, h, d, codec, bitrate, ok := parseMP4Boxes(data, base); ok {
			info.Width, info.Height, info.Duration = w, h, d
			info.VideoCodec, info.Bitrate = codec, bitrate
		}
		return info, nil
	}
	if w, h, d, codec, bitrate, ok := parseMP4Boxes(data, base); ok {
		info.Width, info.Height, info.Duration = w, h, d
		info.VideoCodec, info.Bitrate = codec, bitrate
	}
	return info, nil
}

// parseMP4Boxes 从数据中解析 moov 内的 tkhd(分辨率) 与 mvhd(时长)
func parseMP4Boxes(data []byte, base int64) (w, h int, dur float64, codec string, bitrate int64, ok bool) {
	// 在数据中定位 moov box
	moov := findBox(data, "moov")
	if moov == nil {
		return 0, 0, 0, "", 0, false
	}
	// moov 内找 mvhd 与 trak->tkhd / mdia->minf->stbl->stsd
	parseChildren(moov, func(typ string, body []byte) {
		switch typ {
		case "mvhd":
			ts, d := parseMVHD(body)
			if ts > 0 && d > 0 {
				dur = float64(d) / float64(ts)
			}
		case "trak":
			// 仅取第一个有分辨率的轨道（视频轨），避免音频轨覆盖
			if w > 0 && h > 0 {
				return
			}
			tkhd := findBox(body, "tkhd")
			if tkhd != nil {
				tw, th := parseTKHD(tkhd)
				if tw > 0 && th > 0 {
					w, h = tw, th
				}
			}
			if codec == "" {
				codec = findCodec(body)
			}
		}
	})
	_ = base
	return w, h, dur, codec, 0, true
}

// findBox 扫描顶层 box 返回指定类型的第一个
func findBox(data []byte, typ string) []byte {
	offset := 0
	for offset+8 <= len(data) {
		boxSize := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])
		if boxSize < 8 {
			return nil
		}
		bodyStart := offset + 8
		bodyEnd := offset + boxSize
		if bodyEnd > len(data) {
			bodyEnd = len(data)
		}
		if boxType == typ {
			return data[bodyStart:bodyEnd]
		}
		if boxType == "wide" || boxType == "free" {
			offset += boxSize
			continue
		}
		offset += boxSize
		if boxSize == 0 {
			break
		}
	}
	return nil
}

// parseChildren 遍历 box 的子 box
func parseChildren(box []byte, fn func(typ string, body []byte)) {
	offset := 0
	for offset+8 <= len(box) {
		boxSize := int(binary.BigEndian.Uint32(box[offset : offset+4]))
		boxType := string(box[offset+4 : offset+8])
		if boxSize < 8 {
			return
		}
		bodyStart := offset + 8
		bodyEnd := offset + boxSize
		if bodyEnd > len(box) {
			bodyEnd = len(box)
		}
		fn(boxType, box[bodyStart:bodyEnd])
		offset += boxSize
		if boxSize == 0 {
			return
		}
	}
}

// parseMVHD 返回 (timescale, duration)
func parseMVHD(b []byte) (uint32, uint32) {
	if len(b) < 20 {
		return 0, 0
	}
	version := b[0]
	if version == 1 {
		if len(b) < 28 {
			return 0, 0
		}
		ts := binary.BigEndian.Uint32(b[20:24])
		dur64 := binary.BigEndian.Uint64(b[24:32])
		if dur64 > (1 << 40) {
			return ts, uint32(dur64)
		}
		return ts, uint32(dur64)
	}
	// version 0: 4(ctime) 4(mtime) 4(timescale) 4(duration)
	if len(b) < 16 {
		return 0, 0
	}
	ts := binary.BigEndian.Uint32(b[12:16])
	dur := binary.BigEndian.Uint32(b[16:20])
	return ts, dur
}

// parseTKHD 返回 (width, height) - 16.16 定点数
func parseTKHD(b []byte) (int, int) {
	version := b[0]
	// tkhd: version+flags(4) ctime(4/8) mtime(4/8) trackID(4) reserved(4)
	//       duration(4/8) reserved(8) layer(2) alt_group(2) volume(2) reserved(2)
	//       matrix(36) width(4) height(4)
	var wOff int
	if version == 1 {
		wOff = 88
	} else {
		wOff = 76
	}
	if len(b) < wOff+8 {
		return 0, 0
	}
	wFixed := binary.BigEndian.Uint32(b[wOff : wOff+4])
	hFixed := binary.BigEndian.Uint32(b[wOff+4 : wOff+8])
	return int(wFixed >> 16), int(hFixed >> 16)
}

// findCodec 递归在 box 树中找 stsd, 返回视频编码 (avc1/h264/hev1/hvc1/vp09/av01)
func findCodec(data []byte) string {
	var stsd []byte
	var search func(b []byte)
	search = func(b []byte) {
		if stsd != nil {
			return
		}
		offset := 0
		for offset+8 <= len(b) {
			boxSize := int(binary.BigEndian.Uint32(b[offset : offset+4]))
			boxType := string(b[offset+4 : offset+8])
			if boxSize < 8 {
				return
			}
			bodyStart := offset + 8
			bodyEnd := offset + boxSize
			if bodyEnd > len(b) {
				bodyEnd = len(b)
			}
			if boxType == "stsd" {
				stsd = b[bodyStart:bodyEnd]
				return
			}
			search(b[bodyStart:bodyEnd])
			offset += boxSize
			if boxSize == 0 {
				return
			}
		}
	}
	search(data)
	if stsd == nil || len(stsd) < 16 {
		return ""
	}
	// stsd: version+flags(4) entry_count(4) 之后是 sample entries
	entries := stsd[8:]
	offset := 0
	for offset+8 <= len(entries) {
		boxSize := int(binary.BigEndian.Uint32(entries[offset : offset+4]))
		boxType := string(entries[offset+4 : offset+8])
		if boxSize < 8 {
			break
		}
		switch boxType {
		case "avc1", "avc3":
			return "h264"
		case "hev1", "hvc1":
			return "h265"
		case "vp09":
			return "vp9"
		case "av01":
			return "av1"
		case "mp4v":
			return "mpeg4"
		}
		offset += boxSize
	}
	return ""
}

// ---------- 图片 ----------

func (r *RemoteExec) probePNG(p string, info *MediaInfo) (*MediaInfo, error) {
	data, err := r.readAt(p, 0, 33)
	if err != nil || len(data) < 33 {
		return info, nil
	}
	// PNG: 8 字节签名 + IHDR(4 len + 4 type + w/h)
	info.Width = int(binary.BigEndian.Uint32(data[16:20]))
	info.Height = int(binary.BigEndian.Uint32(data[20:24]))
	return info, nil
}

func (r *RemoteExec) probeJPEG(p string, info *MediaInfo) (*MediaInfo, error) {
	data, err := r.readAt(p, 0, 256<<10)
	if err != nil || len(data) < 4 {
		return info, nil
	}
	// 扫描 SOF0/1/2 markers
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			// SOF: len(2) precision(1) height(2) width(2)
			if i+9 <= len(data) {
				info.Height = int(binary.BigEndian.Uint16(data[i+5 : i+7]))
				info.Width = int(binary.BigEndian.Uint16(data[i+7 : i+9]))
				return info, nil
			}
		}
		if marker == 0xD8 || marker == 0xD9 {
			i += 2
			continue
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		// segment: len(2) 包含自身
		if i+4 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		i += 2 + segLen
	}
	return info, nil
}

func (r *RemoteExec) probeGIF(p string, info *MediaInfo) (*MediaInfo, error) {
	data, err := r.readAt(p, 0, 10)
	if err != nil || len(data) < 10 {
		return info, nil
	}
	info.Width = int(binary.LittleEndian.Uint16(data[6:8]))
	info.Height = int(binary.LittleEndian.Uint16(data[8:10]))
	return info, nil
}

func (r *RemoteExec) probeWebP(p string, info *MediaInfo) (*MediaInfo, error) {
	data, err := r.readAt(p, 0, 30)
	if err != nil || len(data) < 30 {
		return info, nil
	}
	// RIFF....WEBPVP8 / VP8L / VP8X
	if string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		format := string(data[12:16])
		switch format {
		case "VP8X":
			info.Width = int(uint32(data[24])|uint32(data[25])<<8|uint32(data[26])<<16) + 1
			info.Height = int(uint32(data[27])|uint32(data[28])<<8|uint32(data[29])<<16) + 1
		case "VP8 ":
			info.Width = int(binary.LittleEndian.Uint16(data[26:28])) & 0x3FFF
			info.Height = int(binary.LittleEndian.Uint16(data[28:30])) & 0x3FFF
		case "VP8L":
			b := binary.LittleEndian.Uint32(data[21:25])
			info.Width = int(b&0x3FFF) + 1
			info.Height = int((b>>14)&0x3FFF) + 1
		}
	}
	return info, nil
}

var _ = fmt.Sprintf
