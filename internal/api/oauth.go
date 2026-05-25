package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var oauthTokenURL = "https://platform.claude.com/v1/oauth/token"

const oauthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

type refreshedTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

func refreshOAuth(ctx context.Context, refreshToken string) (refreshedTokens, error) {
	reqBody := struct {
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
		Scope        string `json:"scope"`
	}{
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
		ClientID:     oauthClientID,
		Scope:        "user:inference user:profile org:create_api_key",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return refreshedTokens{}, fmt.Errorf("marshal oauth refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, bytes.NewReader(body))
	if err != nil {
		return refreshedTokens{}, fmt.Errorf("create oauth refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return refreshedTokens{}, fmt.Errorf("refresh oauth token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := readAll(resp.Body, 1024)
		snippet = strings.TrimSpace(snippet)
		if snippet != "" {
			return refreshedTokens{}, fmt.Errorf("refresh oauth token: %s: %s", resp.Status, snippet)
		}
		return refreshedTokens{}, fmt.Errorf("refresh oauth token: %s", resp.Status)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return refreshedTokens{}, fmt.Errorf("decode oauth refresh response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return refreshedTokens{}, fmt.Errorf("oauth refresh response missing access_token")
	}
	if tokenResp.RefreshToken == "" {
		tokenResp.RefreshToken = refreshToken
	}

	return refreshedTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + tokenResp.ExpiresIn*1000,
	}, nil
}
