package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	sarvam "github.com/crynta/sarvam-go-sdk/src"
	"github.com/crynta/sarvam-go-sdk/src/models"
)

func main() {
	client, err := sarvam.New(sarvam.WithAPIKey("not-a-real-key"))
	if err != nil {
		log.Fatal(err)
	}

	_, err = client.Speech.Create(context.Background(), &models.SpeechRequest{
		Model:    models.TTSBulbulV3,
		Voice:    "shubh",
		Language: models.LangHindi,
		Text:     "Namaste!",
	})

	switch {
	case err == nil:
		fmt.Println("that worked, which was not the plan")
		return
	case errors.Is(err, sarvam.ErrUnauthorized):
		fmt.Println("the API key is missing or wrong")
	case errors.Is(err, sarvam.ErrQuotaExceeded):
		fmt.Println("the account is out of credits")
	case errors.Is(err, sarvam.ErrRateLimited):
		var rateErr *sarvam.RateLimitError
		if errors.As(err, &rateErr) {
			fmt.Println("rate limited; wait", rateErr.RetryAfter)
		}
	case errors.Is(err, sarvam.ErrInvalidRequest):
		fmt.Println("the request was rejected:", err)
	case errors.Is(err, sarvam.ErrServer):
		fmt.Println("Sarvam had a server problem")
	case errors.Is(err, sarvam.ErrTimeout):
		fmt.Println("the request timed out")
	}

	var apiErr *sarvam.APIError
	if errors.As(err, &apiErr) {
		fmt.Printf("HTTP %d, code %q, request %q\n", apiErr.StatusCode, apiErr.Code, apiErr.RequestID)
		fmt.Println("message:", apiErr.Message)
	}

	var retryErr *sarvam.MaxRetriesError
	if errors.As(err, &retryErr) {
		fmt.Printf("gave up after %d attempts\n", retryErr.Attempts)
	}
}
