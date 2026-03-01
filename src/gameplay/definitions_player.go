package gameplay

var playerBehaviorDefinitions = map[string]BehaviorDefinition{
	"player_scavenge_scrap": {
		Key:             "player_scavenge_scrap",
		ActorType:       ActorPlayer,
		DurationMinutes: 30,
		StaminaCost:     18,
		Outputs: map[string]int64{
			"scrap": 1,
		},
		OutputExpressions: map[string]string{
			"coins": "1+d2",
		},
		OutputChances: map[string]float64{
			"coins": 0.35,
		},
		StartMessage:    "You search alleys and abandoned lots for anything useful.",
		CompleteMessage: "You return with salvage and a few coins from quick trades.",
	},
	"player_sell_scrap": {
		Key:             "player_sell_scrap",
		ActorType:       ActorPlayer,
		DurationMinutes: 10,
		StaminaCost:     4,
		Requirements:    Requirement{Items: map[string]int64{"scrap": 1}},
		Costs: map[string]int64{
			"scrap": 1,
		},
		RequiresMarketOpen: true,
		StartMessage:       "You head toward the nearest buyer with scrap in hand.",
		CompleteMessage:    "The scrap changes hands and your coin pouch feels heavier.",
	},
	"player_sell_wood": {
		Key:             "player_sell_wood",
		ActorType:       ActorPlayer,
		DurationMinutes: 12,
		StaminaCost:     6,
		Requirements:    Requirement{Items: map[string]int64{"wood": 1}},
		Costs: map[string]int64{
			"wood": 1,
		},
		RequiresMarketOpen: true,
		StartMessage:       "You carry your cut wood to market stalls and lumber buyers.",
		CompleteMessage:    "The wood lot sells and your coin pouch grows.",
	},
	"player_petition_forester": {
		Key:             "player_petition_forester",
		ActorType:       ActorPlayer,
		DurationMinutes: 45,
		StaminaCost:     6,
		Requirements:    Requirement{Items: map[string]int64{"coins": 12}},
		Costs: map[string]int64{
			"coins": 12,
		},
		GrantsUnlocks:   []string{"forest_access"},
		StartMessage:    "You wait in a long line to petition for forest cutting rights.",
		CompleteMessage: "A stamped permit is granted. The forest now opens to your axe.",
	},
	"player_chop_wood": {
		Key:             "player_chop_wood",
		ActorType:       ActorPlayer,
		DurationMinutes: 25,
		StaminaCost:     22,
		Requirements: Requirement{
			Unlocks: []string{"forest_access"},
		},
		OutputExpressions: map[string]string{
			"wood": "1-3",
		},
		StartMessage:    "You enter the forest edge and begin chopping carefully.",
		CompleteMessage: "You haul back cut wood for crafting or sale.",
	},
	"player_pushups": {
		Key:             "player_pushups",
		ActorType:       ActorPlayer,
		DurationMinutes: 20,
		StaminaCost:     14,
		StatDeltas: map[string]int64{
			"strength": 1,
		},
		StartMessage:    "You drop to the ground and begin a disciplined pushup set.",
		CompleteMessage: "Your muscles burn and adapt. Strength inches upward.",
	},
	"player_run_training": {
		Key:             "player_run_training",
		ActorType:       ActorPlayer,
		DurationMinutes: 30,
		StaminaCost:     20,
		StatDeltas: map[string]int64{
			"max_stamina": 2,
		},
		StartMessage:    "You settle into a steady running pace to build endurance.",
		CompleteMessage: "Your lungs and legs adapt. Your stamina ceiling improves.",
	},
	"player_socialize_market": {
		Key:             "player_socialize_market",
		ActorType:       ActorPlayer,
		DurationMinutes: 30,
		StaminaCost:     8,
		StatDeltas: map[string]int64{
			"social": 1,
		},
		StartMessage:    "You spend time talking with traders, runners, and regular buyers.",
		CompleteMessage: "You learn how people negotiate, and your social instincts sharpen.",
	},
	"player_buy_weights": {
		Key:             "player_buy_weights",
		ActorType:       ActorPlayer,
		DurationMinutes: 15,
		Requirements:    Requirement{Items: map[string]int64{"coins": 60}},
		Costs: map[string]int64{
			"coins": 60,
		},
		GrantsUnlocks:   []string{"home_weights"},
		StartMessage:    "You search second-hand stalls for a battered but usable weight set.",
		CompleteMessage: "You haul a weight set home. Heavier training is now possible.",
	},
	"player_weight_training": {
		Key:             "player_weight_training",
		ActorType:       ActorPlayer,
		DurationMinutes: 35,
		StaminaCost:     26,
		Requirements: Requirement{
			Unlocks: []string{"home_weights"},
		},
		StatDeltas: map[string]int64{
			"strength": 3,
		},
		StartMessage:    "You commit to a focused weight training routine.",
		CompleteMessage: "Progressive overload pays off. Your strength rises noticeably.",
	},
}
