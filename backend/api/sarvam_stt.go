package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// StreamSarvamSTT streams audio to Sarvam AI and returns 'is_final' text strings to a channel.
// It also maintains a thread-safe lastTranscript for instant endpointing.
func StreamSarvamSTT(ctx context.Context, audioChan <-chan []byte, transcriptChan chan<- string, langCode string, lastTranscript *strings.Builder, mu *sync.Mutex) error {
	apiKey := strings.TrimSpace(os.Getenv("SARVAM_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("missing SARVAM_API_KEY for STT")
	}

	if langCode == "" || langCode == "Unknown" {
		langCode = "te-IN"
	}

	modelCtx := "saaras:v3"
	params := fmt.Sprintf("model=%s&language-code=%s&sample_rate=16000&high_vad_sensitivity=true&mode=transcribe",
		url.QueryEscape(modelCtx), url.QueryEscape(langCode))
	wsURL := "wss://api.sarvam.ai/speech-to-text/ws?" + params

	dialer := websocket.DefaultDialer
	headers := http.Header{}
	headers.Add("Api-Subscription-Key", apiKey)

	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		log.Printf("STT Connection failed with %s, trying fallback model saarika:v2.5...", modelCtx)
		modelCtx = "saarika:v2.5"
		params = fmt.Sprintf("model=%s&language-code=%s&sample_rate=16000&high_vad_sensitivity=true",
			url.QueryEscape(modelCtx), url.QueryEscape(langCode))
		wsURL = "wss://api.sarvam.ai/speech-to-text/ws?" + params
		conn, _, err = dialer.Dial(wsURL, headers)
		if err != nil {
			return fmt.Errorf("all STT model attempts failed: %w", err)
		}
	}
	defer conn.Close()

	log.Printf("STT connected with model=%s\n", modelCtx)

	// Sarvam STT requires a config payload
	configPayload := map[string]interface{}{
		"type": "config",
		"data": map[string]string{
			"language_code": langCode,
			"encoding":      "audio/wav",
		},
	}
	if err := conn.WriteJSON(configPayload); err != nil {
		log.Println("Failed to send STT config:", err)
	}

	readerDone := make(chan struct{})

	// Receiver loop
	go func() {
		defer close(readerDone)
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				log.Println("STT Read Error:", err)
				return
			}
			if mt == websocket.TextMessage {
				log.Printf("[STT RAW] %s\n", string(msg))
				
				var raw struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					Transcript string `json:"transcript"`
					IsFinal    bool   `json:"is_final"`
					Data       struct {
						Transcript string `json:"transcript"`
						IsFinal    bool   `json:"is_final"`
					} `json:"data"`
				}

				if err := json.Unmarshal(msg, &raw); err != nil {
					continue
				}

				transcript := ""
				isFinal := false

				if raw.Transcript != "" {
					transcript = raw.Transcript
					isFinal = raw.IsFinal
				} else if raw.Text != "" {
					transcript = raw.Text
					isFinal = true
				}
				if raw.Data.Transcript != "" {
					transcript = raw.Data.Transcript
					isFinal = raw.Data.IsFinal
				}

				if transcript != "" {
					log.Printf("[STT] %s (isFinal: %v)\n", transcript, isFinal)
					
					// Thread-safe update of the latest partial transcript
					mu.Lock()
					lastTranscript.Reset()
					lastTranscript.WriteString(transcript)
					mu.Unlock()

					if isFinal {
						select {
						case transcriptChan <- transcript:
							mu.Lock()
							lastTranscript.Reset() // Clear after sending
							mu.Unlock()
						default:
						}
					}
				}
			}
		}
	}()

	// Sender loop (Pipe continuous JSON encoded PCM chunks)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readerDone:
			return nil
		case chunk, ok := <-audioChan:
			if !ok {
				return nil
			}

			// Send raw PCM frame wrapped in JSON
			msg := map[string]interface{}{
				"audio": map[string]interface{}{
					"data":        base64.StdEncoding.EncodeToString(chunk),
					"sample_rate": 16000,
					"encoding":    "audio/wav",
				},
			}
			if err := conn.WriteJSON(msg); err != nil {
				return err
			}
		}
	}
}
