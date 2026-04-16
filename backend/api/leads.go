package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type Lead struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	CampaignID string    `json:"campaign_id"`
	Name       string    `json:"name"`
	Phone      string    `json:"phone"`
	Email      string    `json:"email"`
	Summary    string    `json:"summary"`
	Contacted  bool      `json:"ai_contacted"`
	CreatedAt  time.Time `json:"created_at"`
}

func HandleListLeads(c *fiber.Ctx) error {
	initDB()
	userID, ok := c.Locals("user_id").(string)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	campaignID := c.Query("campaign_id")

	if dbPool == nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB connection not initialized"})
	}

	log.Printf("[CRM] Fetching leads for user: %s (Filter Campaign: %s)", userID, campaignID)

	query := "SELECT id, campaign_id, name, phone, email, summary, ai_contacted, created_at FROM leads WHERE user_id = $1"
	args := []interface{}{userID}

	if campaignID != "" {
		query += " AND campaign_id = $2"
		args = append(args, campaignID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := dbPool.Query(context.Background(), query, args...)
	if err != nil {
		log.Printf("List leads failed: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "DB query failed"})
	}
	defer rows.Close()

	leads := []Lead{}
	for rows.Next() {
		var l Lead
		var campID *string // Can be NULL
		err := rows.Scan(&l.ID, &campID, &l.Name, &l.Phone, &l.Email, &l.Summary, &l.Contacted, &l.CreatedAt)
		if err != nil {
			log.Printf("Scan lead failed: %v", err)
			continue
		}
		if campID != nil {
			l.CampaignID = *campID
		}
		l.UserID = userID
		leads = append(leads, l)
	}

	log.Printf("[CRM] Returning %d leads to frontend", len(leads))
	return c.JSON(leads)
}

func HandleBulkUploadLeads(c *fiber.Ctx) error {
	initDB()
	userID, ok := c.Locals("user_id").(string)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var payload struct {
		CampaignID string `json:"campaign_id"`
		Leads      []Lead `json:"leads"`
	}

	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid JSON payload"})
	}

	if len(payload.Leads) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "No leads provided"})
	}

	// Bulk insert using transaction or multiple execs
	// For simplicity and to avoid complex SQL generation, we'll loop with single inserts for now
	// but a real production app should use COPY or a bulk INSERT statement.
	successCount := 0
	for _, l := range payload.Leads {
		if l.Phone == "" {
			continue // Mandatory field
		}
		
		id := uuid.New().String()
		_, err := dbPool.Exec(context.Background(),
			"INSERT INTO leads (id, user_id, campaign_id, name, phone, email, summary) VALUES ($1, $2, $3, $4, $5, $6, $7)",
			id, userID, payload.CampaignID, l.Name, strings.TrimSpace(l.Phone), l.Email, l.Summary)
		
		if err == nil {
			successCount++
		} else {
			log.Printf("Failed to insert lead %s: %v", l.Phone, err)
		}
	}

	return c.JSON(fiber.Map{
		"message": fmt.Sprintf("Successfully uploaded %d leads", successCount),
		"count":   successCount,
	})
}

func HandleDeleteLead(c *fiber.Ctx) error {
	initDB()
	userID, ok := c.Locals("user_id").(string)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Lead ID is required"})
	}

	_, err := dbPool.Exec(context.Background(), "DELETE FROM leads WHERE id = $1 AND user_id = $2", id, userID)
	if err != nil {
		log.Printf("Delete lead failed: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to delete lead"})
	}

	return c.JSON(fiber.Map{"message": "Lead deleted successfully"})
}

func GetLeadByID(ctx context.Context, id string) (*Lead, error) {
	initDB()
	if dbPool == nil {
		return nil, fmt.Errorf("DB not initialized")
	}

	var l Lead
	var campID *string
	err := dbPool.QueryRow(ctx, "SELECT id, user_id, campaign_id, name, phone, email, summary, ai_contacted, created_at FROM leads WHERE id = $1", id).
		Scan(&l.ID, &l.UserID, &campID, &l.Name, &l.Phone, &l.Email, &l.Summary, &l.Contacted, &l.CreatedAt)

	if err != nil {
		return nil, err
	}
	if campID != nil {
		l.CampaignID = *campID
	}
	return &l, nil
}
