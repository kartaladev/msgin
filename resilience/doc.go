// Package resilience holds the concrete implementations of the root package's
// cross-cutting governor and strategy interfaces: [msgin.BackoffStrategy],
// [msgin.CircuitBreaker], and [msgin.RateLimiter].
//
// Unlike endpoint, routing, transform, and channel, this package has NO
// Enterprise Integration Patterns chapter and NO Spring Integration
// counterpart — resilience is not an EIP concern, and Spring Integration
// delegates it to Spring Retry and Resilience4j rather than owning it. The
// package is named for what it does, not for a chapter it maps to. Its
// rationale is ADR 0006 (resilience and flow control), not the EIP book.
//
// [ExponentialBackoff] is the default retry schedule used by a
// [msgin.RetryPolicy]: capped exponential growth, with optional jitter via
// RandomizationFactor. [NewCircuitBreaker] is a
// dependency-free breaker that opens after a threshold of consecutive failures
// and half-opens after a cooldown. [NewTokenBucket] is a dependency-free rate
// limiter. All three are clockwork-driven, so a test drives them with a fake
// clock instead of sleeping.
//
// Each is an opinionated default, not a mandate: the root interfaces are the
// contract, so a caller may substitute x/time/rate, sony/gobreaker, or any
// other implementation without either the core or this package changing.
package resilience
