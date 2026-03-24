package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/supabase-community/supabase-go"
)

type ExtractedData map[string]interface{}

// ExtractCallData runs in the background. It takes the full transcript, asks Groq to extract lead info,
// and saves the result to the collected_leads table in Supabase.
func ExtractCallData(ctx context.Context, campaignID, userPhone, transcript string) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" || transcript == "" || campaignID == "" {
		log.Println("Skipping extraction: missing config or empty transcript")
		return
	}

	systemInstruction := `You are an expert CRM data extractor. 
Review the following call transcript and extract key information.
You must output STRICT JSON. Include fields like "name", "appointment_time", "contact_info", "intent", and "summary".
If a field is not mentioned, omit it or set to null.
Return ONLY valid JSON and nothing else.`

	url := "https://api.groq.com/openai/v1/chat/completions"
	payload := map[string]interface{}{
		"model": "llama-3.1-8b-instant",
		"messages": []map[string]string{
			{"role": "system", "content": systemInstruction},
			{"role": "user", "content": "Transcript:\n" + transcript},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0.1,
	}

	payloadBytes, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Println("Extraction failed: could not create request", err)
		return
	}

	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Println("Extraction failed: Groq request error", err)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var groqResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &groqResp); err != nil || len(groqResp.Choices) == 0 {
		log.Println("Extraction failed: invalid response", string(bodyBytes))
		return
	}

	content := groqResp.Choices[0].Message.Content

	var extractedData ExtractedData
	if err := json.Unmarshal([]byte(content), &extractedData); err != nil {
		log.Println("Extraction failed: invalid JSON output", content)
		return
	}

	// Save to Supabase
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")
	if supabaseURL == "" || supabaseKey == "" {
		log.Println("Skipping save: missing Supabase credentials")
		return
	}

	dbClient, err := supabase.NewClient(supabaseURL, supabaseKey, &supabase.ClientOptions{})
	if err != nil {
		log.Println("Skipping save: supabase init error", err)
		return
	}

	insertData := map[string]interface{}{
		"campaign_id":     campaignID,
		"user_phone":      userPhone,
		"extracted_data":  extractedData,
		"call_transcript": transcript,
	}

	var inserted []map[string]interface{}
	_, err = dbClient.From("collected_leads").Insert(insertData, false, "exact", "representation", "").ExecuteTo(&inserted)
	if err != nil {
		log.Println("Failed to completely insert lead data into Supabase:", err)
	} else {
		log.Println("Successfully saved post-call extracted lead data.")
	}
}
