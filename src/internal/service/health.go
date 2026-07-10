package service

import (
	"context"

	"lunar-rockets/src/internal/service/repository"
)

type HealthService struct {
	checker repository.HealthChecker
}

type HealthResult struct {
	Status string
	Checks map[string]string
}

func NewHealthService(checker repository.HealthChecker) HealthService {
	return HealthService{checker: checker}
}

func (s HealthService) Check(ctx context.Context) (HealthResult, error) {
	if err := s.checker.Ping(ctx); err != nil {
		return HealthResult{}, err
	}
	return HealthResult{
		Status: "ok",
		Checks: map[string]string{
			"sqlite": "ok",
		},
	}, nil
}
