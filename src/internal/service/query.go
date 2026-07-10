package service

import (
	"context"
	"fmt"

	"lunar-rockets/src/internal/domain"
	"lunar-rockets/src/internal/service/repository"
)

const (
	DefaultSort  = repository.SortChannel
	DefaultOrder = repository.OrderAsc
)

type RocketQueryService struct {
	states repository.RocketStateReader
}

type ListRocketsQuery struct {
	Sort  string
	Order string
}

type ListRocketsResult struct {
	Rockets []domain.RocketState
	Sort    string
	Order   string
}

func NewRocketQueryService(states repository.RocketStateReader) RocketQueryService {
	return RocketQueryService{states: states}
}

func (s RocketQueryService) List(ctx context.Context, query ListRocketsQuery) (ListRocketsResult, error) {
	options, err := normalizeListOptions(query)
	if err != nil {
		return ListRocketsResult{}, err
	}
	states, err := s.states.List(ctx, options)
	if err != nil {
		return ListRocketsResult{}, err
	}
	return ListRocketsResult{
		Rockets: states,
		Sort:    options.Sort,
		Order:   options.Order,
	}, nil
}

func (s RocketQueryService) Get(ctx context.Context, channel string) (domain.RocketState, error) {
	state, ok, err := s.states.Get(ctx, channel)
	if err != nil {
		return domain.RocketState{}, err
	}
	if !ok {
		return domain.RocketState{}, ErrNotFound
	}
	return state, nil
}

func normalizeListOptions(query ListRocketsQuery) (repository.ListRocketsOptions, error) {
	sort := query.Sort
	if sort == "" {
		sort = DefaultSort
	}
	order := query.Order
	if order == "" {
		order = DefaultOrder
	}

	switch sort {
	case repository.SortChannel,
		repository.SortMission,
		repository.SortSpeed,
		repository.SortStatus,
		repository.SortLastMessageNumber:
	default:
		return repository.ListRocketsOptions{}, fmt.Errorf("%w: unsupported sort field %q", ErrInvalidSort, sort)
	}
	switch order {
	case repository.OrderAsc, repository.OrderDesc:
	default:
		return repository.ListRocketsOptions{}, fmt.Errorf("%w: unsupported order %q", ErrInvalidSort, order)
	}

	return repository.ListRocketsOptions{Sort: sort, Order: order}, nil
}
