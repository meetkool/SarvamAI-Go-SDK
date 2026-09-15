package models

type SpeechRequest struct {
	Model    Model
	Voice    string
	Text     string
	Language Language
	Format   Format

	Speed       *float64
	Pitch       *float64
	Loudness    *float64
	Temperature *float64
	Preprocess  *bool
	Dictionary  string

	MinBufferSize  int
	MaxChunkLength int
	SentenceLimit  int
}

type AudioChunk struct {
	Bytes []byte
	Index int
}
