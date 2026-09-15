// Package domain holds wagering entities, value objects and ports.
//
// It must stay free of Fx, net/http, the AWS SDK and persistence libraries
// (pgx, database/sql). Dependencies are checked in deps_test.go.
package domain
