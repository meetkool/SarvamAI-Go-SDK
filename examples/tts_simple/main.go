package main

import (
	"context"
	"fmt"
	"log"

	sarvam "github.com/crynta/sarvam-go-sdk"
)

func main() {
	c, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	a, err := c.Speech.Create(context.Background(), &sarvam.SpeechRequest{
		Model:    sarvam.TTSBulbulV3,
		Voice:    "shubh",
		Language: sarvam.LangHindi,
		Text:     "Namaste! Yah Sarvam Go SDK ka test hai.",
		Format:   sarvam.WAV(24000),
		Speed:    sarvam.Float(1.05),
	})
	if err != nil {
		log.Fatal(err)
	}
	a.Save("hello.wav")
	fmt.Printf("saved hello.wav: %d bytes, %.2fs\n", a.Len(), a.Duration().Seconds())
}
