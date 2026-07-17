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
		msgin.ErrPayloadTooLarge, msgin.ErrUnexpectedCodec,
		msgin.ErrInvalidConcurrency, msgin.ErrUnsupportedSource,
		msgin.ErrInvalidAttemptTTL,
	}
	for _, s := range sentinels {
		t.Run(s.Error(), func(t *testing.T) {
			wrapped := fmt.Errorf("context: %w", s)
			assert.True(t, errors.Is(wrapped, s))
		})
	}
}

func TestSentinels_Reliability_WrapAndCompare(t *testing.T) {
	sentinels := []error{
		msgin.ErrHandlerPanic, msgin.ErrNoDeadLetter, msgin.ErrInvalidMaxAttempts,
	}
	for _, s := range sentinels {
		t.Run(s.Error(), func(t *testing.T) {
			assert.True(t, errors.Is(fmt.Errorf("context: %w", s), s))
		})
	}
}
