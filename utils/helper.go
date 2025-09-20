package utils

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
)

const tokenFile = "./tokens.json"

func LoadTokens() ([]string, error) {
	data, err := os.ReadFile(tokenFile)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tokens []string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func SaveTokens(tokens []string) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFile, data, 0600)
}
