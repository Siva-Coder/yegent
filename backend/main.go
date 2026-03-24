package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
	"yegent-backend/api"
	"yegent-backend/ws"
)

func main() {
	log.SetOutput(os.Stderr)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: false,
	})

	app.Use(logger.New())
	app.Use(cors.New())

	// Health Check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("Yegent WebSocket Engine is running")
	})

	// API Routes for RAG Knowledgebase uploads
	apiGroup := app.Group("/api")
	apiGroup.Post("/documents/upload", api.HandleDocumentUpload)

	// Phase 7: Magic Campaign Builder & CRUD
	apiGroup.Post("/campaigns/generate", api.HandleGenerateCampaign)
	apiGroup.Put("/campaigns/:id", api.HandleUpdateCampaign)

	// WebSocket Route for the Voice Agent Pipeline
	app.Get("/ws/call", ws.HandleCallSocket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("Starting engine on port %s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatal("Error starting server:", err)
	}
}
