// Package config persists the user's last-used settings between runs.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const appDirName = "email-client-v2"

type Settings struct {
	From            string  `json:"from"`
	Subject         string  `json:"subject"`
	RecipientsPath  string  `json:"recipientsPath"`
	TemplatePath    string  `json:"templatePath"`
	UseCSVTemplate  bool    `json:"useCsvTemplate"`
	CSVEncoding     string  `json:"csvEncoding"`
	CSVDelimiter    string  `json:"csvDelimiter"`
	DelaySeconds    float64 `json:"delaySeconds"`
	CredentialsPath string  `json:"credentialsPath"`
	TokenPath       string  `json:"tokenPath"`
}

func Defaults() Settings {
	creds, err := filepath.Abs("credentials.json")
	if err != nil {
		creds = "credentials.json"
	}
	return Settings{
		UseCSVTemplate:  true,
		CSVEncoding:     "auto",
		CSVDelimiter:    "auto",
		DelaySeconds:    1,
		CredentialsPath: creds,
	}
}

// Path returns the location of the settings file.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appDirName, "settings.json"), nil
}

// Load reads saved settings, falling back to defaults when none exist.
func Load() (Settings, error) {
	s := Defaults()
	p, err := Path()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Defaults(), err
	}
	return s, nil
}

func (s Settings) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// TokenFile is where the OAuth token is stored: the explicit TokenPath, or
// token.json next to the credentials file.
func (s Settings) TokenFile() string {
	if s.TokenPath != "" {
		return s.TokenPath
	}
	return filepath.Join(filepath.Dir(s.CredentialsPath), "token.json")
}
