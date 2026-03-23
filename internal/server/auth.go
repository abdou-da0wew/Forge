package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// authMiddleware returns a middleware that validates tokens
func (s *Server) authMiddleware(requireAdmin bool) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Get token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}
			
			// Parse "Bearer <token>" or "Admin <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}
			
			authType := parts[0]
			token := parts[1]
			
			// Validate auth type
			if requireAdmin {
				if authType != "Admin" {
					http.Error(w, `{"error":"admin token required"}`, http.StatusUnauthorized)
					return
				}
				if !validateToken(token, s.cfg.Server.AdminToken, r) {
					http.Error(w, `{"error":"invalid admin token"}`, http.StatusUnauthorized)
					return
				}
			} else {
				if authType != "Bearer" && authType != "Admin" {
					http.Error(w, `{"error":"invalid authorization type"}`, http.StatusUnauthorized)
					return
				}
				// Session auth can use either session token or admin token
				if authType == "Admin" {
					if !validateToken(token, s.cfg.Server.AdminToken, r) {
						http.Error(w, `{"error":"invalid admin token"}`, http.StatusUnauthorized)
						return
					}
				} else {
					if !validateToken(token, s.cfg.Auth.Token, r) {
						http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
						return
					}
				}
			}
			
			next(w, r)
		}
	}
}

// validateToken checks HMAC-SHA256 signature
func validateToken(token, secret string, r *http.Request) bool {
	// Token format: signature:timestamp
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}
	
	signature := parts[0]
	timestampStr := parts[1]
	
	// Parse timestamp
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return false
	}
	
	// Check timestamp age (prevent replay attacks)
	age := time.Now().Unix() - timestamp
	if age < 0 || age > int64(300) { // 5 minutes max age
		return false
	}
	
	// Compute expected signature
	// Signature = HMAC-SHA256(secret, method + path + timestamp)
	message := r.Method + r.URL.Path + timestampStr
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	
	// Constant-time comparison
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// SignRequest creates a signed token for a request
func SignRequest(method, path, secret string) string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	message := method + path + timestamp
	
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))
	
	return fmt.Sprintf("%s:%s", signature, timestamp)
}
