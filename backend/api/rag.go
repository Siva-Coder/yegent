package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ledongthuc/pdf"
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

	// 2. Extract Text
	var content string
	
	if strings.HasSuffix(strings.ToLower(file.Filename), ".pdf") {
		var err error
		content, err = extractTextFromPDF(file)
		if err != nil {
			log.Printf("PDF extraction failed: %v", err)
			return c.Status(500).JSON(fiber.Map{"error": "Failed to parse PDF text"})
		}
	} else {
		// Fallback for plain text files
		f, _ := file.Open()
		buf := make([]byte, file.Size)
		f.Read(buf)
		content = string(buf)
		f.Close()
	}

	// 3. Chunk & Embed
	chunks := chunkText(content)
	successCount := 0
	var lastErr error
	
	for _, chunk := range chunks {
		embedding, err := GetGeminiEmbedding(chunk)
		if err != nil {
			log.Printf("Embedding failed for chunk: %v", err)
			lastErr = err
			continue
		}
		
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
			if err == nil {
				successCount++
			} else {
				lastErr = err
			}
		}
	}

	if successCount == 0 && len(chunks) > 0 {
		return c.Status(500).JSON(fiber.Map{
			"error": fmt.Sprintf("Failed to embed document chunks: %v", lastErr),
		})
	}

	return c.JSON(fiber.Map{
		"message":        fmt.Sprintf("Document uploaded: %d/%d chunks embedded successfully", successCount, len(chunks)),
		"document_id":    docID,
		"chunks_created": successCount,
	})
}

func extractTextFromPDF(fileHeader *multipart.FileHeader) (string, error) {
	f, err := fileHeader.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Create a temporary file because the PDF parser requires a file path/ReaderAt
	tempFile, err := os.CreateTemp("", "upload-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(tempFile.Name()) // Clean up after we are done
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, f); err != nil {
		return "", err
	}

	_, pdfReader, err := pdf.Open(tempFile.Name())
	if err != nil {
		return "", err
	}

	var textBuilder strings.Builder
	for i := 1; i <= pdfReader.NumPage(); i++ {
		p := pdfReader.Page(i)
		if p.V.IsNull() {
			continue
		}
		s, _ := p.GetPlainText(nil)
		textBuilder.WriteString(s)
		textBuilder.WriteString("\n")
	}
	return textBuilder.String(), nil
}
