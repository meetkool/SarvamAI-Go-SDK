package models

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
)

var ErrUnsupportedFormat = errors.New("sarvam: unsupported audio format")

const (
	ulawBias = 0x84
	ulawClip = 32635
)

func ULawToPCM16(ulaw []byte) []byte {
	pcm := make([]byte, len(ulaw)*2)
	for i, u := range ulaw {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(ulawToLinear(u)))
	}
	return pcm
}

func PCM16ToULaw(pcm []byte) []byte {
	out := make([]byte, len(pcm)/2)
	for i := range out {
		out[i] = linearToULaw(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
	}
	return out
}

func ulawToLinear(u byte) int16 {
	u = ^u
	exponent := int(u>>4) & 0x07
	mantissa := int(u & 0x0f)

	sample := ((mantissa << 3) + ulawBias) << exponent
	sample -= ulawBias
	if u&0x80 != 0 {
		sample = -sample
	}
	return int16(sample)
}

func linearToULaw(sample int16) byte {
	sign := byte(0)
	if sample < 0 {
		if sample == math.MinInt16 {
			sample = -math.MaxInt16
		}
		sample = -sample
		sign = 0x80
	}
	if sample > ulawClip {
		sample = ulawClip
	}
	sample += ulawBias

	exponent := byte(7)
	for mask := int16(0x4000); sample&mask == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}
	mantissa := byte(sample>>(exponent+3)) & 0x0f
	return ^(sign | (exponent << 4) | mantissa)
}

func ResamplePCM16(pcm []byte, from, to int) []byte {
	if from == to || from <= 0 || to <= 0 || len(pcm) < 4 {
		return pcm
	}

	in := len(pcm) / 2
	out := in * to / from
	if out < 1 {
		return nil
	}

	res := make([]byte, out*2)
	for i := range out {
		pos := float64(i) * float64(from) / float64(to)
		j := int(pos)
		frac := pos - float64(j)

		first := int16(binary.LittleEndian.Uint16(pcm[j*2:]))
		second := first
		if j+1 < in {
			second = int16(binary.LittleEndian.Uint16(pcm[(j+1)*2:]))
		}

		v := float64(first) + (float64(second)-float64(first))*frac
		binary.LittleEndian.PutUint16(res[i*2:], uint16(int16(math.Round(v))))
	}
	return res
}

func WrapWAV(pcm []byte, sampleRate int) []byte {
	return append(wav.Header(sampleRate, 1, 16, len(pcm)), pcm...)
}

func UnwrapWAV(data []byte) ([]byte, int, error) {
	info, err := wav.Parse(data)
	if err != nil {
		return nil, 0, err
	}
	if info.BitsPerSample != 16 {
		return nil, 0, fmt.Errorf("%w: WAV is %d-bit, want 16", ErrUnsupportedFormat, info.BitsPerSample)
	}
	return data[info.DataOffset : info.DataOffset+info.DataLen], info.SampleRate, nil
}

func (a *Audio) PCM16() ([]byte, int, error) {
	switch a.format.Codec {
	case CodecPCM16:
		return a.data, a.format.SampleRate, nil
	case CodecWAV:
		return UnwrapWAV(a.data)
	case CodecULaw:
		return ULawToPCM16(a.data), a.format.SampleRate, nil
	}
	return nil, 0, fmt.Errorf("%w: cannot turn %s into PCM16", ErrUnsupportedFormat, a.format)
}
