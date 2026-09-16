package db

import "errors"

// ErrUnknownStorageType is returned when no driver is registered for the configured type.
var ErrUnknownStorageType = errors.New("unknown storage type")
