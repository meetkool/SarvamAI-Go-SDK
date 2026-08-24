package sarvam

import (
	"bytes"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crynta/sarvam-go-sdk/internal/wav"
)

type Input interface {
	filename() string
	contentType() string
	open() (io.ReadCloser, error)
	size() int64
	replayable() bool
	duration() (time.Duration, bool)
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

func (f *fileInput) filename() string             { return filepath.Base(f.path) }
func (f *fileInput) contentType() string          { return mimeType(f.path) }
func (f *fileInput) replayable() bool             { return true }
func (f *fileInput) open() (io.ReadCloser, error) { return os.Open(f.path) }

func (f *fileInput) size() int64 {
	st, err := os.Stat(f.path)
	if err != nil {
		return -1
	}
	return st.Size()
}

func (f *fileInput) duration() (time.Duration, bool) {
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
	d := seconds(int(f.size())-info.DataOffset, info.SampleRate*info.Channels*info.BitsPerSample/8)
	return d, d > 0
}

type bytesInput struct {
	data   []byte
	format Format
}

func (b *bytesInput) filename() string    { return "audio." + b.format.extension() }
func (b *bytesInput) contentType() string { return b.format.MIMEType() }
func (b *bytesInput) size() int64         { return int64(len(b.data)) }
func (b *bytesInput) replayable() bool    { return true }
func (b *bytesInput) open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}
func (b *bytesInput) duration() (time.Duration, bool) {
	d := newAudio(b.data, b.format).Duration()
	return d, d > 0
}

type readerInput struct {
	r    io.Reader
	name string
	mime string
}

func (r *readerInput) filename() string                { return r.name }
func (r *readerInput) contentType() string             { return r.mime }
func (r *readerInput) size() int64                     { return -1 }
func (r *readerInput) replayable() bool                { return false }
func (r *readerInput) duration() (time.Duration, bool) { return 0, false }

func (r *readerInput) open() (io.ReadCloser, error) {
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

func readAll(in Input) ([]byte, error) {
	body, err := in.open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}
