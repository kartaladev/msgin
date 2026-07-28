package endpoint

import (
	"context"

	"github.com/kartaladev/msgin"
)

// boxMessage re-boxes a typed message as Message[any], preserving its headers
// (and therefore its id and timestamp — it is NOT msgin.New, which would stamp
// fresh ones). Inlined per Spec 014 §3.3.
func boxMessage[T any](m msgin.Message[T]) msgin.Message[any] {
	return msgin.NewMessage[any](m.Payload(), m.Headers())
}

// nilFuncStep returns a Step whose handler always fails with ErrNilFunc — the
// shared "constructed with a nil function" degradation. Inlined per Spec 014
// §3.3.
func nilFuncStep() msgin.Step {
	return func(msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
	}
}
