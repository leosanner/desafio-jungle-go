package app

// Actor is the authenticated caller. Identity comes from the IdP (JWT azp).
// HTTP handlers must not trust providerId from the body or path without
// comparing it to this value.
type Actor struct {
	Subject    string
	ClientID   string
	ProviderID string // empty when Internal
	Internal   bool
}

// CanAccessProvider reports whether this actor may act as providerID.
// The internal service may access any provider; a provider only itself.
func (a Actor) CanAccessProvider(providerID string) bool {
	if providerID == "" {
		return false
	}
	if a.Internal {
		return true
	}
	return a.ProviderID == providerID
}
