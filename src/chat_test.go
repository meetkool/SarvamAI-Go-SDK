package sarvam

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func TestChatCreate(t *testing.T) {
	var sent chatBody
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"sarvam-105b",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Namaste!"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	})

	resp, err := client.Chat.Create(context.Background(), &models.ChatRequest{
		Model: models.ChatSarvam105B,
		Messages: []models.Message{
			models.SystemMessage("be brief"),
			models.UserMessage("hi"),
		},
		Temperature: models.Float(0.7),
	})
	if err != nil {
		t.Fatal(err)
	}

	if sent.Model != models.ChatSarvam105B || len(sent.Messages) != 2 {
		t.Errorf("request body = %+v", sent)
	}
	if sent.Messages[0].Role != models.RoleSystem || sent.Messages[1].Content != "hi" {
		t.Errorf("messages = %+v", sent.Messages)
	}
	if sent.Stream {
		t.Error("Create must not ask for a stream")
	}
	if resp.Content() != "Namaste!" {
		t.Errorf("Content = %q", resp.Content())
	}
	if resp.FinishReason() != "stop" {
		t.Errorf("FinishReason = %q", resp.FinishReason())
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 8 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestChatReasoningOffSendsNull(t *testing.T) {
	var raw []byte
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	})

	_, err := client.Chat.Create(context.Background(), &models.ChatRequest{
		Model:           models.ChatSarvam105BConversations,
		Messages:        []models.Message{models.UserMessage("hi")},
		ReasoningEffort: models.ReasoningOff,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reasoning_effort":null`) {
		t.Fatalf("thinking should be switched off with an explicit null, body was %s", raw)
	}
}

func TestChatStream(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var sent chatBody
		json.NewDecoder(r.Body).Decode(&sent)
		if !sent.Stream {
			t.Error("Stream must set stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"Na"}}]}`,
			`{"id":"1","choices":[{"index":0,"delta":{"content":"maste"}}]}`,
			`{"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`[DONE]`,
		} {
			w.Write([]byte("data: " + event + "\n\n"))
			w.(http.Flusher).Flush()
		}
	})

	stream, err := client.Chat.Stream(context.Background(), &models.ChatRequest{
		Model:    models.ChatSarvam105B,
		Messages: []models.Message{models.UserMessage("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var text strings.Builder
	var finished bool
	for stream.Next() {
		chunk := stream.Chunk()
		text.WriteString(chunk.Content())
		if chunk.Done() {
			finished = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if text.String() != "Namaste" {
		t.Fatalf("streamed text = %q", text.String())
	}
	if !finished {
		t.Error("the last chunk should report a finish reason")
	}
}

func TestChatToolCalls(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":null,
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Pune\"}"}}]},
			"finish_reason":"tool_calls"}]}`))
	})

	resp, err := client.Chat.Create(context.Background(), &models.ChatRequest{
		Model:    models.ChatSarvam105B,
		Messages: []models.Message{models.UserMessage("weather in Pune?")},
		Tools: []models.Tool{
			models.FunctionTool("get_weather", "current weather", map[string]any{"type": "object"}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !resp.HasToolCalls() {
		t.Fatal("want tool calls")
	}
	call := resp.ToolCalls()[0]
	if call.Function.Name != "get_weather" {
		t.Errorf("function = %q", call.Function.Name)
	}
	var args struct {
		City string `json:"city"`
	}
	if err := call.Function.ParseArguments(&args); err != nil {
		t.Fatal(err)
	}
	if args.City != "Pune" {
		t.Errorf("city = %q", args.City)
	}
}

func TestChatChecksTheRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an invalid request must not reach the server")
	})

	if _, err := client.Chat.Create(context.Background(), &models.ChatRequest{Model: models.ChatSarvam105B}); err == nil {
		t.Error("no messages should be refused")
	}
	if _, err := client.Chat.Create(context.Background(), &models.ChatRequest{
		Messages: []models.Message{models.UserMessage("hi")},
	}); err == nil {
		t.Error("no model should be refused")
	}
}
