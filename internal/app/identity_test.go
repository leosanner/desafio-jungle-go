package app

import "testing"

func TestActorCanAccessProvider(t *testing.T) {
	t.Parallel()
	internal := Actor{ClientID: "wagering-internal", Internal: true}
	providerA := Actor{ClientID: "provider-a", ProviderID: "provider-a"}

	if !internal.CanAccessProvider("provider-a") || !internal.CanAccessProvider("provider-b") {
		t.Fatal("internal must access any provider")
	}
	if !providerA.CanAccessProvider("provider-a") {
		t.Fatal("provider-a must access itself")
	}
	if providerA.CanAccessProvider("provider-b") {
		t.Fatal("provider-a must not access provider-b")
	}
	if providerA.CanAccessProvider("") {
		t.Fatal("empty providerId is never allowed")
	}
}
