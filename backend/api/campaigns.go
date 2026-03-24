package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/supabase-community/supabase-go"
)

type GenerateRequest struct {
	Prompt   string `json:"prompt"`
}

type TimelineStep struct {
	Step  int    `json:"step"`
	Label string `json:"label"`
}

type GenerateResponse struct {
	Greeting      string         `json:"greeting"`
	SystemPrompt  string         `json:"system_prompt"`
	TimelineSteps []TimelineStep `json:"timeline_steps"`
}

// HandleGenerateCampaign calls Groq in JSON mode to generate the campaign config
func HandleGenerateCampaign(c *fiber.Ctx) error {
	var req GenerateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		return c.Status(500).JSON(fiber.Map{"error": "Missing GROQ_API_KEY"})
	}

	systemInstruction := `You are an expert AI architect creating phone agent campaigns.
The user describes their business. You must output STRICT JSON with this exact schema:
{
  "greeting": "The first sentence the AI says when the user connects (e.g. Hello! Welcome to Apollo Clinic...)",
  "system_prompt": "The exact 'You are [persona]... Your objective is [objective]' hidden prompt instruction for the AI. CRITICAL: You must use Romanized Code-Mixing. Write Telugu words using the English alphabet (e.g., 'Sare, nenu check chestanu'). Do NOT use native Telugu script. Blend English business terms with conversational Romanized Telugu natively. Never use lists or markdown. Max 4 sentences.",
  "timeline_steps": [
    {"step": 1, "label": "Greeting & Qualification"},
    {"step": 2, "label": "Answer Questions"},
    {"step": 3, "label": "Book Appointment"}
  ]
}
Return ONLY valid JSON and nothing else.`

	url := "https://api.groq.com/openai/v1/chat/completions"
	payload := map[string]interface{}{
		"model": "llama-3.1-8b-instant",
		"messages": []map[string]string{
			{"role": "system", "content": systemInstruction},
			{"role": "user", "content": req.Prompt},
		},
		"response_format": map[string]string{"type": "json_object"},
		"temperature":     0.2, // Low temp for more deterministic JSON
	}

	payloadBytes, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create request"})
	}

	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to call Groq"})
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
		log.Println("Groq Error Response:", string(bodyBytes))
		return c.Status(500).JSON(fiber.Map{"error": "Failed to parse Groq response"})
	}

	content := groqResp.Choices[0].Message.Content

	var genResp GenerateResponse
	if err := json.Unmarshal([]byte(content), &genResp); err != nil {
		log.Println("Invalid JSON from Groq:", content)
		return c.Status(500).JSON(fiber.Map{"error": "Groq returned invalid JSON schema"})
	}

	return c.JSON(genResp)
}

type UpdateCampaignRequest struct {
	Status string `json:"status"`
}

// HandleUpdateCampaign toggles the status (pause/resume/archive) of a campaign
func HandleUpdateCampaign(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing campaign id"})
	}

	var req UpdateCampaignRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")
	if supabaseURL == "" || supabaseKey == "" {
		return c.Status(500).JSON(fiber.Map{"error": "Missing Supabase env vars"})
	}

	client, err := supabase.NewClient(supabaseURL, supabaseKey, &supabase.ClientOptions{})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to init DB client"})
	}

	var updated []map[string]interface{}
	// Use map[string]interface{} so supabase-go accurately marshals real JSON obj.
	updateBody := map[string]interface{}{
		"status": req.Status,
	}

	_, err = client.From("campaigns").Update(updateBody, "exact", "").Eq("id", id).ExecuteTo(&updated)
	if err != nil {
		log.Println("Failed to update campaign status via Supabase:", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to update db"})
	}

	return c.JSON(fiber.Map{"success": true})
}
