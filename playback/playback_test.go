package playback

import (
	"bytes"
	"errors"
	"testing"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func TestMonoToStereo(t *testing.T) {
	got := monoToStereo([]byte{1, 2, 3, 4})
	want := []byte{1, 2, 1, 2, 3, 4, 3, 4}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResample(t *testing.T) {
	pcm := make([]byte, 4*4)

	if got := len(resample(pcm, 48000, 24000)); got != 2*4 {
		t.Errorf("halving the rate gave %d bytes, want 8", got)
	}
	if got := len(resample(pcm, 24000, 48000)); got != 8*4 {
		t.Errorf("doubling the rate gave %d bytes, want 32", got)
	}
	if got := len(resample(pcm, 24000, 24000)); got != len(pcm) {
		t.Errorf("the same rate should pass through untouched, got %d bytes", got)
	}
}

func TestDecodeWAVToStereo(t *testing.T) {
	const samples = 16000
	data := append(wav.Header(16000, 1, 16, samples*2), make([]byte, samples*2)...)

	pcm, rate, err := decode(models.NewAudio(data, models.WAV(16000)))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 {
		t.Errorf("rate = %d, want 16000", rate)
	}
	if len(pcm) != samples*4 {
		t.Errorf("mono should have become stereo: got %d bytes, want %d", len(pcm), samples*4)
	}
}

func TestDecodeRawPCM16(t *testing.T) {
	pcm, rate, err := decode(models.NewAudio([]byte{1, 2, 3, 4}, models.PCM16(24000)))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 24000 {
		t.Errorf("rate = %d", rate)
	}
	if !bytes.Equal(pcm, []byte{1, 2, 1, 2, 3, 4, 3, 4}) {
		t.Errorf("pcm = %v", pcm)
	}
}

func TestDecodeRejectsOtherCodecs(t *testing.T) {
	_, _, err := decode(models.NewAudio([]byte("data"), models.FLAC(24000)))
	if !errors.Is(err, sarvam.ErrUnsupportedFormat) {
		t.Fatalf("got %v, want ErrUnsupportedFormat", err)
	}
}

func TestSpeakerBuffer(t *testing.T) {
	buf := &pcmBuffer{from: 16000, to: 16000}
	p := make([]byte, 64)

	n, err := buf.Read(p)
	if err != nil || n == 0 || n%4 != 0 {
		t.Fatalf("empty read gave n=%d err=%v", n, err)
	}
	if !bytes.Equal(p[:n], make([]byte, n)) {
		t.Error("an empty buffer should read as silence")
	}

	buf.Write([]byte{1, 2})
	n, err = buf.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p[:n], []byte{1, 2, 1, 2}) {
		t.Fatalf("read %v", p[:n])
	}

	buf.Write([]byte{3, 4})
	buf.reset()
	n, _ = buf.Read(p)
	if !bytes.Equal(p[:n], make([]byte, n)) {
		t.Error("reset should have dropped the queued audio")
	}

	buf.close()
	if _, err := buf.Read(p); err == nil {
		t.Error("a closed buffer should report EOF")
	}
}
