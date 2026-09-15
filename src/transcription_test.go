package sarvam

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/crynta/sarvam-go-sdk/src/models"
)

func TestTranscriptionCreate(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speech-to-text" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		if got := r.FormValue("model"); got != string(models.STTSaarasV4) {
			t.Errorf("model = %q", got)
		}
		if got := r.FormValue("with_timestamps"); got != "true" {
			t.Errorf("with_timestamps = %q", got)
		}
		if got := r.FormValue("language_code"); got != string(models.LangHindi) {
			t.Errorf("language_code = %q", got)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		if header.Filename != "clip.wav" {
			t.Errorf("uploaded file name = %q", header.Filename)
		}
		if body, _ := io.ReadAll(file); len(body) == 0 {
			t.Error("no audio reached the server")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"request_id":           "req-1",
			"transcript":           "hello world",
			"language_code":        "en-IN",
			"language_probability": 0.97,
			"timestamps": map[string]any{
				"words":              []string{"hello", "world"},
				"start_time_seconds": []float64{0, 0.5},
				"end_time_seconds":   []float64{0.5, 1},
			},
		})
	})

	path := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(path, testWAV(16000, 2*time.Second), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := client.Transcription.Create(context.Background(), &models.TranscriptionRequest{
		Model:      models.STTSaarasV4,
		Audio:      models.FileInput(path),
		Language:   models.LangHindi,
		Timestamps: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "hello world" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Language != models.LangEnglish {
		t.Errorf("models.Language = %q", result.Language)
	}
	if len(result.Words) != 2 {
		t.Fatalf("Words = %d, want 2", len(result.Words))
	}
	if result.Words[1].Start != 500*time.Millisecond {
		t.Errorf("second start = %v, want 500ms", result.Words[1].Start)
	}
	if want := "00:00:00,000 --> 00:00:00,500"; !strings.Contains(result.SRT(), want) {
		t.Errorf("SRT missing %q:\n%s", want, result.SRT())
	}
	if !strings.HasPrefix(result.VTT(), "WEBVTT") {
		t.Errorf("VTT should start with WEBVTT:\n%s", result.VTT())
	}
}

func TestTranscriptionRejectsLongAudioBeforeUploading(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a clip over the limit must not be uploaded")
	})

	_, err := client.Transcription.Create(context.Background(), &models.TranscriptionRequest{
		Audio: models.BytesInput(testWAV(16000, 45*time.Second), models.WAV(16000)),
	})

	var tooLong *AudioTooLongError
	if !errors.As(err, &tooLong) || !errors.Is(err, ErrAudioTooLong) {
		t.Fatalf("got %v, want *AudioTooLongError", err)
	}
	if tooLong.Duration != 45*time.Second || tooLong.MaxDuration != restAudioLimit {
		t.Errorf("error said %v of %v", tooLong.Duration, tooLong.MaxDuration)
	}
}

func TestDiarizedResultGroupsSpeakers(t *testing.T) {
	var resp sttResponse
	body := `{"transcript":"hello there hi","diarized_transcript":{"entries":[
		{"transcript":"hello","start_time_seconds":0,"end_time_seconds":1,"speaker_id":"0"},
		{"transcript":"there","start_time_seconds":1,"end_time_seconds":2,"speaker_id":"0"},
		{"transcript":"hi","start_time_seconds":2,"end_time_seconds":3,"speaker_id":1}]}}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}

	turns := resp.result().SpeakerTurns()
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(turns))
	}
	if turns[0].Text != "hello there" || turns[0].Speaker != "0" {
		t.Errorf("first turn = %+v", turns[0])
	}
	if turns[1].Speaker != "1" {
		t.Errorf("a numeric speaker_id should still read as %q, got %q", "1", turns[1].Speaker)
	}
	if turns[0].End != time.Duration(2)*time.Second {
		t.Errorf("first turn ends at %v, want 2s", turns[0].End)
	}
}

func TestRealtimeStreamEvents(t *testing.T) {
	client := wsTestClient(t, func(conn *websocket.Conn) {
		ctx := context.Background()
		send := func(msg string) { conn.Write(ctx, websocket.MessageText, []byte(msg)) }

		send(`{"event":"session.begin","request_id":"req-9"}`)
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		send(`{"event":"vad.speech_start","utterance_idx":0}`)
		send(`{"event":"transcript.partial","utterance_idx":0,"text":"he","language":"en-IN"}`)
		send(`{"event":"transcript.final","utterance_idx":0,"text":"hello","language":"en-IN","start_s":0,"end_s":1.5}`)
		send(`{"event":"error","code":"slow_consumer","message":"buffer filling","is_fatal":false}`)
		send(`{"event":"session.end","audio_duration_s":3.5,"total_utterances":1}`)
	})

	ctx := context.Background()
	stream, err := client.Transcription.Stream(ctx, &models.TranscriptionStreamRequest{InputFormat: models.PCM16(16000)})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if err := stream.SendAudio(ctx, make([]byte, 320)); err != nil {
		t.Fatal(err)
	}

	var got []string
	for range 5 {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch e := event.(type) {
		case *models.SessionStarted:
			got = append(got, "start:"+e.RequestID)
		case *models.SpeechStarted:
			got = append(got, "speech")
		case *models.PartialTranscript:
			got = append(got, "partial:"+e.Text)
		case *models.FinalTranscript:
			got = append(got, "final:"+e.Text)
			if e.End != 1500*time.Millisecond {
				t.Errorf("final ends at %v, want 1.5s", e.End)
			}
		case *models.SessionEnded:
			got = append(got, "end")
			if e.AudioDuration != 3500*time.Millisecond {
				t.Errorf("billed %v, want 3.5s", e.AudioDuration)
			}
		}
	}

	want := []string{"start:req-9", "speech", "partial:he", "final:hello", "end"}
	if !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v (a warning should be skipped)", got, want)
	}
}

func TestRealtimeStreamChecksTheFormat(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Transcription.Stream(context.Background(), &models.TranscriptionStreamRequest{
		InputFormat: models.MP3(44100, 128),
	})

	var formatErr *FormatError
	if !errors.As(err, &formatErr) || !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("got %v, want *FormatError", err)
	}
	if len(formatErr.Supported) == 0 {
		t.Error("the error should list what is supported")
	}
}
