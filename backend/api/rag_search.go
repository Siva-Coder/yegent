package api

import (
	"context"
	"fmt"
	"strings"
)

// SearchKnowledgeBase takes a user query, embeds it, and searches Supabase for matching chunks.
func SearchKnowledgeBase(ctx context.Context, query string) (string, error) {
	initDB() // Ensure DB pool is ready
	if dbPool == nil {
		return "", fmt.Errorf("database not initialized")
	}

	// 1. Get embedding for the query
	queryEmbedding, err := GetGeminiEmbedding(query)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}

	// 2. Call the RPC match_document_chunks
	// Pgvector string representation: '[0.1, 0.2, ...]'
	embStr := "["
	for i, v := range queryEmbedding {
		embStr += fmt.Sprintf("%f", v)
		if i < len(queryEmbedding)-1 {
			embStr += ","
		}
	}
	embStr += "]"

	// match_threshold=0.5, match_count=3
	rows, err := dbPool.Query(ctx, "SELECT content FROM match_document_chunks($1, $2, $3)", 
		embStr, 0.5, 3)
	if err != nil {
		return "", fmt.Errorf("RPC call failed: %w", err)
	}
	defer rows.Close()

	var chunks []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			continue
		}
		chunks = append(chunks, content)
	}

	if len(chunks) == 0 {
		return "", nil
	}

	return strings.Join(chunks, "\n---\n"), nil
}
