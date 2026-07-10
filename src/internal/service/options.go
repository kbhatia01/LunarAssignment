package service

import "log/slog"

type IngestOption func(*IngestMessageService)

func WithLogger(logger *slog.Logger) IngestOption {
	return func(service *IngestMessageService) {
		if logger != nil {
			service.logger = logger
		}
	}
}
