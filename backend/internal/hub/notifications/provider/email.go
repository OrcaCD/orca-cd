package provider

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

type EmailProvider struct{}

type emailConfig struct {
	SMTPHost    string   `json:"smtpHost"`
	SMTPPort    int      `json:"smtpPort"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	FromAddress string   `json:"fromAddress"`
	FromName    string   `json:"fromName"`
	ToAddresses []string `json:"toAddresses"`
	UseTLS      bool     `json:"useTls"`
}

func (EmailProvider) BuildShoutrrrUrls(rawConfig string) ([]string, error) {
	trimmed, err := normalizeRawConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfigJSON[emailConfig](trimmed, "invalid JSON email config")
	if err != nil {
		return nil, err
	}

	smtpHost := strings.TrimSpace(cfg.SMTPHost)
	if smtpHost == "" {
		return nil, errors.New("email config requires smtpHost")
	}
	if strings.ContainsAny(smtpHost, "/?#") {
		return nil, errors.New("email smtpHost must be a hostname or IP address")
	}

	if cfg.SMTPPort < 1 || cfg.SMTPPort > 65535 {
		return nil, errors.New("email smtpPort must be between 1 and 65535")
	}

	fromAddress, err := normalizeEmailAddress(cfg.FromAddress, "fromAddress")
	if err != nil {
		return nil, err
	}

	toAddresses, err := normalizeEmailAddresses(cfg.ToAddresses)
	if err != nil {
		return nil, err
	}

	smtpURL := url.URL{
		Scheme: "smtp",
		Host:   net.JoinHostPort(smtpHost, strconv.Itoa(cfg.SMTPPort)),
		Path:   "/",
	}

	username := strings.TrimSpace(cfg.Username)
	password := cfg.Password
	switch {
	case username != "" && password != "":
		smtpURL.User = url.UserPassword(username, password)
	case username != "":
		smtpURL.User = url.User(username)
	case password != "":
		return nil, errors.New("email password requires username")
	}

	query := url.Values{}
	query.Set("fromaddress", fromAddress)
	query.Set("toaddresses", strings.Join(toAddresses, ","))
	if fromName := strings.TrimSpace(cfg.FromName); fromName != "" {
		query.Set("fromname", fromName)
	}
	if !cfg.UseTLS {
		query.Set("usestarttls", "No")
		query.Set("encryption", "None")
	}
	smtpURL.RawQuery = query.Encode()

	return []string{smtpURL.String()}, nil
}

func normalizeEmailAddresses(rawAddresses []string) ([]string, error) {
	if len(rawAddresses) == 0 {
		return nil, errors.New("email config requires toAddresses")
	}

	addresses := make([]string, 0, len(rawAddresses))
	for i, rawAddress := range rawAddresses {
		address, err := normalizeEmailAddress(rawAddress, fmt.Sprintf("toAddresses[%d]", i))
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, address)
	}

	return addresses, nil
}

func normalizeEmailAddress(rawAddress, fieldName string) (string, error) {
	address := strings.TrimSpace(rawAddress)
	if address == "" {
		return "", fmt.Errorf("email config requires %s", fieldName)
	}

	parsedAddress, err := mail.ParseAddress(address)
	if err != nil || parsedAddress.Address != address {
		return "", fmt.Errorf("email %s must be a valid email address", fieldName)
	}

	return address, nil
}
