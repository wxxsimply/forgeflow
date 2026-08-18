package postgres

import (
	"context"
	"testing"
)

func TestOpenRejectsMissingDSN(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); err == nil {
		t.Fatal("Open accepted an empty DSN")
	}
}
