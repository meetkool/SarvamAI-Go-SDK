package models

import (
	"bytes"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
)

type Input interface {
	Filename() string
	ContentType() string
	Open() (io.ReadCloser, error)
	Size() int64
	Replayable() bool
	Duration() (time.Duration, bool)
}

func FileInput(path string) Input            { return &fileInput{path} }
func BytesInput(data []byte, f Format) Input { return &bytesInput{data, f} }

func ReaderInput(r io.Reader, name, ctype string) Input {
	if ctype == "" {
		ctype = mimeType(name)
	}
	return &readerInput{r, name, ctype}
}

type fileInput struct{ path string }

func (f *fileInput) Filename() string             { return filepath.Base(f.path) }
func (f *fileInput) ContentType() string          { return mimeType(f.path) }
func (f *fileInput) Replayable() bool             { return true }
func (f *fileInput) Open() (io.ReadCloser, error) { return os.Open(f.path) }

func (f *fileInput) Size() int64 {
	st, err := os.Stat(f.path)
	if err != nil {
		return -1
	}
	return st.Size()
}

func (f *fileInput) Duration() (time.Duration, bool) {
	if !strings.EqualFold(filepath.Ext(f.path), ".wav") {
		return 0, false
	}
	fh, err := os.Open(f.path)
	if err != nil {
		return 0, false
	}
	defer fh.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(fh, head)
	if n < 44 {
		return 0, false
	}
	info, err := wav.Parse(head[:n])
	if err != nil {
		return 0, false
	}
	d := seconds(int(f.Size())-info.DataOffset, info.SampleRate*info.Channels*info.BitsPerSample/8)
	return d, d > 0
}

type bytesInput struct {
	data   []byte
	format Format
}

func (b *bytesInput) Filename() string    { return "audio." + b.format.extension() }
func (b *bytesInput) ContentType() string { return b.format.MIMEType() }
func (b *bytesInput) Size() int64         { return int64(len(b.data)) }
func (b *bytesInput) Replayable() bool    { return true }
func (b *bytesInput) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}
func (b *bytesInput) Duration() (time.Duration, bool) {
	d := newAudio(b.data, b.format).Duration()
	return d, d > 0
}

type readerInput struct {
	r    io.Reader
	name string
	mime string
}

func (r *readerInput) Filename() string                { return r.name }
func (r *readerInput) ContentType() string             { return r.mime }
func (r *readerInput) Size() int64                     { return -1 }
func (r *readerInput) Replayable() bool                { return false }
func (r *readerInput) Duration() (time.Duration, bool) { return 0, false }

func (r *readerInput) Open() (io.ReadCloser, error) {
	if rc, ok := r.r.(io.ReadCloser); ok {
		return rc, nil
	}
	return io.NopCloser(r.r), nil
}

func mimeType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func ReadAll(in Input) ([]byte, error) {
	body, err := in.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}
