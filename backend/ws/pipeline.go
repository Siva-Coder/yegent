package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"yegent-backend/api" // Make sure to use your project's module name. wait, I'll use relative or just "yegent-backend/api" if it was that. Wait, the original pipeline.go used what? Let me check how it imported api.

	"github.com/gofiber/contrib/websocket"
	"github.com/supabase-community/supabase-go"
	"os"
)

type Campaign struct {
	ID                 string
	Persona            string
	Objective          string
	Greeting           string
	LanguagePreference string
}



// HandleCallSocket bridges the Fiber HTTP request into the websocket connection loop
var HandleCallSocket = websocket.New(func(c *websocket.Conn) {
	campaignID := c.Query("campaign_id")
	log.Println("New AI Voice Call connected! CampaignID:", campaignID)
	RunPipeline(c, campaignID)
})

// RunPipeline orchestrates the continuous duplex streaming phase 8 architecture
func RunPipeline(conn *websocket.Conn, campaignID string) {
	defer conn.Close()

	var fullTranscript strings.Builder
	var transcriptMutex sync.Mutex

	defer func() {
		transcriptMutex.Lock()
		finalText := fullTranscript.String()
		transcriptMutex.Unlock()

		if strings.TrimSpace(finalText) != "" && campaignID != "" {
			log.Println("Call ended. Spawning background lead extractor...")
			go api.ExtractCallData(context.Background(), campaignID, "WebRTC Tester", finalText)
		}
	}()

	campaign := &Campaign{
		Persona:            "You are a helpful AI receptionist.",
		Objective:          "Help the user.",
		Greeting:           "Hello! How can I help you today?",
		LanguagePreference: "te-IN",
	}

	if campaignID != "" {
		supabaseURL := os.Getenv("SUPABASE_URL")
		supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")
		if supabaseURL != "" && supabaseKey != "" {
			dbClient, err := supabase.NewClient(supabaseURL, supabaseKey, &supabase.ClientOptions{})
			if err == nil {
				var results []map[string]interface{}
				_, err = dbClient.From("campaigns").Select("*", "exact", false).Eq("id", campaignID).ExecuteTo(&results)
				
				if err == nil && len(results) > 0 {
					row := results[0]
					if v, ok := row["persona"].(string); ok { campaign.Persona = v }
					if v, ok := row["objective"].(string); ok { campaign.Objective = v }
					if v, ok := row["greeting"].(string); ok { campaign.Greeting = v }
					if v, ok := row["language_preference"].(string); ok { campaign.LanguagePreference = v }
					log.Println("Loaded Campaign:", campaign.LanguagePreference)
				} else if err != nil {
					log.Println("Failed to fetch campaign:", err)
				}
			}
		}
	}

	// 1. Map the language preference to a strict LLM instruction
	languageRule := "pure English"
	fillerWords := "'Hmm,', 'Well,', 'Okay,'"

	switch campaign.LanguagePreference {
	case "te-IN":
		languageRule = "a highly casual Telugu-English code-mix using ONLY the English alphabet (Romanized Telugu). Example: 'Avunu, nenu check chestanu.' NEVER use native Telugu script."
		fillerWords = "'Hmm,', 'Sare,', 'Okay,'"
	case "hi-IN":
		languageRule = "a highly casual Hindi-English code-mix using ONLY the English alphabet (Hinglish). Example: 'Haan, main check karta hoon.' NEVER use native Hindi script (Devanagari)."
		fillerWords = "'Hmm,', 'Achha,', 'Theek hai,'"
	}

	// 2. Inject the dynamic rules into the system prompt
	systemPrompt := fmt.Sprintf(
		"You are %s. Your objective is: %s. "+
			"CRITICAL RULES: "+
			"1. You are speaking on a live phone call in India. "+
			"2. LANGUAGE: You MUST reply in %s "+
			"3. Use English for all complex software, business, and technical terms. Use the local language for conversational glue. "+
			"4. HUMAN CONVERSATION: Start your turns with natural filler words like %s. "+
			"5. Keep responses to a maximum of 2 short, punchy sentences. No lists or markdown.",
		campaign.Persona, campaign.Objective, languageRule, fillerWords,
	)

	pipelineCtx, cancelPipeline := context.WithCancel(context.Background())
	defer cancelPipeline()

	// Channels
	audioChan := make(chan []byte, 300)        // PCM from browser -> STT
	transcriptChan := make(chan string, 10)    // STT -> LLM
	ttsChan := make(chan api.TTSChunk, 100)    // TTS -> Browser

	// Helper to launch isolated TTS streams for precise boundaries
	spinupUtteranceTTS := func(ctx context.Context, tokens <-chan string) {
		go func() {
			if err := api.StreamSarvamTTS(ctx, tokens, ttsChan, campaign.LanguagePreference); err != nil {
				log.Println("Sarvam TTS Connection failed:", err)
			}
			select {
			case <-ctx.Done():
			case ttsChan <- api.TTSChunk([]byte(`{"type":"audio_end"}`)):
			}
		}()
	}

	// 1. Start Continuous STT Worker
	var lastTranscript strings.Builder
	var lastTranscriptMu sync.Mutex
	go func() {
		err := api.StreamSarvamSTT(pipelineCtx, audioChan, transcriptChan, campaign.LanguagePreference, &lastTranscript, &lastTranscriptMu)
		if err != nil {
			log.Println("STT Stream ending:", err)
		}
	}()

	// 2. Sender Loop: Forward TTS audio to Browser
	go func() {
		for {
			select {
			case <-pipelineCtx.Done():
				return
			case chunk, ok := <-ttsChan:
				if !ok { return }

				if string(chunk) == `{"type":"audio_end"}` {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"audio_end"}`)); err != nil {
						log.Println("WS write error:", err)
						return
					}
					continue
				}

				res := map[string]interface{}{
					"type": "audio",
					"data": map[string]string{"audio": string(chunk)},
				}
				if err := conn.WriteJSON(res); err != nil {
					log.Println("WS write error:", err)
					return
				}
			}
		}
	}()

	var llmCancel context.CancelFunc
	var llmMutex sync.Mutex

	// 4. Orchestrator Loop: Handle incoming `is_final` transcripts
	go func() {
		for {
			select {
			case <-pipelineCtx.Done():
				return
			case transcript, ok := <-transcriptChan:
				if !ok { return }
				
				transcript = strings.TrimSpace(transcript)
				if transcript == "" { continue }
				log.Println("STT Final:", transcript)

				transcriptMutex.Lock()
				fullTranscript.WriteString(fmt.Sprintf("\nUser: %s\n", transcript))
				transcriptMutex.Unlock()

				llmMutex.Lock()
				if llmCancel != nil {
					llmCancel() // Kill previous generation
				}
				
				var reqCtx context.Context
				reqCtx, llmCancel = context.WithCancel(pipelineCtx)
				llmMutex.Unlock()

				userMessage := transcript
				
				// Phase 14: Semantic Search (RAG)
				contextText, _ := api.SearchKnowledgeBase(reqCtx, userMessage)
				if contextText != "" {
					log.Println("RAG context retrieved, enriching prompt.")
					userMessage = fmt.Sprintf("KNOWLEDGE BASE CONTEXT:\n%s\n\nUSER SAID: %s", contextText, userMessage)
				}
				
				log.Println("AI evaluating:", transcript)

				llmLocalChan := make(chan string, 100)
				go api.StreamLLM(reqCtx, systemPrompt, userMessage, llmLocalChan)

				uttTokenQueue := make(chan string, 100)
				spinupUtteranceTTS(reqCtx, uttTokenQueue)

				// Pipe LLM to TTS
				go func(ctx context.Context, in <-chan string, out chan<- string) {
					var aiResponse strings.Builder
					var sentenceBuffer strings.Builder

					defer func() {
						close(out) // Triggers TTS flush
						transcriptMutex.Lock()
						finalAI := strings.TrimSpace(aiResponse.String())
						if finalAI != "" {
							fullTranscript.WriteString(fmt.Sprintf("AI: %s\n", finalAI))
						}
						transcriptMutex.Unlock()
					}()

					for {
						select {
						case <-ctx.Done():
							return
						case token, ok := <-in:
							if !ok {
								chunk := strings.TrimSpace(sentenceBuffer.String())
								if len(chunk) > 0 {
									select {
									case <-ctx.Done():
										return
									case out <- chunk:
									}
								}
								return
							}

							sentenceBuffer.WriteString(token)
							aiResponse.WriteString(token)

							if strings.ContainsAny(token, ".,?!:\n") {
								chunk := strings.TrimSpace(sentenceBuffer.String())
								if len(chunk) > 0 {
									select {
									case <-ctx.Done():
										return
									case out <- chunk:
									}
								}
								sentenceBuffer.Reset()
							}
						}
					}
				}(reqCtx, llmLocalChan, uttTokenQueue)
			}
		}
	}()

	// 5. Instantly Speak the Greeting — split into sentences for zero-delay first word
	if campaign.Greeting != "" {
		transcriptMutex.Lock()
		fullTranscript.WriteString(fmt.Sprintf("AI: %s\n", campaign.Greeting))
		transcriptMutex.Unlock()

		log.Println("Sending Initial Greeting to TTS:", campaign.Greeting)

		// Split by sentence-ending punctuation so TTS starts on the first sentence immediately
		sentenceRe := regexp.MustCompile(`[^.!?]+[.!?]*`)
		parts := sentenceRe.FindAllString(campaign.Greeting, -1)
		if len(parts) == 0 {
			parts = []string{campaign.Greeting}
		}

		greetQueue := make(chan string, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				greetQueue <- part
			}
		}
		close(greetQueue)
		spinupUtteranceTTS(pipelineCtx, greetQueue)
	}

	// 6. Main Receiver Loop: Listen for Browser binary PCM frames and Barge-ins
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Client disconnected:", err)
			break
		}

		if mt == websocket.BinaryMessage {
			// Continuous PCM stream -> STT
			log.Printf("-> [Pipeline] Received %d bytes (Binary)\n", len(msg))
			select {
			case audioChan <- msg:
			default:
				log.Println("Warning: audioChan full, dropping frame")
			}
		} else if mt == websocket.TextMessage {
			var signal struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(msg, &signal); err != nil {
				continue
			}

			if signal.Type == "barge_in" {
				llmMutex.Lock()
				if llmCancel != nil {
					llmCancel()
				}
				llmMutex.Unlock()
				log.Println("Barge-in detected, halted AI output.")
				conn.WriteMessage(websocket.TextMessage, []byte("barge_in_ok"))
			} else if signal.Type == "speech_end" {
				// Rapid endpointing: Pull what we have and trigger Gemini
				lastTranscriptMu.Lock()
				text := strings.TrimSpace(lastTranscript.String())
				lastTranscript.Reset()
				lastTranscriptMu.Unlock()

				if text != "" {
					log.Println("Speech End (Hark): Triggering LLM for:", text)
					select {
					case transcriptChan <- text:
					default:
					}
				}
			}
		}
	}
}
