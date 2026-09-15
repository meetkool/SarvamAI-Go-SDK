package models

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func pcmOf(samples ...int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

func TestULawRoundTrip(t *testing.T) {
	samples := []int16{0, 100, -100, 1000, -1000, 8000, -8000, 32000, -32000}
	back := ULawToPCM16(PCM16ToULaw(pcmOf(samples...)))

	if len(back) != len(samples)*2 {
		t.Fatalf("got %d bytes, want %d", len(back), len(samples)*2)
	}
	for i, want := range samples {
		got := int16(binary.LittleEndian.Uint16(back[i*2:]))
		diff := int(got) - int(want)
		if diff < 0 {
			diff = -diff
		}
		limit := int(want)/10 + 16
		if limit < 0 {
			limit = -limit
		}
		if diff > limit {
			t.Errorf("sample %d: got %d, want %d (off by %d, mu-law allows %d)", i, got, want, diff, limit)
		}
	}
}

func TestULawSilence(t *testing.T) {
	quiet := ULawToPCM16([]byte{0xFF, 0xFF})
	for i := 0; i+1 < len(quiet); i += 2 {
		if v := int16(binary.LittleEndian.Uint16(quiet[i:])); v < -8 || v > 8 {
			t.Fatalf("0xFF should decode to near silence, got %d", v)
		}
	}
}

func TestResamplePCM16(t *testing.T) {
	ramp := make([]int16, 100)
	for i := range ramp {
		ramp[i] = int16(i * 100)
	}
	pcm := pcmOf(ramp...)

	down := ResamplePCM16(pcm, 16000, 8000)
	if len(down) != 50*2 {
		t.Fatalf("16k to 8k gave %d bytes, want 100", len(down))
	}
	up := ResamplePCM16(pcm, 8000, 16000)
	if len(up) != 200*2 {
		t.Fatalf("8k to 16k gave %d bytes, want 400", len(up))
	}
	if same := ResamplePCM16(pcm, 16000, 16000); len(same) != len(pcm) {
		t.Fatalf("same rate changed the length: %d", len(same))
	}

	previous := int16(-1)
	for i := 0; i+1 < len(down); i += 2 {
		v := int16(binary.LittleEndian.Uint16(down[i:]))
		if v < previous {
			t.Fatalf("a rising ramp should stay rising, %d came after %d", v, previous)
		}
		previous = v
	}
}

func TestWrapAndUnwrapWAV(t *testing.T) {
	pcm := pcmOf(1, 2, 3, 4, 5, -1, -2, -3)

	back, rate, err := UnwrapWAV(WrapWAV(pcm, 8000))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 8000 {
		t.Errorf("rate = %d", rate)
	}
	if !bytes.Equal(back, pcm) {
		t.Errorf("samples changed through the round trip")
	}
}

func TestAudioPCM16(t *testing.T) {
	pcm := pcmOf(1000, -1000, 500, -500)

	data, rate, err := NewAudio(WrapWAV(pcm, 16000), WAV(16000)).PCM16()
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 || !bytes.Equal(data, pcm) {
		t.Errorf("WAV: rate %d, %d bytes", rate, len(data))
	}

	data, rate, err = NewAudio(PCM16ToULaw(pcm), ULaw8000()).PCM16()
	if err != nil {
		t.Fatal(err)
	}
	if rate != 8000 || len(data) != len(pcm) {
		t.Errorf("mu-law: rate %d, %d bytes, want 8000 and %d", rate, len(data), len(pcm))
	}

	if _, _, err := NewAudio([]byte("x"), MP3(24000, 128)).PCM16(); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("MP3 should report ErrUnsupportedFormat, got %v", err)
	}
}
