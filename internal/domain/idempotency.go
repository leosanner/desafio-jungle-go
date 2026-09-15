package domain

import "fmt"

// CheckIdempotencyReplay compares a stored key/hash with an incoming pair.
// Same key and hash → replay. Same key, different hash → payload conflict.
func CheckIdempotencyReplay(storedKey, storedHash, incomingKey, incomingHash string) (replay bool, err error) {
	if storedKey == "" || incomingKey == "" {
		return false, validation(FailureMissingIdentity, fmt.Errorf("%w: idempotencyKey", ErrMissingIdentity))
	}
	if storedKey != incomingKey {
		return false, validation(FailureMissingIdentity, fmt.Errorf("%w: idempotency key mismatch for this record", ErrMissingIdentity))
	}
	if storedHash == incomingHash {
		return true, nil
	}
	return false, rejection(FailureIdempotencyPayloadConflict, ErrIdempotencyPayloadConflict)
}

// CheckExternalIdentity ensures (providerId, externalTransactionId) is not reused under another key.
func CheckExternalIdentity(storedKey, incomingKey string) error {
	if storedKey == "" || incomingKey == "" {
		return validation(FailureMissingIdentity, fmt.Errorf("%w: idempotencyKey", ErrMissingIdentity))
	}
	if storedKey != incomingKey {
		return rejection(FailureDuplicateExternalTransaction, ErrDuplicateExternalTransaction)
	}
	return nil
}
