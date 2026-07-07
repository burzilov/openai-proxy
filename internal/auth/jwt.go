package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type jwtClaims struct {
	Exp  float64 `json:"exp"`
	Auth struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
		ChatGPTPlanType  string `json:"chatgpt_plan_type"`
		Email            string `json:"email"`
	} `json:"https://api.openai.com/auth"`
	Email string `json:"email"`
}

func decodeJWTClaims(token string) (jwtClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return jwtClaims{}, false
	}
	payload := parts[1]
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return jwtClaims{}, false
	}
	var claims jwtClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return jwtClaims{}, false
	}
	return claims, true
}

func ExtractAccountID(accessToken string) string {
	claims, ok := decodeJWTClaims(accessToken)
	if !ok {
		return ""
	}
	return strings.TrimSpace(claims.Auth.ChatGPTAccountID)
}

func ExtractLabel(accessToken string) string {
	claims, ok := decodeJWTClaims(accessToken)
	if !ok {
		return ""
	}
	if email := strings.TrimSpace(claims.Auth.Email); email != "" {
		return email
	}
	return strings.TrimSpace(claims.Email)
}

func ExtractPlanType(accessToken string) string {
	claims, ok := decodeJWTClaims(accessToken)
	if !ok {
		return ""
	}
	return strings.TrimSpace(claims.Auth.ChatGPTPlanType)
}

func AccessTokenExpiring(accessToken string, skew time.Duration) bool {
	claims, ok := decodeJWTClaims(accessToken)
	if !ok || claims.Exp == 0 {
		return true
	}
	deadline := time.Now().Add(skew).Unix()
	return int64(claims.Exp) <= deadline
}

func AccessTokenExpiresAt(accessToken string) (time.Time, bool) {
	claims, ok := decodeJWTClaims(accessToken)
	if !ok || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(claims.Exp), 0).UTC(), true
}
