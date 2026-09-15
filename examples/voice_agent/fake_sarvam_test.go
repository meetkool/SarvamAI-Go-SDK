package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

const (
	fakeAPIKey  = "test-key-never-a-real-one"
	waitTimeout = 10 * time.Second
	ttsFinal    = `{"type":"event","data":{"event_type":"final"}}`
)

type chatCall struct {
	Model     string
	MaxTokens int
	Messages  []models.Message
}

func (c chatCall) lastUser() string {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == models.RoleUser {
			return c.Messages[i].Content
		}
	}
	return ""
}

func (c chatCall) roles() []string {
	roles := make([]string, 0, len(c.Messages))
	for _, m := range c.Messages {
		roles = append(roles, string(m.Role))
	}
	return roles
}

type chatScript func(emit func(string), cancelled <-chan struct{})

func speak(tokens ...string) chatScript {
	return func(emit func(string), cancelled <-chan struct{}) {
		for _, token := range tokens {
			select {
			case <-cancelled:
				return
			default:
			}
			emit(token)
		}
	}
}

type ttsAction int

const (
	ttsSpeak ttsAction = iota
	ttsSilent
	ttsHangUp
	ttsStall
)

type fakeSarvam struct {
	t   *testing.T
	srv *httptest.Server
	URL string

	stt   chan *fakeSTT
	tts   chan *fakeTTS
	chats chan chatCall

	mu         sync.Mutex
	scripts    map[string]chatScript
	ttsPolicy  func(flush int) ttsAction
	sttRefusal int
	errs       []string
	ttsSeen    []*fakeTTS
}

func (f *fakeSarvam) allTTS() []*fakeTTS {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeTTS(nil), f.ttsSeen...)
}

func newFakeSarvam(t *testing.T) *fakeSarvam {
	t.Helper()

	f := &fakeSarvam{
		t:       t,
		stt:     make(chan *fakeSTT, 4),
		tts:     make(chan *fakeTTS, 8),
		chats:   make(chan chatCall, 32),
		scripts: map[string]chatScript{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/speech-to-text-realtime/ws", f.handleSTT)
	mux.HandleFunc("/text-to-speech/ws", f.handleTTS)
	mux.HandleFunc("/v1/chat/completions", f.handleChat)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.errorf("the agent called %s %s, which is not one of the three Sarvam endpoints it is supposed to use",
			r.Method, r.URL.Path)
		http.NotFound(w, r)
	})

	f.srv = httptest.NewServer(mux)
	f.URL = f.srv.URL

	t.Cleanup(func() {
		f.srv.Close()
		f.flushErrors()
	})
	return f
}

func (f *fakeSarvam) client(t *testing.T) *sarvam.Client {
	t.Helper()
	client, err := sarvam.New(sarvam.WithAPIKey(fakeAPIKey), sarvam.WithBaseURL(f.URL))
	if err != nil {
		t.Fatalf("building a Sarvam client aimed at the fake server: %v", err)
	}
	return client
}

func (f *fakeSarvam) errorf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

func (f *fakeSarvam) flushErrors() {
	f.mu.Lock()
	errs := f.errs
	f.errs = nil
	f.mu.Unlock()

	for _, e := range errs {
		f.t.Errorf("fake sarvam: %s", e)
	}
}

func (f *fakeSarvam) reply(userText string, tokens ...string) {
	f.replyFunc(userText, speak(tokens...))
}

func (f *fakeSarvam) replyFunc(userText string, script chatScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[userText] = script
}

func (f *fakeSarvam) scriptFor(userText string) chatScript {
	f.mu.Lock()
	defer f.mu.Unlock()
	if script, ok := f.scripts[userText]; ok {
		return script
	}
	return speak("Right.")
}

func (f *fakeSarvam) setTTSPolicy(policy func(flush int) ttsAction) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ttsPolicy = policy
}

func (f *fakeSarvam) actionFor(flush int) ttsAction {
	f.mu.Lock()
	policy := f.ttsPolicy
	f.mu.Unlock()

	if policy == nil {
		return ttsSpeak
	}
	return policy(flush)
}

func (f *fakeSarvam) refuseTranscription(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sttRefusal = status
}

func (f *fakeSarvam) transcriptionRefusal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sttRefusal
}

func (f *fakeSarvam) checkKey(r *http.Request) {
	if got := r.Header.Get("api-subscription-key"); got != fakeAPIKey {
		f.errorf("a request to %s carried the api key %q: the suite must only ever talk to this fake, never to the live API",
			r.URL.Path, got)
	}
}

func (f *fakeSarvam) waitSTT(t *testing.T) *fakeSTT {
	t.Helper()
	select {
	case stt := <-f.stt:
		return stt
	case <-time.After(waitTimeout):
		t.Fatal("the session never opened a realtime transcription socket, so no mic audio can ever be understood")
		return nil
	}
}

func (f *fakeSarvam) waitTTS(t *testing.T) *fakeTTS {
	t.Helper()
	select {
	case tts := <-f.tts:
		return tts
	case <-time.After(waitTimeout):
		t.Fatal("the session never opened a text to speech socket, so the agent can never say anything")
		return nil
	}
}

func (f *fakeSarvam) waitChat(t *testing.T) chatCall {
	t.Helper()
	select {
	case call := <-f.chats:
		return call
	case <-time.After(waitTimeout):
		t.Fatal("the session never asked the model for a reply")
		return chatCall{}
	}
}

func (f *fakeSarvam) chatCount() int { return len(f.chats) }

func (f *fakeSarvam) handleSTT(w http.ResponseWriter, r *http.Request) {
	f.checkKey(r)

	if status := f.transcriptionRefusal(); status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, `{"error":{"message":"invalid api key","code":"unauthorized"}}`)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		f.errorf("accepting the transcription socket: %v", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)

	stt := &fakeSTT{conn: conn, query: r.URL.Query(), audio: make(chan []byte, 256)}
	select {
	case f.stt <- stt:
	default:
		f.errorf("the session opened more transcription sockets than the test expected")
	}

	if err := stt.write(`{"event":"session.begin","request_id":"fake-request"}`); err != nil {
		return
	}

	for {
		_, payload, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var msg struct {
			Event string `json:"event"`
			Audio string `json:"audio"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			f.errorf("the session sent transcription json the API would reject: %s", payload)
			return
		}
		if msg.Event != "audio_input" {
			continue
		}

		frame, err := base64.StdEncoding.DecodeString(msg.Audio)
		if err != nil {
			f.errorf("the session sent mic audio that is not base64: %v", err)
			continue
		}
		select {
		case stt.audio <- frame:
		default:
		}
	}
}

type fakeSTT struct {
	conn  *websocket.Conn
	query url.Values
	audio chan []byte

	mu sync.Mutex
}

func (s *fakeSTT) write(raw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Write(context.Background(), websocket.MessageText, []byte(raw))
}

func (s *fakeSTT) emit(t *testing.T, ev map[string]any) {
	t.Helper()
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("encoding the fake transcription event %v: %v", ev, err)
	}
	if err := s.write(string(raw)); err != nil {
		t.Fatalf("the session stopped reading transcription events before %s arrived: %v", raw, err)
	}
}

func (s *fakeSTT) final(t *testing.T, text string, lang models.Language) {
	t.Helper()
	s.emit(t, map[string]any{"event": "transcript.final", "text": text, "language": string(lang)})
}

func (s *fakeSTT) partial(t *testing.T, text string) {
	t.Helper()
	s.emit(t, map[string]any{"event": "transcript.partial", "text": text})
}

func (s *fakeSTT) speechStart(t *testing.T) {
	t.Helper()
	s.emit(t, map[string]any{"event": "vad.speech_start", "utterance_idx": 1, "confidence": 0.9})
}

func (s *fakeSTT) waitAudio(t *testing.T) []byte {
	t.Helper()
	select {
	case frame := <-s.audio:
		return frame
	case <-time.After(waitTimeout):
		t.Fatal("the mic audio the browser sent never reached Sarvam, so the agent is deaf")
		return nil
	}
}

type ttsConfigRecord struct {
	Language   string `json:"language_code"`
	Speaker    string `json:"speaker"`
	Model      string `json:"model"`
	SampleRate string `json:"speech_sample_rate"`
	Codec      string `json:"output_audio_codec"`
}

type fakeTTS struct {
	query  url.Values
	config ttsConfigRecord

	mu    sync.Mutex
	texts []string
}

func (s *fakeTTS) record(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
}

func (s *fakeTTS) spoken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

func (f *fakeSarvam) handleTTS(w http.ResponseWriter, r *http.Request) {
	f.checkKey(r)

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		f.errorf("accepting the speech socket: %v", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)
	ctx := r.Context()

	_, payload, err := conn.Read(ctx)
	if err != nil {
		return
	}
	var opening struct {
		Type string          `json:"type"`
		Data ttsConfigRecord `json:"data"`
	}
	if err := json.Unmarshal(payload, &opening); err != nil || opening.Type != "config" {
		f.errorf("the first speech message was %s, but bulbul expects a config message first", payload)
		return
	}

	tts := &fakeTTS{query: r.URL.Query(), config: opening.Data}
	f.mu.Lock()
	f.ttsSeen = append(f.ttsSeen, tts)
	f.mu.Unlock()
	select {
	case f.tts <- tts:
	default:
		f.errorf("the session opened more speech sockets than the test expected")
	}

	var pending []byte
	flushes := 0

	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var msg struct {
			Type string `json:"type"`
			Data struct {
				Text string `json:"text"`
			} `json:"data"`
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			f.errorf("the session sent speech json the API would reject: %s", payload)
			return
		}

		switch msg.Type {
		case "text":
			tts.record(msg.Data.Text)
			pending = append(pending, msg.Data.Text...)

		case "flush":
			flushes++
			action := f.actionFor(flushes)
			if action == ttsStall {
				continue
			}
			if action == ttsHangUp {
				conn.Close(websocket.StatusInternalError, "fake tts hung up")
				return
			}
			if action == ttsSpeak && len(pending) > 0 {
				clip := base64.StdEncoding.EncodeToString(pending)
				if err := conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"audio","data":{"audio":"`+clip+`","content_type":"audio/x-raw"}}`)); err != nil {
					return
				}
			}
			pending = nil
			if err := conn.Write(ctx, websocket.MessageText, []byte(ttsFinal)); err != nil {
				return
			}
		}
	}
}

func (f *fakeSarvam) handleChat(w http.ResponseWriter, r *http.Request) {
	f.checkKey(r)

	var body struct {
		Model     string           `json:"model"`
		MaxTokens int              `json:"max_tokens"`
		Stream    bool             `json:"stream"`
		Messages  []models.Message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.errorf("the session sent a chat request the API would reject: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !body.Stream {
		f.errorf("the chat request asked for a whole answer at once: a voice agent has to stream tokens to keep latency down")
	}

	call := chatCall{Model: body.Model, MaxTokens: body.MaxTokens, Messages: body.Messages}
	select {
	case f.chats <- call:
	default:
		f.errorf("the session made more chat requests than the test expected")
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		f.errorf("the fake chat endpoint cannot stream")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	emit := func(token string) {
		chunk, err := json.Marshal(map[string]any{
			"id":     "fake-chat",
			"object": "chat.completion.chunk",
			"model":  body.Model,
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]string{"role": "assistant", "content": token},
			}},
		})
		if err != nil {
			f.errorf("encoding a chat chunk: %v", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}

	f.scriptFor(call.lastUser())(emit, r.Context().Done())
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
