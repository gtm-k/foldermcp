// Package repository provides a generic data access layer pattern.
//
// This demonstrates Go generics, interface composition, and the
// repository pattern — common patterns the indexer must handle.
package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Entity is the base constraint for storable entities.
type Entity interface {
	GetID() string
}

// Repository defines CRUD operations for an entity type.
type Repository[T Entity] interface {
	Get(ctx context.Context, id string) (T, error)
	List(ctx context.Context, opts ListOptions) ([]T, error)
	Create(ctx context.Context, entity T) error
	Update(ctx context.Context, entity T) error
	Delete(ctx context.Context, id string) error
}

// ListOptions controls pagination and filtering for List operations.
type ListOptions struct {
	Offset  int
	Limit   int
	OrderBy string
	Filter  map[string]any
}

var (
	ErrNotFound      = errors.New("entity not found")
	ErrAlreadyExists = errors.New("entity already exists")
	ErrInvalidEntity = errors.New("invalid entity")
)

// User is a sample entity for the micro-fixture.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetID implements Entity.
func (u User) GetID() string { return u.ID }

// InMemoryRepo is a thread-safe in-memory implementation of Repository.
// Useful for testing and prototyping.
type InMemoryRepo[T Entity] struct {
	mu    sync.RWMutex
	store map[string]T
}

// NewInMemoryRepo creates a new in-memory repository.
func NewInMemoryRepo[T Entity]() *InMemoryRepo[T] {
	return &InMemoryRepo[T]{
		store: make(map[string]T),
	}
}

// Get retrieves an entity by ID.
func (r *InMemoryRepo[T]) Get(_ context.Context, id string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entity, ok := r.store[id]
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	return entity, nil
}

// List returns entities matching the given options.
func (r *InMemoryRepo[T]) List(_ context.Context, opts ListOptions) ([]T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]T, 0, len(r.store))
	for _, entity := range r.store {
		all = append(all, entity)
	}

	// Apply offset and limit
	start := opts.Offset
	if start > len(all) {
		start = len(all)
	}
	end := start + opts.Limit
	if opts.Limit <= 0 || end > len(all) {
		end = len(all)
	}
	return all[start:end], nil
}

// Create adds a new entity.
func (r *InMemoryRepo[T]) Create(_ context.Context, entity T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := entity.GetID()
	if _, exists := r.store[id]; exists {
		return fmt.Errorf("%w: id=%s", ErrAlreadyExists, id)
	}
	r.store[id] = entity
	return nil
}

// Update replaces an existing entity.
func (r *InMemoryRepo[T]) Update(_ context.Context, entity T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := entity.GetID()
	if _, exists := r.store[id]; !exists {
		return fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	r.store[id] = entity
	return nil
}

// Delete removes an entity by ID.
func (r *InMemoryRepo[T]) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.store[id]; !exists {
		return fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	delete(r.store, id)
	return nil
}

// Count returns the number of stored entities.
func (r *InMemoryRepo[T]) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.store)
}
