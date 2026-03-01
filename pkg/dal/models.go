package dal

import "time"

// BaseModel intentionally excludes DeletedAt.
// We avoid gorm.Model because its soft-delete behavior does not match
// save replacement flows (import/new game) where rows are expected to be
// physically removed and recreated.
type BaseModel struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Player struct {
	BaseModel
	Name string `gorm:"size:64;not null;uniqueIndex"`
}

type WorldState struct {
	BaseModel
	SimulationTick int64 `gorm:"not null;default:0"`
}

type WorldRuntimeState struct {
	BaseModel
	Key                  string    `gorm:"size:64;not null;uniqueIndex"`
	LastProcessedTickAt  time.Time `gorm:"not null"`
	CarryGameMinutes     float64   `gorm:"not null;default:0"`
	PendingBehaviorsJSON string    `gorm:"type:text;not null;default:'[]'"`
}

type InventoryEntry struct {
	BaseModel
	OwnerType string `gorm:"size:32;not null;index:idx_inventory_owner_item,priority:1"`
	OwnerID   uint   `gorm:"not null;index:idx_inventory_owner_item,priority:2"`
	ItemKey   string `gorm:"size:64;not null;index:idx_inventory_owner_item,priority:3"`
	Quantity  int64  `gorm:"not null;default:0"`
}

type PlayerUnlock struct {
	BaseModel
	PlayerID  uint   `gorm:"not null;index:idx_player_unlock,priority:1"`
	UnlockKey string `gorm:"size:64;not null;index:idx_player_unlock,priority:2"`
}

type PlayerStat struct {
	BaseModel
	PlayerID uint   `gorm:"not null;index:idx_player_stat,priority:1"`
	StatKey  string `gorm:"size:64;not null;index:idx_player_stat,priority:2"`
	Value    int64  `gorm:"not null;default:0"`
}

type BehaviorInstance struct {
	BaseModel
	Key               string `gorm:"size:64;not null;index"`
	ActorType         string `gorm:"size:32;not null;index"`
	ActorID           uint   `gorm:"not null;index"`
	State             string `gorm:"size:16;not null;index"`
	ScheduledAtTick   int64  `gorm:"not null;index"`
	StartedAtTick     int64  `gorm:"not null;default:0"`
	CompletesAtTick   int64  `gorm:"not null;default:0;index"`
	DurationMinutes   int64  `gorm:"not null;default:0"`
	PayloadJSON       string `gorm:"type:text;not null;default:'{}'"`
	ResultMessage     string `gorm:"type:text;not null;default:''"`
	FailureReason     string `gorm:"type:text;not null;default:''"`
	RepeatIntervalMin int64  `gorm:"not null;default:0"`
}

type WorldEvent struct {
	BaseModel
	Tick        int64  `gorm:"not null;index"`
	EventType   string `gorm:"size:64;not null;index"`
	Message     string `gorm:"type:text;not null"`
	Visibility  string `gorm:"size:16;not null;default:'public'"`
	Source      string `gorm:"size:32;not null;default:'system'"`
	ReferenceID uint   `gorm:"not null;default:0"`
}

type MarketPrice struct {
	BaseModel
	ItemKey     string `gorm:"size:64;not null;uniqueIndex"`
	Price       int64  `gorm:"not null;default:1"`
	LastDelta   int64  `gorm:"not null;default:0"`
	LastSource  string `gorm:"size:64;not null;default:''"`
	UpdatedTick int64  `gorm:"not null;default:0"`
}

type MarketHistory struct {
	BaseModel
	ItemKey      string `gorm:"size:64;not null;index"`
	Tick         int64  `gorm:"not null;index"`
	Price        int64  `gorm:"not null"`
	Delta        int64  `gorm:"not null;default:0"`
	Source       string `gorm:"size:64;not null;default:''"`
	SessionState string `gorm:"size:16;not null;default:'closed'"`
}

type AscensionState struct {
	BaseModel
	Key            string  `gorm:"size:64;not null;uniqueIndex"`
	Count          int64   `gorm:"not null;default:0"`
	WealthBonusPct float64 `gorm:"not null;default:0"`
}

func Models() []any {
	return []any{
		&Player{},
		&WorldState{},
		&WorldRuntimeState{},
		&InventoryEntry{},
		&PlayerUnlock{},
		&PlayerStat{},
		&BehaviorInstance{},
		&WorldEvent{},
		&MarketPrice{},
		&MarketHistory{},
		&AscensionState{},
	}
}
