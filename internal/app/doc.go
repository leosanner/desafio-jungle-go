// Package app holds application use cases. Use cases orchestrate domain
// objects and ports; they must not import Fx, net/http, the AWS SDK or
// persistence drivers. Persistence is expressed as Unit of Work and
// repository ports; use cases arrive in a later phase.
package app
