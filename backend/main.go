package main

import (
	"log"
	"os"

	"yegent-backend/api"
	"yegent-backend/middleware"
	"yegent-backend/ws"

	// "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/websocket/v2"
	"github.com/joho/godotenv"
)

func main() {
	log.SetOutput(os.Stderr)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: false,
		ReadBufferSize:        1024 * 1024,
	})

	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
		AllowMethods: "*",
	}))

	// Health Check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("Yegent WebSocket Engine is running")
	})

	// API Routes for RAG Knowledgebase (Protected)
	apiGroup := app.Group("/api", middleware.AuthMiddleware)
	apiGroup.Post("/documents/upload", api.HandleDocumentUpload)
	apiGroup.Get("/documents", api.HandleListDocuments)
	apiGroup.Delete("/documents/:id", api.HandleDeleteDocument)

	// API Routes for Lead CRM (Protected)
	apiGroup.Get("/leads", api.HandleListLeads)
	apiGroup.Post("/leads/bulk", api.HandleBulkUploadLeads)
	apiGroup.Delete("/leads/:id", api.HandleDeleteLead)

	// Phase 7: Magic Campaign Builder & CRUD (Protected)
	apiGroup.Post("/campaigns/generate", api.HandleGenerateCampaign)
	apiGroup.Put("/campaigns/:id", api.HandleUpdateCampaign)

	// // ✅ REQUIRED for websocket
	// app.Use("/ws", func(c *fiber.Ctx) error {
	// 	if websocket.IsWebSocketUpgrade(c) {
	// 		return c.Next()
	// 	}
	// 	return fiber.ErrUpgradeRequired
	// })

	// WebSocket Route for the Voice Agent Pipeline (Protected inside handler)
	app.Get("/ws/call", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}, ws.HandleCallSocket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("Starting engine on port %s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatal("Error starting server:", err)
	}
}
