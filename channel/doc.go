// Package channel implements the Messaging Channels patterns of Enterprise
// Integration Patterns chapter 3 — in particular the Point-to-Point Channel
// and, from chapter 4, the Publish-Subscribe Channel. It is the msgin
// counterpart of Spring Integration's org.springframework.integration.channel.
//
// [DirectChannel] is the synchronous Point-to-Point Channel: the subscriber's
// handler runs on the sender's goroutine, so Send returns the handler's error
// and a send with no subscriber is [msgin.ErrNoSubscriber]. [QueueChannel] is
// the buffered Point-to-Point Channel, backed by an injected
// [msgin.ChannelStore] that decides its durability and capacity.
// [PublishSubscribeChannel] fans one message out to every subscriber;
// [WithFanOut] chooses between all-subscribers-succeed and best-effort
// settlement, and Subscribe returns a [msgin.Subscription] whose Cancel
// unsubscribes. [PubSub] is the topic registry over those channels — a channel
// per topic name, created on first Subscribe and dropped when empty — and
// satisfies the root [msgin.TopicPublisher]/[msgin.TopicSubscriber] SPI that
// native-topic broker adapters implement.
//
// Deployment topology: every channel here is IN-PROCESS ONLY. A Go channel and
// an in-memory subscriber map cannot cross a process boundary, so with N
// horizontally-scaled instances a message published on one instance reaches
// only that instance's subscribers. Crossing instances is a broker's job: use
// an adapter (its consumer groups for Competing Consumers, its native topics
// behind TopicPublisher/TopicSubscriber for fan-out). QueueChannel is the one
// exception, and only as far as its ChannelStore reaches — a durable,
// shared store makes it multi-process safe; the in-memory one does not.
package channel
