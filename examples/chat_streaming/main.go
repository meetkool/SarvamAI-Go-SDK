package main

import (
	"context"
	"fmt"
	"log"

	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func main() {
	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	stream, err := client.Chat.Stream(context.Background(), &models.ChatRequest{
		Model:           models.ChatSarvam105BConversations,
		Messages:        []models.Message{models.UserMessage("Tell me about Pune in 3 sentences.")},
		ReasoningEffort: models.ReasoningOff,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	for stream.Next() {
		fmt.Print(stream.Chunk().Content())
	}
	fmt.Println()
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
}
