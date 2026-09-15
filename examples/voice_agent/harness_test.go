package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const testVoice = "shubh"

type browserFrame struct {
	binary bool
	audio  []byte
	raw    string
	ev     event
}

type transcript struct {
	frames  []browserFrame
	readErr error
}

func (tr transcript) events(kind string) []event {
	var out []event
	for _, f := range tr.frames {
		if !f.binary && f.ev.Type == kind {
			out = append(out, f.ev)
		}
	}
	return out
}

func (tr transcript) texts(kind string) []string {
	var out []string
	for _, ev := range tr.events(kind) {
		out = append(out, ev.Text)
	}
	return out
}

func (tr transcript) count(kind string) int { return len(tr.events(kind)) }

func (tr transcript) states() []string {
	var out []string
	for _, ev := range tr.events("state") {
		out = append(out, ev.State)
	}
	return out
}

func (tr transcript) countState(state string) int {
	n := 0
	for _, got := range tr.states() {
		if got == state {
			n++
		}
	}
	return n
}

func (tr transcript) audio() []byte {
	var out []byte
	for _, f := range tr.frames {
		if f.binary {
			out = append(out, f.audio...)
		}
	}
	return out
}

func (tr transcript) heard() string { return string(tr.audio()) }

func (tr transcript) dump() string {
	var b strings.Builder
	for i, f := range tr.frames {
		if f.binary {
			fmt.Fprintf(&b, "  %2d  audio %3d bytes %q\n", i, len(f.audio), f.audio)
			continue
		}
		fmt.Fprintf(&b, "  %2d  %s\n", i, f.raw)
	}
	if tr.readErr != nil {
		fmt.Fprintf(&b, "  websocket closed: %v\n", tr.readErr)
	}
	if b.Len() == 0 {
		return "  (the browser received nothing at all)\n"
	}
	return b.String()
}

type harness struct {
	t    *testing.T
	fake *fakeSarvam
	app  *httptest.Server
	conn *websocket.Conn

	sttConn *fakeSTT

	mu      sync.Mutex
	frames  []browserFrame
	readErr error
	runErr  error

	bump chan struct{}
	done chan struct{}
}

type harnessOption func(*fakeSarvam)

func withTranscriptionRefused(status int) harnessOption {
	return func(f *fakeSarvam) { f.refuseTranscription(status) }
}

func withTTSPolicy(policy func(flush int) ttsAction) harnessOption {
	return func(f *fakeSarvam) { f.setTTSPolicy(policy) }
}

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()

	fake := newFakeSarvam(t)
	for _, opt := range opts {
		opt(fake)
	}
	client := fake.client(t)

	h := &harness{
		t:    t,
		fake: fake,
		bump: make(chan struct{}, 1),
		done: make(chan struct{}),
	}

	h.app = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(h.done)

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			h.mu.Lock()
			h.runErr = err
			h.mu.Unlock()
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(4 << 20)

		s := &session{client: client, conn: conn, voice: testVoice}
		err = s.run(r.Context())

		h.mu.Lock()
		h.runErr = err
		h.mu.Unlock()
	}))
	t.Cleanup(h.app.Close)

	dialCtx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, strings.Replace(h.app.URL, "http", "ws", 1)+"/ws", nil)
	if err != nil {
		t.Fatalf("the browser could not open the agent websocket: %v", err)
	}
	conn.SetReadLimit(16 << 20)
	h.conn = conn

	t.Cleanup(func() {
		conn.Close(websocket.StatusNormalClosure, "test finished")
		select {
		case <-h.done:
		case <-time.After(waitTimeout):
			t.Error("the session kept running after the browser hung up, so every closed tab would leak a session")
		}
	})

	go h.read()
	return h
}

func (h *harness) read() {
	ctx := context.Background()
	for {
		kind, data, err := h.conn.Read(ctx)
		if err != nil {
			h.mu.Lock()
			h.readErr = err
			h.mu.Unlock()
			h.signal()
			return
		}

		frame := browserFrame{}
		if kind == websocket.MessageBinary {
			frame.binary = true
			frame.audio = append([]byte(nil), data...)
		} else {
			frame.raw = string(data)
			if err := json.Unmarshal(data, &frame.ev); err != nil {
				frame.ev = event{Type: "unparseable"}
			}
		}

		h.mu.Lock()
		h.frames = append(h.frames, frame)
		h.mu.Unlock()
		h.signal()
	}
}

func (h *harness) signal() {
	select {
	case h.bump <- struct{}{}:
	default:
	}
}

func (h *harness) snapshot() transcript {
	h.mu.Lock()
	defer h.mu.Unlock()
	return transcript{frames: append([]browserFrame(nil), h.frames...), readErr: h.readErr}
}

func (h *harness) sessionError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runErr
}

func (h *harness) wait(why string, cond func(transcript) bool) transcript {
	h.t.Helper()

	deadline := time.After(waitTimeout)
	for {
		tr := h.snapshot()
		if cond(tr) {
			return tr
		}
		select {
		case <-h.bump:
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s\nthe browser received:\n%s", why, tr.dump())
			return tr
		}
	}
}

func (h *harness) waitState(state string, n int) transcript {
	h.t.Helper()
	return h.wait(fmt.Sprintf("the session to report %q %d time(s)", state, n), func(tr transcript) bool {
		return tr.countState(state) >= n
	})
}

func (h *harness) waitHeard(want string) transcript {
	h.t.Helper()
	return h.wait(fmt.Sprintf("the browser to be sent the audio for %q", want), func(tr transcript) bool {
		return strings.Contains(tr.heard(), want)
	})
}

func (h *harness) waitEvent(kind string, n int) transcript {
	h.t.Helper()
	return h.wait(fmt.Sprintf("%d %q event(s)", n, kind), func(tr transcript) bool {
		return tr.count(kind) >= n
	})
}

func (h *harness) stt() *fakeSTT {
	h.t.Helper()
	if h.sttConn == nil {
		h.sttConn = h.fake.waitSTT(h.t)
	}
	return h.sttConn
}

func (h *harness) sendMic(frame []byte) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()

	if err := h.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		h.t.Fatalf("the browser could not send mic audio: %v", err)
	}
}

func micFrame(n int) []byte {
	frame := make([]byte, n)
	for i := range frame {
		frame[i] = byte(i % 251)
	}
	return frame
}
