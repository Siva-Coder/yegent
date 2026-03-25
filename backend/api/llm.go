package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

// StreamLLM connects to Gemini's OpenAI-compatible streaming API and sends tokens to the channel
func StreamLLM(ctx context.Context, systemPrompt, userMessage string, targetChan chan<- string) {
	defer close(targetChan)

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Println("WARNING: GEMINI_API_KEY environment variable is not set. Using mock output.")
		targetChan <- "This is a fallback response because the Gemini API key is missing. Please add it to your .env file."
		return
	}

	// Google's OpenAI-compatible endpoint
	url := "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

	payload := map[string]interface{}{
		"model": "gemini-2.5-flash-lite", // User specifically asked for '2.5-flash-lite'
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMessage},
		},
		"stream": true,
	}

	payloadBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Println("Failed to formulate Gemini request:", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != context.Canceled {
			log.Println("Failed to connect to Gemini:", err)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Gemini returned non-200 status:", resp.Status)
		// Read body for error details
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		log.Println("Error body:", buf.String())
		return
	}

	reader := bufio.NewReader(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var sseResp struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &sseResp); err != nil {
			continue
		}

		if len(sseResp.Choices) > 0 && sseResp.Choices[0].Delta.Content != "" {
			targetChan <- sseResp.Choices[0].Delta.Content
		}
	}
}
