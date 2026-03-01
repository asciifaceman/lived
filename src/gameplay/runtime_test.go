package gameplay

import "testing"

func TestAscensionRequiredCoinsMonotonic(t *testing.T) {
	base := ascensionRequiredCoins(0)
	if base != ascensionBaseMinCoins {
		t.Fatalf("expected base requirement %d, got %d", ascensionBaseMinCoins, base)
	}

	prev := base
	for count := int64(1); count <= 8; count++ {
		next := ascensionRequiredCoins(count)
		if next < prev {
			t.Fatalf("expected non-decreasing requirement, count=%d prev=%d next=%d", count, prev, next)
		}
		prev = next
	}
}

func TestAscensionEligibilityForState(t *testing.T) {
	required := ascensionRequiredCoins(0)

	eligible := ascensionEligibilityForState(required, 0)
	if !eligible.Available {
		t.Fatalf("expected ascension to be available at requirement threshold")
	}
	if eligible.RequirementCoins != required {
		t.Fatalf("expected requirement coins %d, got %d", required, eligible.RequirementCoins)
	}

	notEligible := ascensionEligibilityForState(required-1, 0)
	if notEligible.Available {
		t.Fatalf("expected ascension to be unavailable below requirement")
	}
	if notEligible.CurrentCoins != required-1 {
		t.Fatalf("expected current coins %d, got %d", required-1, notEligible.CurrentCoins)
	}
}

func TestNormalizeRealmID(t *testing.T) {
	if got := normalizeRealmID(0); got != 1 {
		t.Fatalf("expected realm 0 to normalize to 1, got %d", got)
	}
	if got := normalizeRealmID(7); got != 7 {
		t.Fatalf("expected realm 7 to remain 7, got %d", got)
	}
}
