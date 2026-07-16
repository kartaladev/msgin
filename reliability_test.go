package msgin_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermanent_WrapAndUnwrap(t *testing.T) {
	base := errors.New("boom")

	tests := []struct {
		name   string
		in     error
		assert func(t *testing.T, got error)
	}{
		{"nil stays nil", nil, func(t *testing.T, got error) {
			assert.NoError(t, got)
		}},
		{"wraps and unwraps to base", base, func(t *testing.T, got error) {
			require.Error(t, got)
			assert.ErrorIs(t, got, base, "Permanent must remain Is-comparable to its cause")
		}},
		{"survives an outer wrap", fmt.Errorf("ctx: %w", base), func(t *testing.T, got error) {
			assert.ErrorIs(t, got, base)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, msgin.Permanent(tc.in))
		})
	}
}
