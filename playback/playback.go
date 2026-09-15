package playback

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/wav"
	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
	"github.com/ebitengine/oto/v3"
	mp3 "github.com/hajimehoshi/go-mp3"
)

const (
	channels = 2
	bitDepth = 2
)

var (
	deviceMu   sync.Mutex
	device     *oto.Context
	deviceRate int
)

func audioContext(sampleRate int) (*oto.Context, int, error) {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if device != nil {
		return device, deviceRate, nil
	}

	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("playback: opening the audio device: %w", err)
	}
	<-ready
	device, deviceRate = ctx, sampleRate
	return device, deviceRate, nil
}

func Play(ctx context.Context, a *models.Audio) error {
	pcm, rate, err := decode(a)
	if err != nil {
		return err
	}
	out, outRate, err := audioContext(rate)
	if err != nil {
		return err
	}
	if rate != outRate {
		pcm = resample(pcm, rate, outRate)
	}

	player := out.NewPlayer(bytes.NewReader(pcm))
	defer player.Close()

	player.Play()
	for player.IsPlaying() {
		select {
		case <-ctx.Done():
			player.Pause()
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return player.Err()
}

func decode(a *models.Audio) ([]byte, int, error) {
	switch a.Format().Codec {
	case models.CodecWAV:
		info, err := wav.Parse(a.Bytes())
		if err != nil {
			return nil, 0, fmt.Errorf("playback: %w", err)
		}
		if info.BitsPerSample != 16 {
			return nil, 0, fmt.Errorf("%w: playback needs 16-bit WAV, this file is %d-bit",
				sarvam.ErrUnsupportedFormat, info.BitsPerSample)
		}
		samples := a.Bytes()[info.DataOffset : info.DataOffset+info.DataLen]
		if info.Channels == 1 {
			samples = monoToStereo(samples)
		}
		return samples, info.SampleRate, nil

	case models.CodecPCM16:
		return monoToStereo(a.Bytes()), a.SampleRate(), nil

	case models.CodecMP3:
		decoder, err := mp3.NewDecoder(bytes.NewReader(a.Bytes()))
		if err != nil {
			return nil, 0, fmt.Errorf("playback: decoding MP3: %w", err)
		}
		samples, err := io.ReadAll(decoder)
		if err != nil {
			return nil, 0, fmt.Errorf("playback: decoding MP3: %w", err)
		}
		return samples, decoder.SampleRate(), nil
	}

	return nil, 0, fmt.Errorf("%w: playback handles WAV, PCM16 and MP3, not %s",
		sarvam.ErrUnsupportedFormat, a.Format())
}

func monoToStereo(mono []byte) []byte {
	stereo := make([]byte, 0, len(mono)*2)
	for i := 0; i+1 < len(mono); i += 2 {
		stereo = append(stereo, mono[i], mono[i+1], mono[i], mono[i+1])
	}
	return stereo
}

func resample(pcm []byte, from, to int) []byte {
	if from == to || from <= 0 || to <= 0 {
		return pcm
	}
	const frame = channels * bitDepth
	inFrames := len(pcm) / frame
	if inFrames == 0 {
		return pcm
	}
	outFrames := inFrames * to / from
	out := make([]byte, outFrames*frame)
	for i := range outFrames {
		src := min(i*from/to, inFrames-1)
		copy(out[i*frame:(i+1)*frame], pcm[src*frame:(src+1)*frame])
	}
	return out
}

type Speaker struct {
	player *oto.Player
	buf    *pcmBuffer
}

func NewSpeaker(sampleRate int) (*Speaker, error) {
	out, outRate, err := audioContext(sampleRate)
	if err != nil {
		return nil, err
	}

	buf := &pcmBuffer{from: sampleRate, to: outRate}
	player := out.NewPlayer(buf)
	player.SetBufferSize(outRate * channels * bitDepth / 10)
	player.Play()
	return &Speaker{player: player, buf: buf}, nil
}

func (s *Speaker) Write(p []byte) (int, error) { return s.buf.Write(p) }

func (s *Speaker) Clear() {
	s.buf.reset()

	_, _ = s.player.Seek(0, io.SeekStart)
}

func (s *Speaker) Close() error {
	s.buf.close()
	return s.player.Close()
}

type pcmBuffer struct {
	mu       sync.Mutex
	data     []byte
	from, to int
	closed   bool
}

func (b *pcmBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	stereo := monoToStereo(p)
	if b.from != b.to {
		stereo = resample(stereo, b.from, b.to)
	}
	b.data = append(b.data, stereo...)
	return len(p), nil
}

func (b *pcmBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.data) == 0 {
		if b.closed {
			return 0, io.EOF
		}
		n := min(len(p), 1024)
		n -= n % (channels * bitDepth)
		clear(p[:n])
		return n, nil
	}

	n := copy(p, b.data)
	b.data = append(b.data[:0], b.data[n:]...)
	return n, nil
}

func (b *pcmBuffer) Seek(int64, int) (int64, error) { return 0, nil }

func (b *pcmBuffer) reset() {
	b.mu.Lock()
	b.data = b.data[:0]
	b.mu.Unlock()
}

func (b *pcmBuffer) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}
