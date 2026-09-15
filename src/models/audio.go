package models

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
)

type Audio struct {
	data   []byte
	format Format
	reader *bytes.Reader
}

func NewAudio(data []byte, f Format) *Audio { return newAudio(data, f) }

func newAudio(data []byte, f Format) *Audio {
	if f.Codec == CodecWAV {
		if info, err := wav.Parse(data); err == nil {
			f.SampleRate = info.SampleRate
		}
	}
	return &Audio{data: data, format: f}
}

func (a *Audio) Bytes() []byte          { return a.data }
func (a *Audio) Len() int               { return len(a.data) }
func (a *Audio) Format() Format         { return a.format }
func (a *Audio) SampleRate() int        { return a.format.SampleRate }
func (a *Audio) Save(path string) error { return os.WriteFile(path, a.data, 0o644) }

func (a *Audio) Duration() time.Duration {
	f := a.format
	switch f.Codec {
	case CodecWAV:
		info, err := wav.Parse(a.data)
		if err != nil {
			return 0
		}
		return seconds(info.DataLen, info.SampleRate*info.Channels*info.BitsPerSample/8)
	case CodecPCM16:
		return seconds(len(a.data), f.SampleRate*2)
	case CodecULaw, CodecALaw:
		return seconds(len(a.data), f.SampleRate)
	case CodecMP3, CodecOpus, CodecAAC:
		return seconds(len(a.data), f.Bitrate*1000/8)
	}
	return 0
}

func seconds(n, perSecond int) time.Duration {
	if perSecond <= 0 || n <= 0 {
		return 0
	}
	return time.Duration(float64(n) / float64(perSecond) * float64(time.Second))
}

func (a *Audio) Read(p []byte) (int, error) {
	if a.reader == nil {
		a.reader = bytes.NewReader(a.data)
	}
	return a.reader.Read(p)
}
func (a *Audio) WriteTo(w io.Writer) (int64, error) {
	if a.reader == nil {
		a.reader = bytes.NewReader(a.data)
	}
	return a.reader.WriteTo(w)
}
