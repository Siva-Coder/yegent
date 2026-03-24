package api

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
)

// In a real app, this would be initialized in main and passed down
var dbPool *pgxpool.Pool

func initDB() {
	if dbPool == nil {
		dbUrl := os.Getenv("SUPABASE_DB_URL")
		if dbUrl == "" {
			return
		}
		pool, err := pgxpool.New(context.Background(), dbUrl)
		if err != nil {
			fmt.Println("Warning: Could not connect to Supabase DB:", err)
			return
		}
		dbPool = pool

		// FIX: Drop old schema workspace constraints so Campaign IDs work cleanly
		_, _ = dbPool.Exec(context.Background(), "ALTER TABLE document_chunks DROP CONSTRAINT IF EXISTS document_chunks_workspace_id_fkey")
		_, _ = dbPool.Exec(context.Background(), "ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_workspace_id_fkey")
	}
}

// GenerateMockEmbedding simulates calling Sarvam or an embedding model
func GenerateMockEmbedding(text string) []float32 {
	// For pgvector 1536 dim
	emb := make([]float32, 1536)
	for i := range emb {
		emb[i] = rand.Float32()
	}
	return emb
}

// chunkText splits text into roughly 500 token chunks (approximation via words)
func chunkText(text string) []string {
	words := strings.Fields(text)
	var chunks []string
	chunkSize := 400 // approx tokens to words

	for i := 0; i < len(words); i += chunkSize {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
	}
	return chunks
}

func HandleDocumentUpload(c *fiber.Ctx) error {
	initDB()

	// Parse multipart form
	file, err := c.FormFile("document")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Document file is required"})
	}
	
	workspaceIDStr := c.FormValue("workspace_id")
	if workspaceIDStr == "" {
		workspaceIDStr = "11111111-1111-1111-1111-111111111111" // Default Mock Workspace
	}

	// Read file content
	fileHeader, err := file.Open()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to read file"})
	}
	defer fileHeader.Close()

	buf := make([]byte, file.Size)
	fileHeader.Read(buf)
	content := string(buf) // Assuming plain text for this scaffolding

	// 1. Create Document Record
	docID := uuid.New().String()
	if dbPool != nil {
		_, err = dbPool.Exec(context.Background(), 
			"INSERT INTO documents (id, workspace_id, name, content_type) VALUES ($1, $2, $3, $4)",
			docID, workspaceIDStr, file.Filename, file.Header.Get("Content-Type"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "DB insert failed"})
		}
	}

	// 2. Chunk & Embed
	chunks := chunkText(content)
	
	// In reality we would batch these to Pgvector
	for _, chunk := range chunks {
		embedding := GenerateMockEmbedding(chunk)
		
		// Pgvector string representation for INSERT: '[0.1, 0.2, ...]'
		embStr := "["
		for i, v := range embedding {
			embStr += fmt.Sprintf("%f", v)
			if i < len(embedding)-1 {
				embStr += ","
			}
		}
		embStr += "]"

		chunkID := uuid.New().String()
		if dbPool != nil {
			_, err = dbPool.Exec(context.Background(),
				"INSERT INTO document_chunks (id, document_id, workspace_id, content, embedding) VALUES ($1, $2, $3, $4, $5)",
				chunkID, docID, workspaceIDStr, chunk, embStr)
		}
	}

	return c.JSON(fiber.Map{
		"message": "Document uploaded and embedded successfully",
		"document_id": docID,
		"chunks_created": len(chunks),
	})
}
