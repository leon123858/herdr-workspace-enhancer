package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Focus has no action writes; a failed host call triggers one independent sync.
func (s *Service) Focus(ctx context.Context, host string, focus func(context.Context, string) error) error {
	original := focus(ctx, host)
	if original == nil {
		return nil
	}
	repairCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.Refresh(repairCtx)
	if err != nil {
		return errors.Join(original, fmt.Errorf("repair sync failed: %w", err))
	}
	return original
}
