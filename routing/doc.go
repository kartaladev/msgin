// Package routing implements the Message Routing patterns of Enterprise
// Integration Patterns chapter 7 — deciding where a message goes next. It is
// the msgin counterpart of Spring Integration's
// org.springframework.integration.router.
//
// [Filter] is the Message Filter: a predicate that drops (or diverts, via
// [WithDiscardChannel]) a message that does not qualify. [Router] is the
// Content-Based Router: it picks a [msgin.MessageChannel] per message, with an
// optional [WithDefaultChannel] for the unmatched case. [Split] is the
// Splitter: one message in, N out, each stamped with the sequence headers.
// [NewAggregator] builds the Aggregator: it correlates messages into a group
// via a [CorrelationStrategy], releases the group when a [ReleaseStrategy]
// (or [WithCompletionSize] / [WithGroupTimeout]) says so, and forwards the
// combined result to [WithOutputChannel].
//
// Filter and Split are [msgin.Step] values, so [msgin.Chain] composes them;
// [Router] and [Aggregator] are [msgin.MessageHandler]s that terminate a chain
// by branching or by holding state.
//
// Deployment topology: the Aggregator keeps NO state of its own — group state
// lives entirely in the injected [msgin.MessageGroupStore], and that store's
// ClaimGroup lease is the sole serializer. The store therefore decides the
// topology: adapter/memory's GroupStore is IN-PROCESS ONLY (with N
// horizontally-scaled instances, members of one correlation group landing on
// different instances never aggregate), while adapter/database/sql's GroupStore
// is durable and multi-process safe. No core change is needed to move between
// them — that is what the SPI seam is for.
package routing
