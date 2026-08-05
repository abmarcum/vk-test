package main

import (
	"errors"
	"time"
)

// -----------------------------------------------------------------------------
// Sentinel Errors
// -----------------------------------------------------------------------------
// These errors are returned by the storage layer (storage.go) and inspected
// by the HTTP handlers (handlers.go) using errors.Is to map internal failures
// to the correct HTTP status codes without leaking implementation details.
var (
	// ErrNotFound indicates the requested video metadata does not exist.
	ErrNotF
