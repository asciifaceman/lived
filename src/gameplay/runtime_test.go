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

func TestSplitCoreAndDerivedStatsIncludesFinancialAndTradingAptitude(t *testing.T) {
	stats := map[string]int64{
		statStrength:            2,
		statSocial:              3,
		statFinancial:           4,
		statEndurance:           5,
		statStamina:             10,
		statMaxStamina:          100,
		statTradingAptitude:     7,
		statStaminaRecoveryRate: 9,
	}

	core, derived := splitCoreAndDerivedStats(stats)
	if core[statFinancial] != 4 {
		t.Fatalf("expected core financial 4, got %d", core[statFinancial])
	}
	if derived[statTradingAptitude] != 7 {
		t.Fatalf("expected derived trading aptitude 7, got %d", derived[statTradingAptitude])
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

func TestParseBehaviorRuntimePayloadNormalizesRealizedMaps(t *testing.T) {
	payload := parseBehaviorRuntimePayload(`{"spent":{"coins":12,"":5},"gained":{"coins":3,"stamina":0}}`)
	if len(payload.Spent) != 1 || payload.Spent["coins"] != 12 {
		t.Fatalf("expected normalized spent map, got %#v", payload.Spent)
	}
	if len(payload.Gained) != 1 || payload.Gained["coins"] != 3 {
		t.Fatalf("expected normalized gained map, got %#v", payload.Gained)
	}
}

func TestMarshalBehaviorRuntimePayloadEmptyMapPayloadToBraces(t *testing.T) {
	encoded, err := marshalBehaviorRuntimePayload(behaviorRuntimePayload{Spent: map[string]int64{"coins": 0}, Gained: map[string]int64{"": 5}})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if encoded != "{}" {
		t.Fatalf("expected empty payload encoding {}, got %q", encoded)
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

func TestBehaviorModeSupportedDefaultsAndOverrides(t *testing.T) {
	defaultDefinition := BehaviorDefinition{}
	if !behaviorModeSupported(defaultDefinition, "once") {
		t.Fatal("expected default definition to support once")
	}
	if !behaviorModeSupported(defaultDefinition, "repeat") {
		t.Fatal("expected default definition to support repeat")
	}
	if !behaviorModeSupported(defaultDefinition, "repeat-until") {
		t.Fatal("expected default definition to support repeat-until")
	}

	onceOnlyDefinition := BehaviorDefinition{ScheduleModes: []string{"once"}}
	if !behaviorModeSupported(onceOnlyDefinition, "once") {
		t.Fatal("expected once-only definition to support once")
	}
	if behaviorModeSupported(onceOnlyDefinition, "repeat") {
		t.Fatal("expected once-only definition to reject repeat")
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

func TestShouldAutoCompleteQueuedBehavior(t *testing.T) {
	repeatPayload := behaviorRuntimePayload{Mode: behaviorModeRepeat}
	repeatUntilPayload := behaviorRuntimePayload{Mode: behaviorModeRepeatUntil}
	oncePayload := behaviorRuntimePayload{Mode: behaviorModeOnce}

	if !shouldAutoCompleteQueuedBehavior(repeatPayload, errString("missing scrap (need 1)")) {
		t.Fatal("expected repeat mode missing requirement to auto-complete")
	}
	if !shouldAutoCompleteQueuedBehavior(repeatUntilPayload, errString("insufficient scrap")) {
		t.Fatal("expected repeat-until insufficient resource to auto-complete")
	}
	if shouldAutoCompleteQueuedBehavior(oncePayload, errString("missing scrap (need 1)")) {
		t.Fatal("expected once mode requirement miss to remain a failure")
	}
	if shouldAutoCompleteQueuedBehavior(repeatPayload, errString("unknown behavior definition")) {
		t.Fatal("expected unknown errors to remain failures")
	}
}

func TestShouldQueueNextBehaviorStopsRestWhenFull(t *testing.T) {
	payload := behaviorRuntimePayload{Mode: behaviorModeRepeat}
	if shouldQueueNextBehavior(payload, 100, 0, restBehaviorKey, 0) {
		t.Fatal("expected rest repeat to stop when no stamina was recovered")
	}
	if !shouldQueueNextBehavior(payload, 100, 0, restBehaviorKey, 5) {
		t.Fatal("expected rest repeat to continue when stamina was recovered")
	}
	if !shouldQueueNextBehavior(payload, 100, 0, "player_scavenge_scrap", 0) {
		t.Fatal("expected non-rest repeat to continue")
	}
}

func TestIsNightTickWindow(t *testing.T) {
	if !IsNightTick(0) {
		t.Fatal("expected midnight to be night")
	}
	if !IsNightTick(5 * 60) {
		t.Fatal("expected 05:00 to be night")
	}
	if IsNightTick(12 * 60) {
		t.Fatal("expected noon to be daytime")
	}
	if !IsNightTick(22 * 60) {
		t.Fatal("expected 22:00 to be night")
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}
