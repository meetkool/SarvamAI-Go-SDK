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

	resp, err := client.Chat.Create(context.Background(), &models.ChatRequest{
		Model: models.ChatSarvam105B,
		Messages: []models.Message{
			models.SystemMessage("You are helpful. Answer in one short sentence."),
			models.UserMessage("Bharat me sabse lambi nadi kaunsi hai?"),
		},
		Temperature: models.Float(0.5),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Content())
	if resp.Usage != nil {
		fmt.Printf("\ntokens: %d in, %d out\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
}
