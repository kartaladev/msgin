package msgin_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
)

func TestSentinels_WrapAndCompare(t *testing.T) {
	sentinels := []error{
		msgin.ErrPayloadType, msgin.ErrPayloadDecode, msgin.ErrNilAdapter,
		msgin.ErrNoPayloadCodec, msgin.ErrUnexpectedCodec,
		msgin.ErrInvalidConcurrency, msgin.ErrUnsupportedSource,
	}
	for _, s := range sentinels {
		t.Run(s.Error(), func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", s)
			assert.True(t, errors.Is(wrapped, s))
		})
	}
}
