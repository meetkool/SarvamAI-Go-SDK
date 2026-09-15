package models

import "encoding/json"

const (
	ChatSarvam105B              Model = "sarvam-105b"
	ChatSarvam105BConversations Model = "sarvam-105b-conversations"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ReasoningEffort string

const (
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
	ReasoningOff    ReasoningEffort = "off"
)

type Message struct {
	Role             Role       `json:"role"`
	Content          string     `json:"content"`
	Name             string     `json:"name,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

func SystemMessage(content string) Message    { return Message{Role: RoleSystem, Content: content} }
func UserMessage(content string) Message      { return Message{Role: RoleUser, Content: content} }
func AssistantMessage(content string) Message { return Message{Role: RoleAssistant, Content: content} }

func ToolMessage(callID, content string) Message {
	return Message{Role: RoleTool, Content: content, ToolCallID: callID}
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (f FunctionCall) ParseArguments(into any) error {
	return json.Unmarshal([]byte(f.Arguments), into)
}

type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      *bool  `json:"strict,omitempty"`
}

func FunctionTool(name, description string, parameters any) Tool {
	return Tool{
		Type:     "function",
		Function: FunctionDefinition{Name: name, Description: description, Parameters: parameters},
	}
}

type ChatRequest struct {
	Model    Model
	Messages []Message

	Temperature      *float64
	TopP             *float64
	MaxTokens        int
	Stop             []string
	N                int
	Seed             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Tools            []Tool
	ToolChoice       any
	ResponseFormat   any

	ReasoningEffort ReasoningEffort
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *Usage       `json:"usage"`
}

type ChatChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

func (r *ChatResponse) Content() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

func (r *ChatResponse) Reasoning() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.ReasoningContent
}

func (r *ChatResponse) ToolCalls() []ToolCall {
	if len(r.Choices) == 0 {
		return nil
	}
	return r.Choices[0].Message.ToolCalls
}

func (r *ChatResponse) HasToolCalls() bool { return len(r.ToolCalls()) > 0 }

func (r *ChatResponse) FinishReason() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].FinishReason
}

type ChatChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
	Usage   *Usage            `json:"usage"`
}

type ChatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

type ChatDelta struct {
	Role             Role            `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	ToolCalls        []ToolCallDelta `json:"tool_calls"`
}

type ToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (c ChatChunk) Content() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return c.Choices[0].Delta.Content
}

func (c ChatChunk) Reasoning() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return c.Choices[0].Delta.ReasoningContent
}

func (c ChatChunk) Done() bool {
	return len(c.Choices) > 0 && c.Choices[0].FinishReason != ""
}
