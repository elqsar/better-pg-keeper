// Package collectortest holds helpers for testing collectors against real
// SQLite storage.
package collectortest

import (
	"bytes"
	"context"
	"log"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// NewStorage opens storage in a temporary directory and creates one instance.
// It returns the storage and the instance ID.
func NewStorage(t *testing.T) (*sqlite.SQLiteStorage, int64) {
	t.Helper()
	storage, err := sqlite.NewStorage(t.TempDir() + "/collector.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	instance, err := storage.CreateInstance(context.Background(),
		&models.Instance{Name: "test", Host: "localhost", Port: 5432, Database: "app"})
	if err != nil {
		t.Fatal(err)
	}
	return storage, instance
}

// Snapshot creates a snapshot for the instance and returns its ID.
func Snapshot(t *testing.T, storage *sqlite.SQLiteStorage, instanceID int64) int64 {
	t.Helper()
	id, err := storage.CreateSnapshot(context.Background(),
		&models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Logger returns a logger writing to the returned buffer, so tests can check
// what a collector logged.
func Logger() (*log.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return log.New(&buf, "", 0), &buf
}
