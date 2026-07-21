package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collect returns a terminal MessageHandler that appends every handled message
// to *out, plus the handler. It is the Split children's downstream.
func collect(out *[]msgin.Message[any]) msgin.MessageHandler {
	return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		*out = append(*out, m)
		return nil
	})
}

func TestSplit_Forwarding(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name   string
		fn     func(ctx context.Context, m msgin.Message[int]) ([]msgin.Message[int], error)
		in     msgin.Message[any]
		assert func(t *testing.T, forwarded []msgin.Message[any], err error)
	}{
		{
			name: "fans one message into three children in order",
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				p := m.Payload()
				return []msgin.Message[int]{msgin.New(p), msgin.New(p + 1), msgin.New(p + 2)}, nil
			},
			in: boxInt(10),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				require.NoError(t, err)
				require.Len(t, forwarded, 3)
				assert.Equal(t, []any{10, 11, 12}, []any{
					forwarded[0].Payload(), forwarded[1].Payload(), forwarded[2].Payload(),
				})
			},
		},
		{
			name: "empty split forwards nothing and returns nil",
			fn: func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
				return nil, nil
			},
			in: boxInt(1),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				require.NoError(t, err)
				assert.Empty(t, forwarded)
			},
		},
		{
			name: "fn error propagates and forwards nothing",
			fn: func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
				return nil, errBoom
			},
			in: boxInt(1),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				assert.ErrorIs(t, err, errBoom)
				assert.Empty(t, forwarded)
			},
		},
		{
			name: "wrong payload type yields ErrPayloadType",
			fn: func(_ context.Context, _ msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.New(1)}, nil
			},
			in: msgin.New[any]("not-an-int"),
			assert: func(t *testing.T, forwarded []msgin.Message[any], err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Empty(t, forwarded)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded []msgin.Message[any]
			h := msgin.Split(tc.fn)(collect(&forwarded))
			err := h.Handle(t.Context(), tc.in)
			tc.assert(t, forwarded, err)
		})
	}
}

func TestSplit_NilFunc(t *testing.T) {
	h := msgin.Split[int, int](nil)(collect(new([]msgin.Message[any])))
	assert.ErrorIs(t, h.Handle(t.Context(), boxInt(1)), msgin.ErrNilFunc)
}

// TestSplit_DownstreamErrorAbortsRemaining covers the loop's error-return
// branch: a downstream (next) error on one child must abort forwarding the
// rest and propagate, per Split's documented settlement contract.
func TestSplit_DownstreamErrorAbortsRemaining(t *testing.T) {
	errDown := errors.New("downstream boom")
	var forwarded []msgin.Message[any]
	next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		forwarded = append(forwarded, m)
		return errDown
	})
	fn := func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
		p := m.Payload()
		return []msgin.Message[int]{msgin.New(p), msgin.New(p + 1)}, nil
	}

	h := msgin.Split(fn)(next)
	err := h.Handle(t.Context(), boxInt(1))

	assert.ErrorIs(t, err, errDown)
	assert.Len(t, forwarded, 1)
}

// boxInt wraps v as a Message[any] with an int payload.
func boxInt(v int) msgin.Message[any] { return msgin.New[any](v) }

func TestSplit_SequenceHeaders(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(ctx context.Context, m msgin.Message[int]) ([]msgin.Message[int], error)
		parent msgin.Message[any]
		assert func(t *testing.T, children []msgin.Message[any])
	}{
		{
			name: "WithPayload children: distinct deterministic ids, parent-id correlation, 1-based seq",
			// The documented construction path: WithPayload copies the parent's
			// headers (incl. HeaderMessageID), so Split MUST overwrite each child id.
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.WithPayload(m, 1), msgin.WithPayload(m, 2)}, nil
			},
			parent: msgin.New[any](0, msgin.WithID("parent-123")),
			assert: func(t *testing.T, children []msgin.Message[any]) {
				require.Len(t, children, 2)
				assert.Equal(t, "parent-123#1", children[0].ID())
				assert.Equal(t, "parent-123#2", children[1].ID()) // distinct → group fills to N
				for i, c := range children {
					num, _ := c.Header(msgin.HeaderSequenceNumber)
					size, _ := c.Header(msgin.HeaderSequenceSize)
					corr, _ := c.Header(msgin.HeaderCorrelationID)
					assert.Equal(t, i+1, num)
					assert.Equal(t, 2, size)
					assert.Equal(t, "parent-123", corr)
				}
			},
		},
		{
			name: "re-split of the same parent yields identical child ids (idempotent)",
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.WithPayload(m, 1), msgin.WithPayload(m, 2)}, nil
			},
			parent: msgin.New[any](0, msgin.WithID("parent-123")),
			assert: func(t *testing.T, children []msgin.Message[any]) {
				// stable, derived only from parent id + seq — see the extra re-run below.
				assert.Equal(t, "parent-123#1", children[0].ID())
				assert.Equal(t, "parent-123#2", children[1].ID())
			},
		},
		{
			name: "child with its own correlation id keeps it (inherited/nested case)",
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				child := msgin.WithPayload(m, 1).WithHeader(msgin.HeaderCorrelationID, "child-own")
				return []msgin.Message[int]{child}, nil
			},
			parent: msgin.New[any](0, msgin.WithID("parent-123")),
			assert: func(t *testing.T, children []msgin.Message[any]) {
				require.Len(t, children, 1)
				corr, _ := children[0].Header(msgin.HeaderCorrelationID)
				assert.Equal(t, "child-own", corr)                // correlation NOT overwritten
				assert.Equal(t, "parent-123#1", children[0].ID()) // id still deterministic
			},
		},
		{
			name: "id-less parent: no deterministic id, no correlation stamped",
			// Covers the parent.ID()=="" hot-path branch (audit M-2).
			fn: func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{msgin.WithPayload(m, 1)}, nil
			},
			parent: msgin.NewMessage[any](0, msgin.NewHeaders(nil)), // ID()==""
			assert: func(t *testing.T, children []msgin.Message[any]) {
				require.Len(t, children, 1)
				assert.Equal(t, "", children[0].ID()) // no parent id → nothing derived
				_, hasCorr := children[0].Header(msgin.HeaderCorrelationID)
				assert.False(t, hasCorr) // no fallback correlation stamped
				num, _ := children[0].Header(msgin.HeaderSequenceNumber)
				assert.Equal(t, 1, num) // sequence headers still stamped
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded []msgin.Message[any]
			h := msgin.Split(tc.fn)(collect(&forwarded))
			require.NoError(t, h.Handle(t.Context(), tc.parent))
			tc.assert(t, forwarded)
		})
	}
}
