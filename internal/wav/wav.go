package wav

import (
	"encoding/binary"
	"errors"
)

type Info struct {
	Channels      int
	SampleRate    int
	BitsPerSample int
	DataOffset    int
	DataLen       int
}

var errNotWAV = errors.New("wav: not a PCM RIFF/WAVE file")

func Parse(data []byte) (Info, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return Info{}, errNotWAV
	}
	var info Info
	for pos := 12; pos+8 <= len(data); {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8

		switch id {
		case "fmt ":
			if body+16 > len(data) {
				return Info{}, errNotWAV
			}
			info.Channels = int(binary.LittleEndian.Uint16(data[body+2:]))
			info.SampleRate = int(binary.LittleEndian.Uint32(data[body+4:]))
			info.BitsPerSample = int(binary.LittleEndian.Uint16(data[body+14:]))
		case "data":
			if info.SampleRate == 0 || info.Channels == 0 || info.BitsPerSample == 0 {
				return Info{}, errNotWAV
			}
			info.DataOffset = body
			info.DataLen = len(data) - body
			if size > 0 && size < info.DataLen {
				info.DataLen = size
			}
			return info, nil
		}
		pos = body + size + size%2
	}
	return Info{}, errNotWAV
}

func Header(sampleRate, channels, bitsPerSample, dataLen int) []byte {
	blockAlign := channels * bitsPerSample / 8
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+dataLen))
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], uint16(channels))
	binary.LittleEndian.PutUint32(h[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(h[28:], uint32(sampleRate*blockAlign))
	binary.LittleEndian.PutUint16(h[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(h[34:], uint16(bitsPerSample))
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(dataLen))
	return h
}

func Join(files [][]byte) ([]byte, error) {
	if len(files) == 0 {
		return nil, errors.New("wav: nothing to join")
	}
	if len(files) == 1 {
		return files[0], nil
	}
	var first Info
	var samples []byte
	for i, f := range files {
		info, err := Parse(f)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			first = info
		} else if info.SampleRate != first.SampleRate || info.Channels != first.Channels || info.BitsPerSample != first.BitsPerSample {
			return nil, errors.New("wav: cannot join files with different formats")
		}
		samples = append(samples, f[info.DataOffset:info.DataOffset+info.DataLen]...)
	}
	return append(Header(first.SampleRate, first.Channels, first.BitsPerSample, len(samples)), samples...), nil
}
