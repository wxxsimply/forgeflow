package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgeflow/internal/buildinfo"
)

func TestHealthHandlerExposesBuildIdentity(t *testing.T) {
	info := buildinfo.New("0.12.0-rc.1", "abcdef0123456789abcdef0123456789abcdef01")
	response := httptest.NewRecorder()
	healthHandler(info).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var payload struct {
		Status         string `json:"status"`
		ServiceVersion string `json:"serviceVersion"`
		GitCommit      string `json:"gitCommit"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "ok" || payload.ServiceVersion != info.ServiceVersion || payload.GitCommit != info.GitCommit {
		t.Fatalf("payload=%+v", payload)
	}
}
