package models

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
)

func testWAV(sampleRate int, d time.Duration) []byte {
	samples := int(float64(sampleRate) * d.Seconds())
	return append(wav.Header(sampleRate, 1, 16, samples*2), make([]byte, samples*2)...)
}

func TestAudioDuration(t *testing.T) {
	tests := []struct {
		name  string
		audio *Audio
		want  time.Duration
	}{
		{"wav reads its header", newAudio(testWAV(24000, time.Second), WAV(24000)), time.Second},
		{"raw pcm16", newAudio(make([]byte, 16000*2), PCM16(16000)), time.Second},
		{"mp3 from the bitrate", newAudio(make([]byte, 128*1000/8), MP3(24000, 128)), time.Second},
		{"flac cannot be worked out", newAudio(make([]byte, 100), FLAC(24000)), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.audio.Duration(); got != tt.want {
				t.Fatalf("Duration = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAudioTrustsTheWAVHeader(t *testing.T) {

	a := newAudio(testWAV(8000, time.Second), WAV(24000))
	if a.SampleRate() != 8000 {
		t.Fatalf("SampleRate = %d, want 8000", a.SampleRate())
	}
}

func TestAudioSaveReadAndWriteTo(t *testing.T) {
	a := newAudio([]byte("audio-bytes"), MP3(24000, 128))

	path := filepath.Join(t.TempDir(), "clip.mp3")
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "audio-bytes" {
		t.Fatalf("file holds %q", onDisk)
	}

	var buf bytes.Buffer
	if _, err := a.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "audio-bytes" {
		t.Fatalf("WriteTo wrote %q", buf.String())
	}
}

func TestWAVParseRejectsRubbish(t *testing.T) {
	if _, err := wav.Parse([]byte("not a wav file at all")); err == nil {
		t.Fatal("want an error for a non-WAV file")
	}
}

func TestBytesInput(t *testing.T) {
	in := BytesInput(testWAV(16000, 2*time.Second), WAV(16000))

	if got := in.Filename(); got != "audio.wav" {
		t.Errorf("filename = %q", got)
	}
	if got := in.ContentType(); got != "audio/wav" {
		t.Errorf("contentType = %q", got)
	}
	if !in.Replayable() {
		t.Error("bytes should be replayable, so the upload can be retried")
	}
	d, ok := in.Duration()
	if !ok || d != 2*time.Second {
		t.Errorf("duration = %v, %v; want 2s, true", d, ok)
	}
}

func TestFileInputReadsDurationFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(path, testWAV(16000, 3*time.Second), 0o644); err != nil {
		t.Fatal(err)
	}

	in := FileInput(path)
	d, ok := in.Duration()
	if !ok || d != 3*time.Second {
		t.Fatalf("duration = %v, %v; want 3s, true", d, ok)
	}
	if got := in.ContentType(); got != "audio/wav" {
		t.Errorf("contentType = %q", got)
	}
	if got := in.Size(); got != int64(len(testWAV(16000, 3*time.Second))) {
		t.Errorf("size = %d", got)
	}
}

func TestReaderInputIsNotReplayable(t *testing.T) {
	in := ReaderInput(bytes.NewReader([]byte("x")), "clip.mp3", "")
	if in.Replayable() {
		t.Error("a plain reader cannot be rewound, so it must not be retried")
	}
	if got := in.ContentType(); got != "audio/mpeg" {
		t.Errorf("contentType = %q, want audio/mpeg from the file name", got)
	}
	if got := in.Size(); got != -1 {
		t.Errorf("size = %d, want -1 for an unknown length", got)
	}
}
