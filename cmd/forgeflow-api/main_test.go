package main

import (
	"testing"

	"forgeflow/internal/config"
)

func TestValidateAPISecurityConfig(t *testing.T) {
	validKey := []byte("0123456789abcdef0123456789abcdef")
	tests := []struct {
		name    string
		config  config.Config
		wantErr bool
	}{
		{name: "development without enforcement", config: config.Config{Environment: "development"}},
		{name: "production without enforcement", config: config.Config{Environment: "production"}, wantErr: true},
		{name: "required without key", config: config.Config{Environment: "staging", AdminMFARequired: true}, wantErr: true},
		{name: "production with API-only key", config: config.Config{Environment: "production", AdminMFARequired: true, MFAEncryptionKey: validKey}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAPISecurityConfig(test.config)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateAPISecurityConfig() error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}
