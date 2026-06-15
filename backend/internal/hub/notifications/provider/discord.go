package provider

import (
	"errors"
	"net/url"
	"strings"
)

type DiscordProvider struct{}

type discordConfig struct {
	Token     string `json:"token"`
	WebhookID string `json:"webhookId"`
	ThreadID  string `json:"threadId"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatarUrl"`
	Title     string `json:"title"`
}

func (DiscordProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigJSON[discordConfig](trimmed, "invalid JSON discord config")
	if err != nil {
		return nil, err
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
