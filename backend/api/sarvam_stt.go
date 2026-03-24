package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// StreamSarvamSTT streams audio to Sarvam AI and returns 'is_final' text strings to a channel.
func StreamSarvamSTT(ctx context.Context, audioChan <-chan []byte, transcriptChan chan<- string, langCode string) error {
	apiKey := strings.TrimSpace(os.Getenv("SARVAM_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("missing SARVAM_API_KEY for STT")
	}

	if langCode == "" || langCode == "Unknown" {
		langCode = "te-IN"
	}

	// Try saaras:v3 first, fall back to saarika:v2.5 if 403
	models := []string{"saaras:v3", "saarika:v2.5"}
	var conn *websocket.Conn

	for _, model := range models {
		params := fmt.Sprintf("model=%s&language-code=%s&sample_rate=16000&high_vad_sensitivity=true",
			url.QueryEscape(model), url.QueryEscape(langCode))
		if model == "saaras:v3" {
			params += "&mode=transcribe"
		}
		wsURL := "wss://api.sarvam.ai/speech-to-text/ws?" + params

		reqHeader := http.Header{}
		reqHeader.Set("Api-Subscription-Key", apiKey)

		var resp *http.Response
		var err error
		conn, resp, err = websocket.DefaultDialer.Dial(wsURL, reqHeader)
		if err == nil {
			log.Printf("STT connected with model=%s", model)
			break
		}

		statusCode := 0
		body := ""
		if resp != nil {
			statusCode = resp.StatusCode
			b, _ := io.ReadAll(resp.Body)
			body = string(b)
		}
		log.Printf("STT dial failed (model=%s): %v HTTP %d: %s", model, err, statusCode, body)
		conn = nil
	}

	if conn == nil {
		return fmt.Errorf("all STT model attempts failed — check SARVAM_API_KEY permissions")
	}
	defer conn.Close()

	// Sarvam STT requires a config payload as the very first message before any audio
	configPayload := map[string]interface{}{
		"type": "config",
		"data": map[string]string{
			"language_code": langCode,
		},
	}
	if err := conn.WriteJSON(configPayload); err != nil {
		log.Println("Failed to send STT config:", err)
	}

	readerDone := make(chan struct{})

	var lastTranscript string
	var mu sync.Mutex

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
					
					mu.Lock()
					lastTranscript = transcript
					mu.Unlock()

					if isFinal {
						select {
						case transcriptChan <- transcript:
							mu.Lock()
							lastTranscript = "" // Clear after sending
							mu.Unlock()
						default:
						}
					}
				}
			}
		}
	}()

	// Sender loop (Pipe continuous JSON encoded PCM chunks)
	hasSentAudio := false
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
			hasSentAudio = true
		
		case <-time.After(800 * time.Millisecond):
			// If no audio for 800ms AND we have audio to flush, force it.
			if hasSentAudio {
				mu.Lock()
				textToFlush := lastTranscript
				lastTranscript = ""
				mu.Unlock()

				if textToFlush != "" {
					log.Printf("[STT] Watchdog forcing transcript flush: %s\n", textToFlush)
					select {
					case transcriptChan <- textToFlush:
					default:
					}
				}

				// Also send the flush signal to Sarvam to keep them in sync
				flushMsg := map[string]string{"type": "flush"}
				conn.WriteJSON(flushMsg)
				hasSentAudio = false
			}
		}
	}
}
