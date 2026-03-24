package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gorilla/websocket"
)

// hasSpokenChar ensures the token contains at least one alphanumeric or unicode letter character.
var hasSpokenChar = regexp.MustCompile(`[a-zA-Z0-9\p{L}]`)

// TTSChunk maps pipeline output (replicated from ws for loose coupling)
type TTSChunk []byte

// 1. The Config Envelope (Send this once upon dialing the WS)
type SarvamTTSConfig struct {
	Type string `json:"type"` // Must be "config"
	Data struct {
		Speaker            string  `json:"speaker"`              // e.g., "shubh"
		TargetLanguageCode string  `json:"target_language_code"` // e.g., "en-IN"
		Pace               float64 `json:"pace,omitempty"`
	} `json:"data"`
}

// 2. The Text Chunk Envelope (Send this every time Groq yields a sentence)
type SarvamTTSText struct {
	Type string `json:"type"` // Must be "text"
	Data struct {
		Text string `json:"text"`
	} `json:"data"`
}

// StreamSarvamTTS manages a bidirectional WebSocket to Sarvam AI for real-time bulbul:v3 TTS.
func StreamSarvamTTS(ctx context.Context, textChan <-chan string, ttsChan chan<- TTSChunk, langCode string) error {
	apiKey := os.Getenv("SARVAM_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("missing SARVAM_API_KEY, TTS socket failed")
	}

	if langCode == "" {
		langCode = "en-IN"
	}

	url := "wss://api.sarvam.ai/text-to-speech/ws?model=bulbul:v3"
	reqHeader := http.Header{}
	reqHeader.Add("API-Subscription-Key", apiKey)

	conn, _, err := websocket.DefaultDialer.Dial(url, reqHeader)
	if err != nil {
		return fmt.Errorf("failed to dial Sarvam WS: %v", err)
	}
	defer conn.Close()

	config := SarvamTTSConfig{Type: "config"}
	config.Data.Speaker = "shubh"
	config.Data.TargetLanguageCode = langCode

	if err := conn.WriteJSON(config); err != nil {
		return fmt.Errorf("failed to send config: %v", err)
	}

	readerDone := make(chan struct{})

	// Background Reader
	go func() {
		defer close(readerDone)
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				// Suppress harmless error after flush closes the connection
				if !strings.Contains(err.Error(), "use of closed network connection") {
					log.Println("Sarvam WS closed or read error:", err)
				}
				return
			}

			if mt == websocket.TextMessage && len(msg) > 0 {
				// Parse the Sarvam TTS response envelope
				var sarvamResp struct {
					Type string `json:"type"`
					Data struct {
						Audio string `json:"audio"` // base64 MP3 chunk
					} `json:"data"`
				}
				if err := json.Unmarshal(msg, &sarvamResp); err == nil {
					// "event" type signals flush completion — terminate cleanly
					if sarvamResp.Type == "event" {
						return
					}
					// "audio" type — extract ONLY the base64 string
					if sarvamResp.Type == "audio" && sarvamResp.Data.Audio != "" {
						select {
						case <-ctx.Done():
							return
						case ttsChan <- TTSChunk(sarvamResp.Data.Audio):
						}
					}
				}
			} else if mt == websocket.BinaryMessage && len(msg) > 0 {
				select {
				case <-ctx.Done():
					return
				case ttsChan <- TTSChunk(msg):
				}
			}
		}
	}()

	// Foreground Writer
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readerDone:
			return nil
		case token, ok := <-textChan:
			if !ok {
				// Send Flush Signal so Sarvam knows to finish synthesizing and close
				_ = conn.WriteJSON(map[string]string{"type": "flush"})
				
				select {
				case <-ctx.Done():
					return nil
				case <-readerDone:
					return nil
				}
			}

			cleanedText := strings.TrimSpace(token)
			if cleanedText == "" || !hasSpokenChar.MatchString(cleanedText) {
				continue
			}

			textPayload := SarvamTTSText{Type: "text"}
			textPayload.Data.Text = cleanedText
			if err := conn.WriteJSON(textPayload); err != nil {
				return err
			}
		}
	}
}
