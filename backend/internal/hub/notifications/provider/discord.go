package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type DiscordProvider struct{}

type discordConfig struct {
	Token     string   `json:"token"`
	WebhookID string   `json:"webhookId"`
	ThreadID  string   `json:"threadId"`
	Username  string   `json:"username"`
	AvatarURL string   `json:"avatarUrl"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	URLs      []string `json:"urls"`
}

func (DiscordProvider) BuildShouterrrUrls(rawConfig string) ([]string, error) {
	trimmed := strings.TrimSpace(rawConfig)
	if trimmed == "" {
		return nil, errors.New("notification config is empty")
	}

	if strings.HasPrefix(trimmed, "{") {
		var cfg discordConfig
		if err := json.Unmarshal([]byte(trimmed), &cfg); err != nil {
			return nil, fmt.Errorf("invalid JSON discord config: %w", err)
		}

		if cfg.URL != "" || len(cfg.URLs) > 0 {
			return normalizeTargets(append([]string{cfg.URL}, cfg.URLs...))
		}

		token := strings.TrimSpace(cfg.Token)
		webhookID := strings.TrimSpace(cfg.WebhookID)
		if token == "" || webhookID == "" {
			return nil, errors.New("discord config requires token and webhookId")
		}

		query := url.Values{}
		if v := strings.TrimSpace(cfg.ThreadID); v != "" {
			query.Set("thread_id", v)
		}
		if v := strings.TrimSpace(cfg.Username); v != "" {
			query.Set("username", v)
		}
		if v := strings.TrimSpace(cfg.AvatarURL); v != "" {
			query.Set("avatarurl", v)
		}
		if v := strings.TrimSpace(cfg.Title); v != "" {
			query.Set("title", v)
		}

		discordURL := url.URL{
			Scheme:   "discord",
			User:     url.User(token),
			Host:     webhookID,
			RawQuery: query.Encode(),
		}

		return []string{discordURL.String()}, nil
	}

	return parseDirectTargets(trimmed)
}
