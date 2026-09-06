package main

import (
	"context"
	"log"
	"os"
	"time"

	sarvam "github.com/crynta/sarvam-go-sdk"
)

func main() {
	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	duplex, err := client.Speech.Duplex(ctx, &sarvam.SpeechRequest{
		Model:    sarvam.TTSBulbulV3,
		Voice:    "shubh",
		Language: sarvam.LangEnglish,
		Format:   sarvam.MP3(24000, 128),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer duplex.Close()

	go func() {
		tokens := []string{"Sarvam ", "builds ", "voice ", "models ", "for ", "Indian ", "languages."}
		for _, token := range tokens {
			if err := duplex.SendText(ctx, token); err != nil {
				log.Print(err)
				return
			}
			time.Sleep(80 * time.Millisecond)
		}
		if err := duplex.CloseSend(ctx); err != nil {
			log.Print(err)
		}
	}()

	file, err := os.Create("duplex.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	for duplex.Next() {
		if _, err := file.Write(duplex.Chunk().Bytes); err != nil {
			log.Fatal(err)
		}
	}
	if err := duplex.Err(); err != nil {
		log.Fatal(err)
	}
	log.Println("saved duplex.mp3")
}
