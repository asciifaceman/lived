package gameplay

import "fmt"

type Requirement struct {
	Unlocks []string
	Items   map[string]int64
}

type BehaviorDefinition struct {
	Key                   string
	Name                  string
	Summary               string
	ActorType             string
	ExclusiveGroup        string
	ScheduleModes         []string
	SingleUsePerAscension bool
	DurationMinutes       int64
	StaminaCost           int64
	Requirements          Requirement
	Costs                 map[string]int64
	Outputs               map[string]int64
	OutputExpressions     map[string]string
	OutputChances         map[string]float64
	StatDeltas            map[string]int64
	RequiresMarketOpen    bool
	RequiresNight         bool
	GrantsUnlocks         []string
	MarketEffects         map[string]int64
	StartMessage          string
	CompleteMessage       string
	RepeatIntervalMin     int64
}

type UpgradeOutputDefinition struct {
	QueueSlotsDelta int64
	Unlocks         []string
	Items           map[string]int64
	StatDeltas      map[string]int64
}

type UpgradeDefinition struct {
	Key           string
	Name          string
	Summary       string
	Category      string
	GateTypes     []string
	MaxPurchases  int64
	CostScaling   float64
	OutputScaling float64
	Requirements  Requirement
	Costs         map[string]int64
	Outputs       UpgradeOutputDefinition
}

const (
	ActorPlayer = "player"
	ActorWorld  = "world"
)

var definitions = mergeDefinitions(playerBehaviorDefinitions, worldBehaviorDefinitions)

func mergeDefinitions(groups ...map[string]BehaviorDefinition) map[string]BehaviorDefinition {
	merged := map[string]BehaviorDefinition{}
	for _, group := range groups {
		for key, definition := range group {
			merged[key] = definition
		}
	}
	return merged
}

func GetBehaviorDefinition(key string) (BehaviorDefinition, bool) {
	definition, ok := definitions[key]
	return definition, ok
}

func ListBehaviorDefinitions() []BehaviorDefinition {
	all := make([]BehaviorDefinition, 0, len(definitions))
	for _, definition := range definitions {
		all = append(all, definition)
	}
	return all
}

func GetUpgradeDefinition(key string) (UpgradeDefinition, bool) {
	definition, ok := upgradeDefinitions[key]
	return definition, ok
}

func ListUpgradeDefinitions() []UpgradeDefinition {
	all := make([]UpgradeDefinition, 0, len(upgradeDefinitions))
	for _, definition := range upgradeDefinitions {
		all = append(all, definition)
	}
	return all
}

func ValidatePlayerBehaviorKey(key string) error {
	definition, ok := GetBehaviorDefinition(key)
	if !ok {
		return fmt.Errorf("unknown behavior key: %s", key)
	}
	if definition.ActorType != ActorPlayer {
		return fmt.Errorf("behavior %s is not a player behavior", key)
	}
	return nil
}

func BehaviorDisplayName(definition BehaviorDefinition) string {
	name := definition.Name
	if name == "" {
		name = HumanizeIdentifier(definition.Key)
	}
	if name == "" {
		return "Behavior"
	}
	return name
}

func HumanizeIdentifier(raw string) string {
	if raw == "" {
		return ""
	}

	replacer := map[rune]rune{'_': ' ', '-': ' '}
	chars := make([]rune, 0, len(raw))
	for _, token := range []rune(raw) {
		if replacement, ok := replacer[token]; ok {
			chars = append(chars, replacement)
			continue
		}
		chars = append(chars, token)
	}

	words := make([]string, 0)
	current := make([]rune, 0)
	flushCurrent := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, string(current))
		current = current[:0]
	}

	for _, token := range chars {
		if token == ' ' || token == '\t' || token == '\n' {
			flushCurrent()
			continue
		}
		current = append(current, token)
	}
	flushCurrent()

	if len(words) == 0 {
		return ""
	}

	titled := make([]string, 0, len(words))
	for _, word := range words {
		if word == "player" || word == "world" {
			continue
		}
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		first := runes[0]
		if first >= 'a' && first <= 'z' {
			first = first - ('a' - 'A')
		}
		runes[0] = first
		for i := 1; i < len(runes); i++ {
			if runes[i] >= 'A' && runes[i] <= 'Z' {
				runes[i] = runes[i] + ('a' - 'A')
			}
		}
		titled = append(titled, string(runes))
	}

	if len(titled) == 0 {
		return ""
	}

	result := ""
	for i, token := range titled {
		if i > 0 {
			result += " "
		}
		result += token
	}
	return result
}
