package mic

import (
	"errors"
	"sync"
	"time"

	"github.com/crynta/sarvam-go-sdk/src/models"
)

var ErrNoCapture = errors.New("mic: capture needs cgo (install a C compiler and build with CGO_ENABLED=1)")

const DefaultFrame = 100 * time.Millisecond

type Mic struct {
	frames chan []byte
	rate   int
	size   int
	stop   func()
	once   sync.Once

	mu  sync.Mutex
	buf []byte
}

func Open(sampleRate int, frame time.Duration) (*Mic, error) {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if frame <= 0 {
		frame = DefaultFrame
	}
	size := int(float64(sampleRate) * 2 * frame.Seconds())
	size -= size % 2
	if size < 2 {
		size = 2
	}

	m := &Mic{frames: make(chan []byte, 32), rate: sampleRate, size: size}
	stop, err := open(m, sampleRate)
	if err != nil {
		return nil, err
	}
	m.stop = stop
	return m, nil
}

func (m *Mic) Frames() <-chan []byte { return m.frames }

func (m *Mic) Format() models.Format { return models.PCM16(m.rate) }

func (m *Mic) Close() error {
	m.once.Do(func() {
		if m.stop != nil {
			m.stop()
		}
		close(m.frames)
	})
	return nil
}

func (m *Mic) push(samples []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.buf = append(m.buf, samples...)
	for len(m.buf) >= m.size {
		frame := make([]byte, m.size)
		copy(frame, m.buf)
		m.buf = append(m.buf[:0], m.buf[m.size:]...)

		select {
		case m.frames <- frame:
		default:

		}
	}
}
