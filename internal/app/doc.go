// Package app holds application use cases and ports. Use cases orchestrate domain
// objects and ports; they must not import Fx, net/http, the AWS SDK or
// persistence drivers. Persistence is expressed as Unit of Work and
// repository ports. OutboxRelay publishes committed rows via OutboxClaimer and
// EventBus. HandleInbound writes inbox + domain in one unit of work. Actor is
// the authenticated caller derived from the IdP (HTTP) or queue-gated providerId (SQS).
package app
