package gameplay

import "fmt"

type Requirement struct {
	Unlocks []string
	Items   map[string]int64
}

type BehaviorDefinition struct {
	Key                string
	ActorType          string
	DurationMinutes    int64
	StaminaCost        int64
	Requirements       Requirement
	Costs              map[string]int64
	Outputs            map[string]int64
	OutputExpressions  map[string]string
	OutputChances      map[string]float64
	StatDeltas         map[string]int64
	RequiresMarketOpen bool
	GrantsUnlocks      []string
	MarketEffects      map[string]int64
	StartMessage       string
	CompleteMessage    string
	RepeatIntervalMin  int64
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
