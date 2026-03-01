package gameplay

var worldBehaviorDefinitions = map[string]BehaviorDefinition{
	"world_market_pulse": {
		Key:             "world_market_pulse",
		ActorType:       ActorWorld,
		DurationMinutes: 60,
		StartMessage:    "Legacy market pulse behavior is phasing out.",
		CompleteMessage: "Legacy pulse completed as market systems migrate.",
	},
	"world_market_ai_cycle": {
		Key:               "world_market_ai_cycle",
		ActorType:         ActorWorld,
		DurationMinutes:   60,
		RepeatIntervalMin: 60,
		StartMessage:      "Market analysts gather ledgers and regional rumors.",
		CompleteMessage:   "The market council concludes a new pricing posture.",
	},
	"world_merchant_convoy": {
		Key:               "world_merchant_convoy",
		ActorType:         ActorWorld,
		DurationMinutes:   90,
		RepeatIntervalMin: 180,
		MarketEffects: map[string]int64{
			"scrap": +1,
			"wood":  +1,
		},
		StartMessage:    "A merchant convoy prepares to enter nearby routes.",
		CompleteMessage: "The convoy arrives and demand rises across local stalls.",
	},
}
