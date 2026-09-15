package sarvam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"bytes"
	"github.com/coder/websocket"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func wsTestClient(t *testing.T, handler func(*websocket.Conn)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.CloseNow()
		handler(conn)
	}))
	t.Cleanup(srv.Close)

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSpeechCreate(t *testing.T) {
	var sent speechBody
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/text-to-speech" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		clip := base64.StdEncoding.EncodeToString(testWAV(24000, time.Second))
		json.NewEncoder(w).Encode(map[string]any{"request_id": "req-1", "audios": []string{clip}})
	})

	audio, err := client.Speech.Create(context.Background(), &models.SpeechRequest{
		Model:    models.TTSBulbulV3,
		Voice:    "shubh",
		Language: models.LangHindi,
		Text:     "Namaste",
		Format:   models.WAV(24000),
		Speed:    models.Float(1.1),
	})
	if err != nil {
		t.Fatal(err)
	}

	if sent.Text != "Namaste" || sent.LanguageCode != models.LangHindi || sent.Speaker != "shubh" {
		t.Errorf("request body = %+v", sent)
	}
	if sent.Pace == nil || *sent.Pace != 1.1 {
		t.Errorf("pace = %v, want 1.1", sent.Pace)
	}
	if sent.SpeechSampleRate != 24000 || sent.OutputAudioCodec != models.CodecWAV {
		t.Errorf("format fields = %d %q", sent.SpeechSampleRate, sent.OutputAudioCodec)
	}
	if sent.OutputAudioBitrate != "" {
		t.Errorf("the one-shot endpoint takes no bitrate, got %q", sent.OutputAudioBitrate)
	}
	if got := audio.Duration(); got != time.Second {
		t.Errorf("duration = %v, want 1s", got)
	}
}

func TestSpeechCreateJoinsSeveralClips(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		half := base64.StdEncoding.EncodeToString(testWAV(24000, 500*time.Millisecond))
		json.NewEncoder(w).Encode(map[string]any{"audios": []string{half, half}})
	})

	audio, err := client.Speech.Create(context.Background(), &models.SpeechRequest{
		Language: models.LangHindi, Text: "Namaste", Format: models.WAV(24000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := audio.Duration(); got != time.Second {
		t.Fatalf("duration = %v, want the two halves joined into 1s", got)
	}
}

func TestSpeechCreateChecksTheRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an invalid request must not reach the server")
	})

	for _, req := range []*models.SpeechRequest{
		{Language: models.LangHindi},
		{Text: "Namaste"},
	} {
		if _, err := client.Speech.Create(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("got %v, want ErrInvalidRequest", err)
		}
	}
}

func TestSpeechStream(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/text-to-speech/stream" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var sent speechBody
		json.NewDecoder(r.Body).Decode(&sent)
		if sent.OutputAudioBitrate != "128k" {
			t.Errorf("bitrate = %q, want 128k", sent.OutputAudioBitrate)
		}

		w.Header().Set("Content-Type", "audio/mpeg")
		for _, part := range []string{"one", "two", "three"} {
			w.Write([]byte(part))
			w.(http.Flusher).Flush()
			time.Sleep(time.Millisecond)
		}
	})

	stream, err := client.Speech.Stream(context.Background(), &models.SpeechRequest{
		Language: models.LangEnglish, Text: "hello", Format: models.MP3(24000, 128),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var got strings.Builder
	for stream.Next() {
		got.Write(stream.Chunk().Bytes)
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if got.String() != "onetwothree" {
		t.Fatalf("stream gave %q", got.String())
	}
}

func TestSpeechDuplex(t *testing.T) {
	client := wsTestClient(t, func(conn *websocket.Conn) {
		ctx := context.Background()

		var config struct {
			Type string    `json:"type"`
			Data ttsConfig `json:"data"`
		}
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		json.Unmarshal(payload, &config)
		if config.Type != "config" || config.Data.Speaker != "shubh" {
			t.Errorf("first message = %+v, want the config", config)
		}

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
			json.Unmarshal(payload, &msg)

			switch msg.Type {
			case "text":
				audio := base64.StdEncoding.EncodeToString([]byte(msg.Data.Text))
				conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"audio","data":{"audio":"`+audio+`","content_type":"audio/mp3"}}`))
			case "flush":
				conn.Write(ctx, websocket.MessageText,
					[]byte(`{"type":"event","data":{"event_type":"final"}}`))
			}
		}
	})

	ctx := context.Background()
	duplex, err := client.Speech.Duplex(ctx, &models.SpeechRequest{
		Voice: "shubh", Language: models.LangEnglish, Format: models.MP3(24000, 128),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	go func() {
		duplex.SendText(ctx, "hello ")
		duplex.SendText(ctx, "world")
		duplex.CloseSend(ctx)
	}()

	var got strings.Builder
	for duplex.Next() {
		got.Write(duplex.Chunk().Bytes)
	}
	if err := duplex.Err(); err != nil {
		t.Fatal(err)
	}
	if got.String() != "hello world" {
		t.Fatalf("duplex gave %q", got.String())
	}
}

func TestSpeechDuplexReportsServerErrors(t *testing.T) {
	client := wsTestClient(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		conn.Read(ctx)
		conn.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"error","data":{"message":"voice not available","code":422}}`))
	})

	duplex, err := client.Speech.Duplex(context.Background(), &models.SpeechRequest{
		Voice: "shubh", Language: models.LangEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	if duplex.Next() {
		t.Fatal("Next should stop on an error message")
	}
	var apiErr *APIError
	if !errors.As(duplex.Err(), &apiErr) || apiErr.StatusCode != 422 {
		t.Fatalf("Err = %v, want an *APIError with status 422", duplex.Err())
	}
}

func TestJoinAudio(t *testing.T) {
	half := testWAV(24000, 500*time.Millisecond)
	joined, err := joinAudio([][]byte{half, half}, models.WAV(24000))
	if err != nil {
		t.Fatal(err)
	}
	if got := models.NewAudio(joined, models.WAV(24000)).Duration(); got != time.Second {
		t.Fatalf("joined duration = %v, want 1s", got)
	}

	raw, err := joinAudio([][]byte{{1, 2}, {3, 4}}, models.MP3(24000, 128))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte{1, 2, 3, 4}) {
		t.Fatalf("joined mp3 = %v", raw)
	}
}

func TestSpeechDuplexSkipsUnspeakableText(t *testing.T) {
	sent := make(chan string, 8)
	done := make(chan struct{})

	client := wsTestClient(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		conn.Read(ctx)

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
			json.Unmarshal(payload, &msg)

			switch msg.Type {
			case "text":
				sent <- msg.Data.Text
			case "flush":
				close(done)
				return
			}
		}
	})

	ctx := context.Background()
	duplex, err := client.Speech.Duplex(ctx, &models.SpeechRequest{
		Voice: "shubh", Language: models.LangEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	for _, text := range []string{"", " ", "\n", "!", "।", "...", " ?! "} {
		if err := duplex.SendText(ctx, text); err != nil {
			t.Fatalf("SendText(%q) = %v", text, err)
		}
	}
	if err := duplex.SendText(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	duplex.Flush(ctx)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the server never saw the flush")
	}
	close(sent)

	var got []string
	for text := range sent {
		got = append(got, text)
	}
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("server received %q, want only [hello]: text with no letters makes the real API close the socket", got)
	}
}

func ttsEchoServer(t *testing.T, flushes chan<- string) *Client {
	return wsTestClient(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		conn.Read(ctx)

		audio := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
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
			json.Unmarshal(payload, &msg)

			switch msg.Type {
			case "text":
				if flushes != nil {
					flushes <- msg.Data.Text
				}
			case "flush":
				conn.Write(ctx, websocket.MessageText, []byte(`{"type":"audio","data":{"audio":"`+audio+`"}}`))
				conn.Write(ctx, websocket.MessageText, []byte(`{"type":"event","data":{"event_type":"final"}}`))
			}
		}
	})
}

func TestSendTokenAggregatesSentences(t *testing.T) {
	texts := make(chan string, 8)
	client := ttsEchoServer(t, texts)

	ctx := context.Background()
	duplex, err := client.Speech.Duplex(ctx, &models.SpeechRequest{
		Voice: "shubh", Language: models.LangEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	go func() {
		for _, token := range []string{"\n", "Hello", " there", ".", " Next", " one", "!"} {
			duplex.SendToken(ctx, token)
		}
		duplex.CloseSend(ctx)
	}()

	for duplex.Next() {
	}
	if err := duplex.Err(); err != nil {
		t.Fatal(err)
	}
	close(texts)

	var got []string
	for text := range texts {
		got = append(got, text)
	}
	want := []string{"Hello there.", " Next one!"}
	if !slices.Equal(got, want) {
		t.Fatalf("server received %q, want %q: tokens must be joined into sentences", got, want)
	}
}

func TestSpokenTextTracksConfirmedAudio(t *testing.T) {
	client := ttsEchoServer(t, nil)

	ctx := context.Background()
	duplex, err := client.Speech.Duplex(ctx, &models.SpeechRequest{
		Voice: "shubh", Language: models.LangEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	go func() {
		duplex.SendText(ctx, "Hello there.")
		duplex.Flush(ctx)
		time.Sleep(80 * time.Millisecond)
		duplex.CloseSend(ctx)
	}()

	chunks := 0
	for duplex.Next() {
		chunks++
	}
	if err := duplex.Err(); err != nil {
		t.Fatal(err)
	}
	if chunks == 0 {
		t.Fatal("no audio arrived")
	}
	if got := duplex.SpokenText(); got != "Hello there." {
		t.Fatalf("SpokenText = %q, want the text whose audio was confirmed", got)
	}
}

func TestDuplexResetReusesTheConnection(t *testing.T) {
	client := ttsEchoServer(t, nil)

	ctx := context.Background()
	duplex, err := client.Speech.Duplex(ctx, &models.SpeechRequest{
		Voice: "shubh", Language: models.LangEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	for _, line := range []string{"first turn.", "second turn."} {
		duplex.Reset()

		go func(text string) {
			duplex.SendText(ctx, text)
			duplex.CloseSend(ctx)
		}(line)

		chunks := 0
		for duplex.Next() {
			chunks++
		}
		if err := duplex.Err(); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if chunks == 0 {
			t.Fatalf("%s: no audio on the reused connection", line)
		}
		if got := duplex.SpokenText(); got != line {
			t.Fatalf("SpokenText = %q, want %q", got, line)
		}
	}
}
