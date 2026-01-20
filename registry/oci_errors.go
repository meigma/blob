package registry

import (
	"errors"
	"fmt"

	"github.com/meigma/blob/registry/oras"
)

// mapOCIError translates low-level ORAS errors to client-level sentinel errors.
func mapOCIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if errors.Is(err, oras.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if errors.Is(err, oras.ErrReferrersUnsupported) {
		return fmt.Errorf("%w: %v", ErrReferrersUnsupported, err)
	}
	return err
}
