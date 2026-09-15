package sarvam

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

const chatPath = "/v1/chat/completions"

type ChatService struct{ client *Client }

type chatBody struct {
	Model            models.Model     `json:"model"`
	Messages         []models.Message `json:"messages"`
	Stream           bool             `json:"stream,omitempty"`
	Temperature      *float64         `json:"temperature,omitempty"`
	TopP             *float64         `json:"top_p,omitempty"`
	MaxTokens        int              `json:"max_tokens,omitempty"`
	Stop             []string         `json:"stop,omitempty"`
	N                int              `json:"n,omitempty"`
	Seed             *int             `json:"seed,omitempty"`
	FrequencyPenalty *float64         `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64         `json:"presence_penalty,omitempty"`
	Tools            []models.Tool    `json:"tools,omitempty"`
	ToolChoice       any              `json:"tool_choice,omitempty"`
	ResponseFormat   any              `json:"response_format,omitempty"`
	ReasoningEffort  json.RawMessage  `json:"reasoning_effort,omitempty"`
}

func (c *ChatService) newBody(req *models.ChatRequest, stream bool) (*chatBody, error) {
	if req == nil || len(req.Messages) == 0 {
		return nil, invalidRequest("Messages is required")
	}
	if req.Model == "" {
		return nil, invalidRequest("Model is required, e.g. models.ChatSarvam105B")
	}

	body := &chatBody{
		Model:            req.Model,
		Messages:         req.Messages,
		Stream:           stream,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		MaxTokens:        req.MaxTokens,
		Stop:             req.Stop,
		N:                req.N,
		Seed:             req.Seed,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
		Tools:            req.Tools,
		ToolChoice:       req.ToolChoice,
		ResponseFormat:   req.ResponseFormat,
	}

	switch req.ReasoningEffort {
	case "":
	case models.ReasoningOff:
		body.ReasoningEffort = json.RawMessage("null")
	default:
		body.ReasoningEffort = json.RawMessage(`"` + string(req.ReasoningEffort) + `"`)
	}
	return body, nil
}

func (c *ChatService) Create(ctx context.Context, req *models.ChatRequest) (*models.ChatResponse, error) {
	body, err := c.newBody(req, false)
	if err != nil {
		return nil, err
	}

	var out models.ChatResponse
	if err := c.client.doJSON(ctx, http.MethodPost, chatPath, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ChatService) Stream(ctx context.Context, req *models.ChatRequest) (*Stream[models.ChatChunk], error) {
	body, err := c.newBody(req, true)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := c.client.openStream(ctx, chatPath, body)
	if err != nil {
		return nil, err
	}
	stream := newStream[models.ChatChunk](resp.Body)
	stream.onFirst = func() {
		c.client.emit(Metric{Name: MetricFirstToken, Endpoint: chatPath, Duration: time.Since(started)})
	}
	return stream, nil
}
