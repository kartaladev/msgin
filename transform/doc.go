// Package transform implements the Message Transformation patterns of
// Enterprise Integration Patterns chapter 8 — changing a message's content
// without changing where it goes. It is the msgin counterpart of Spring
// Integration's org.springframework.integration.transformer.
//
// [Transform] is the Message Translator: it maps a Message[A] to a Message[B]
// and forwards the result to the next handler. It is a [msgin.Step], so
// [msgin.Chain] composes it with the routing and endpoint steps.
//
// Header enrichment needs no endpoint of its own — [msgin.Message.WithHeader]
// returns a new message, so a Content Enricher is written as a Transform whose
// function only adds headers. Messages are immutable: Transform never mutates
// its input, which is what makes a message safe to share across a
// publish-subscribe channel's subscribers.
package transform
