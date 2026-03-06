package gameplay

import (
	"testing"

	"github.com/asciifaceman/lived/pkg/dal"
)

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

func TestDeriveStaminaByEndurance(t *testing.T) {
	max0, recovery0 := deriveStaminaByEndurance(0)
	if max0 != defaultMaxStamina {
		t.Fatalf("expected default max stamina %d, got %d", defaultMaxStamina, max0)
	}
	if recovery0 != defaultStaminaRecoveryRate {
		t.Fatalf("expected default recovery %d, got %d", defaultStaminaRecoveryRate, recovery0)
	}

	max12, recovery12 := deriveStaminaByEndurance(12)
	if max12 <= max0 {
		t.Fatalf("expected max stamina to grow with endurance, got base=%d next=%d", max0, max12)
	}
	if recovery12 <= recovery0 {
		t.Fatalf("expected recovery rate to grow with endurance, got base=%d next=%d", recovery0, recovery12)
	}
}

func TestInferEnduranceFromLegacyStats(t *testing.T) {
	legacy := map[string]int64{
		statMaxStamina:          defaultMaxStamina + 30,
		statStaminaRecoveryRate: defaultStaminaRecoveryRate + 2,
	}
	endurance := inferEnduranceFromLegacyStats(legacy)
	if endurance <= 0 {
		t.Fatalf("expected inferred endurance from legacy stamina fields, got %d", endurance)
	}

	none := inferEnduranceFromLegacyStats(map[string]int64{})
	if none != 0 {
		t.Fatalf("expected zero inferred endurance when no legacy fields exist, got %d", none)
	}
}

func TestHumanizeIdentifier(t *testing.T) {
	if got := HumanizeIdentifier("player_scavenge_scrap"); got != "Scavenge Scrap" {
		t.Fatalf("expected humanized behavior key, got %q", got)
	}
	if got := HumanizeIdentifier("forest_access"); got != "Forest Access" {
		t.Fatalf("expected humanized unlock key, got %q", got)
	}
}

func TestSortBehaviorDefinitionsByDisplayName(t *testing.T) {
	definitions := []BehaviorDefinition{
		{Key: "player_zeta", Name: "Zeta"},
		{Key: "player_alpha"},
		{Key: "player_beta", Name: "Beta"},
	}

	sorted := SortBehaviorDefinitions(definitions)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(sorted))
	}

	if sorted[0].Key != "player_alpha" || sorted[1].Key != "player_beta" || sorted[2].Key != "player_zeta" {
		t.Fatalf("unexpected sort order: %q, %q, %q", sorted[0].Key, sorted[1].Key, sorted[2].Key)
	}
}

func TestParseBehaviorRuntimePayloadModeNormalization(t *testing.T) {
	payload := parseBehaviorRuntimePayload(`{"mode":"REPEAT-UNTIL","repeatIntervalMinutes":30,"repeatUntilTick":900}`)
	if payload.Mode != behaviorModeRepeatUntil {
		t.Fatalf("expected mode %q, got %q", behaviorModeRepeatUntil, payload.Mode)
	}
	if payload.RepeatIntervalMinutes != 30 {
		t.Fatalf("expected repeat interval 30, got %d", payload.RepeatIntervalMinutes)
	}
	if payload.RepeatUntilTick != 900 {
		t.Fatalf("expected repeat-until tick 900, got %d", payload.RepeatUntilTick)
	}
}

func TestParseBehaviorRuntimePayloadInvalidModeFallsBackToOnce(t *testing.T) {
	payload := parseBehaviorRuntimePayload(`{"mode":"forever"}`)
	if payload.Mode != behaviorModeOnce {
		t.Fatalf("expected fallback mode %q, got %q", behaviorModeOnce, payload.Mode)
	}
}

func TestComputeRestRecoveryPoints(t *testing.T) {
	if got := computeRestRecoveryPoints(8, 30); got <= 8 {
		t.Fatalf("expected accelerated rest recovery greater than passive baseline, got %d", got)
	}

	if got := computeRestRecoveryPoints(0, 30); got != restMinimumRecoveryPoints {
		t.Fatalf("expected minimum recovery %d, got %d", restMinimumRecoveryPoints, got)
	}

	if got := computeRestRecoveryPoints(12, 0); got < restMinimumRecoveryPoints {
		t.Fatalf("expected minimum floor for zero duration input, got %d", got)
	}
}

func TestBehaviorExclusiveGroupNormalization(t *testing.T) {
	definition := BehaviorDefinition{ExclusiveGroup: "  BODY_STATE  "}
	if got := behaviorExclusiveGroup(definition); got != "body_state" {
		t.Fatalf("expected normalized group body_state, got %q", got)
	}
}

func TestBehaviorDefinitionsConflict(t *testing.T) {
	rest := playerBehaviorDefinitions["player_rest"]
	pushups := playerBehaviorDefinitions["player_pushups"]
	scavenge := playerBehaviorDefinitions["player_scavenge_scrap"]

	if !behaviorDefinitionsConflict(rest, pushups) {
		t.Fatal("expected rest and pushups to conflict by exclusive group")
	}

	if behaviorDefinitionsConflict(rest, scavenge) {
		t.Fatal("expected rest and scavenge to not conflict")
	}

	if behaviorDefinitionsConflict(scavenge, pushups) {
		t.Fatal("expected non-grouped behavior to not conflict")
	}
}

func TestHasExclusiveConflictWithActiveBehaviorKeys(t *testing.T) {
	rest := playerBehaviorDefinitions["player_rest"]

	active := []dal.BehaviorInstance{
		{BaseModel: dal.BaseModel{ID: 10}, Key: "player_pushups"},
		{BaseModel: dal.BaseModel{ID: 11}, Key: "player_scavenge_scrap"},
	}

	if !hasExclusiveConflictWithActiveBehaviorKeys(rest, 99, active) {
		t.Fatal("expected conflict when active set includes behavior in same exclusivity group")
	}

	if hasExclusiveConflictWithActiveBehaviorKeys(rest, 10, active) {
		t.Fatal("expected no conflict when candidate id is excluded from active set")
	}

	nonExclusive := playerBehaviorDefinitions["player_scavenge_scrap"]
	if hasExclusiveConflictWithActiveBehaviorKeys(nonExclusive, 99, active) {
		t.Fatal("expected non-exclusive definition to not conflict")
	}
}
