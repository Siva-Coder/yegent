package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

var (
	jwksCache     []map[string]interface{}
	jwksCacheLock sync.RWMutex
	lastFetch     time.Time
)

func AuthMiddleware(c *fiber.Ctx) error {
	// Skip auth for health check
	if c.Path() == "/health" {
		return c.Next()
	}

	authHeader := c.Get("Authorization")
	tokenString := ""

	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		tokenString = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		// Fallback to query param for websockets or simple links
		tokenString = c.Query("token")
	}

	userID, err := VerifyToken(tokenString)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": fmt.Sprintf("Authentication failed: %v", err),
		})
	}

	// Store the user ID in the Fiber locals context for use in handlers
	c.Locals("user_id", userID)
	return c.Next()
}

// VerifyToken validates a Supabase JWT (HS256/ES256/RS256) and returns the user ID (sub)
func VerifyToken(tokenString string) (string, error) {
	if tokenString == "" {
		return "", fmt.Errorf("missing or empty token")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// 1. Handle Symmetric Signing (HS256) - Legacy or Local
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); ok {
			secret := os.Getenv("SUPABASE_JWT_SECRET")
			if secret != "" {
				return []byte(secret), nil
			}
		}

		// 2. Handle Asymmetric Signing (ES256, RS256) - Modern Supabase
		if alg, ok := token.Header["alg"].(string); ok && (strings.HasPrefix(alg, "ES") || strings.HasPrefix(alg, "RS")) {
			kid, _ := token.Header["kid"].(string)
			return getSupabasePublicKey(kid)
		}

		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	})

	if err != nil {
		return "", err
	}

	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("invalid token claims")
	}

	// Supabase stores the user ID in the 'sub' claim
	userID, ok := claims["sub"].(string)
	if !ok {
		return "", fmt.Errorf("user ID (sub) not found in token")
	}

	return userID, nil
}

func getSupabasePublicKey(kid string) (interface{}, error) {
	jwksCacheLock.RLock()
	// Refresh cache if older than 1 hour or empty
	if time.Since(lastFetch) > time.Hour || len(jwksCache) == 0 {
		jwksCacheLock.RUnlock()
		if err := refreshJWKS(); err != nil {
			return nil, err
		}
		jwksCacheLock.RLock()
	}
	defer jwksCacheLock.RUnlock()

	for _, key := range jwksCache {
		if k, ok := key["kid"].(string); ok && (kid == "" || k == kid) {
			// Currently only supporting ES256 (Supabase modern default)
			if kty, ok := key["kty"].(string); ok && kty == "EC" {
				x64, _ := key["x"].(string)
				y64, _ := key["y"].(string)
				crv, _ := key["crv"].(string)

				if crv != "P-256" {
					return nil, fmt.Errorf("unsupported curve: %s", crv)
				}

				x, _ := base64.RawURLEncoding.DecodeString(x64)
				y, _ := base64.RawURLEncoding.DecodeString(y64)

				return &ecdsa.PublicKey{
					Curve: elliptic.P256(),
					X:     new(big.Int).SetBytes(x),
					Y:     new(big.Int).SetBytes(y),
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("public key not found in JWKS for kid: %s", kid)
}

func refreshJWKS() error {
	jwksCacheLock.Lock()
	defer jwksCacheLock.Unlock()

	// Re-check after acquiring lock
	if time.Since(lastFetch) < time.Minute && len(jwksCache) > 0 {
		return nil
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		return fmt.Errorf("SUPABASE_URL missing in env")
	}

	// Supabase JWKS endpoint
	url := fmt.Sprintf("%s/auth/v1/.well-known/jwks.json", strings.TrimSuffix(supabaseURL, "/"))
	
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Keys []map[string]interface{} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode JWKS: %v", err)
	}

	jwksCache = result.Keys
	lastFetch = time.Now()
	return nil
}
