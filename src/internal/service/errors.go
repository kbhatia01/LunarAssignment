package service

import "errors"

var (
	ErrInvalidSort = errors.New("invalid sort")
	ErrNotFound    = errors.New("not found")
)
