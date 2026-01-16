package client

import (
	"errors"
	"fmt"

	"github.com/meigma/blob/client/oras"
)

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
	return err
}
