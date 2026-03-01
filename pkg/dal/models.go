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

type Character struct {
	BaseModel
	AccountID uint   `gorm:"not null;index:idx_character_account_realm,priority:1"`
	PlayerID  uint   `gorm:"not null;uniqueIndex"`
	RealmID   uint   `gorm:"not null;default:1;index:idx_character_account_realm,priority:2;index:idx_character_realm_name,priority:1"`
	Name      string `gorm:"size:64;not null;index:idx_character_realm_name,priority:2"`
	IsPrimary bool   `gorm:"not null;default:false;index"`
	Status    string `gorm:"size:16;not null;default:'active';index"`
}

type Account struct {
	BaseModel
	Username     string `gorm:"size:64;not null;uniqueIndex"`
	PasswordHash string `gorm:"size:255;not null"`
	Status       string `gorm:"size:16;not null;default:'active';index"`
}

type AccountRole struct {
	BaseModel
	AccountID uint   `gorm:"not null;index:idx_account_role,priority:1"`
	RoleKey   string `gorm:"size:32;not null;index:idx_account_role,priority:2"`
}

type AccountSession struct {
	BaseModel
	AccountID  uint       `gorm:"not null;index"`
	TokenHash  string     `gorm:"size:128;not null;uniqueIndex"`
	ExpiresAt  time.Time  `gorm:"not null;index"`
	RevokedAt  *time.Time `gorm:"index"`
	UserAgent  string     `gorm:"size:255;not null;default:''"`
	RemoteAddr string     `gorm:"size:64;not null;default:''"`
	LastUsedAt *time.Time `gorm:"index"`
}

type WorldState struct {
	BaseModel
	RealmID        uint  `gorm:"not null;default:1;uniqueIndex:idx_world_state_realm"`
	SimulationTick int64 `gorm:"not null;default:0"`
}

type RealmConfig struct {
	BaseModel
	RealmID        uint   `gorm:"not null;default:1;uniqueIndex:idx_realm_config_realm"`
	DisplayName    string `gorm:"size:64;not null;default:''"`
	WhitelistOnly  bool   `gorm:"not null;default:false"`
	Decommissioned bool   `gorm:"not null;default:false"`
}

type RealmAccessGrant struct {
	BaseModel
	RealmID      uint   `gorm:"not null;default:1;uniqueIndex:idx_realm_access_grant,priority:1;index"`
	AccountID    uint   `gorm:"not null;uniqueIndex:idx_realm_access_grant,priority:2;index"`
	GrantedByID  uint   `gorm:"not null;default:0;index"`
	IsActive     bool   `gorm:"not null;default:true;index"`
	ReasonCode   string `gorm:"size:64;not null;default:''"`
	Note         string `gorm:"size:500;not null;default:''"`
	LastActionBy uint   `gorm:"not null;default:0;index"`
}

type WorldRuntimeState struct {
	BaseModel
	RealmID              uint      `gorm:"not null;default:1;uniqueIndex:idx_world_runtime_realm_key,priority:1"`
	Key                  string    `gorm:"size:64;not null;uniqueIndex:idx_world_runtime_realm_key,priority:2"`
	LastProcessedTickAt  time.Time `gorm:"not null"`
	CarryGameMinutes     float64   `gorm:"not null;default:0"`
	PendingBehaviorsJSON string    `gorm:"type:text;not null;default:'[]'"`
}

type InventoryEntry struct {
	BaseModel
	RealmID   uint   `gorm:"not null;default:1;index"`
	OwnerType string `gorm:"size:32;not null;index:idx_inventory_owner_item,priority:1"`
	OwnerID   uint   `gorm:"not null;index:idx_inventory_owner_item,priority:2"`
	ItemKey   string `gorm:"size:64;not null;index:idx_inventory_owner_item,priority:3"`
	Quantity  int64  `gorm:"not null;default:0"`
}

type PlayerUnlock struct {
	BaseModel
	RealmID   uint   `gorm:"not null;default:1;index"`
	PlayerID  uint   `gorm:"not null;index:idx_player_unlock,priority:1"`
	UnlockKey string `gorm:"size:64;not null;index:idx_player_unlock,priority:2"`
}

type PlayerStat struct {
	BaseModel
	RealmID  uint   `gorm:"not null;default:1;index"`
	PlayerID uint   `gorm:"not null;index:idx_player_stat,priority:1"`
	StatKey  string `gorm:"size:64;not null;index:idx_player_stat,priority:2"`
	Value    int64  `gorm:"not null;default:0"`
}

type BehaviorInstance struct {
	BaseModel
	RealmID           uint   `gorm:"not null;default:1;index"`
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
	RealmID     uint   `gorm:"not null;default:1;index"`
	Tick        int64  `gorm:"not null;index"`
	EventType   string `gorm:"size:64;not null;index"`
	Message     string `gorm:"type:text;not null"`
	Visibility  string `gorm:"size:16;not null;default:'public'"`
	Source      string `gorm:"size:32;not null;default:'system'"`
	ReferenceID uint   `gorm:"not null;default:0"`
}

type MarketPrice struct {
	BaseModel
	RealmID     uint   `gorm:"not null;default:1;uniqueIndex:idx_market_price_realm_item,priority:1"`
	ItemKey     string `gorm:"size:64;not null;uniqueIndex:idx_market_price_realm_item,priority:2"`
	Price       int64  `gorm:"not null;default:1"`
	LastDelta   int64  `gorm:"not null;default:0"`
	LastSource  string `gorm:"size:64;not null;default:''"`
	UpdatedTick int64  `gorm:"not null;default:0"`
}

type MarketHistory struct {
	BaseModel
	RealmID      uint   `gorm:"not null;default:1;index"`
	ItemKey      string `gorm:"size:64;not null;index"`
	Tick         int64  `gorm:"not null;index"`
	Price        int64  `gorm:"not null"`
	Delta        int64  `gorm:"not null;default:0"`
	Source       string `gorm:"size:64;not null;default:''"`
	SessionState string `gorm:"size:16;not null;default:'closed'"`
}

type AscensionState struct {
	BaseModel
	RealmID        uint    `gorm:"not null;default:1;uniqueIndex:idx_ascension_realm_key,priority:1"`
	Key            string  `gorm:"size:64;not null;uniqueIndex:idx_ascension_realm_key,priority:2"`
	Count          int64   `gorm:"not null;default:0"`
	WealthBonusPct float64 `gorm:"not null;default:0"`
}

type ChatChannel struct {
	BaseModel
	RealmID      uint   `gorm:"not null;default:1;uniqueIndex:idx_chat_channel_realm_key,priority:1;index"`
	ChannelKey   string `gorm:"size:32;not null;uniqueIndex:idx_chat_channel_realm_key,priority:2"`
	DisplayName  string `gorm:"size:64;not null"`
	Subject      string `gorm:"size:140;not null;default:''"`
	Description  string `gorm:"size:255;not null;default:''"`
	IsActive     bool   `gorm:"not null;default:true;index"`
	ManagedByKey string `gorm:"size:32;not null;default:'system'"`
}

type ChatChannelModeration struct {
	BaseModel
	RealmID     uint       `gorm:"not null;default:1;index:idx_chat_moderation_lookup,priority:1"`
	ChannelKey  string     `gorm:"size:32;not null;index:idx_chat_moderation_lookup,priority:2"`
	AccountID   uint       `gorm:"not null;index:idx_chat_moderation_lookup,priority:3"`
	ActionKey   string     `gorm:"size:16;not null"`
	Active      bool       `gorm:"not null;default:true;index"`
	ExpiresAt   *time.Time `gorm:"index"`
	ReasonCode  string     `gorm:"size:64;not null;index"`
	Note        string     `gorm:"size:500;not null;default:''"`
	CreatedByID uint       `gorm:"not null;index"`
}

type ChatChannelWordRule struct {
	BaseModel
	Term        string `gorm:"size:128;not null;index:idx_chat_word_rule_lookup,priority:1"`
	MatchMode   string `gorm:"size:16;not null;default:'contains';index:idx_chat_word_rule_lookup,priority:2"`
	IsActive    bool   `gorm:"not null;default:true;index"`
	ReasonCode  string `gorm:"size:64;not null;index"`
	Note        string `gorm:"size:500;not null;default:''"`
	CreatedByID uint   `gorm:"not null;index"`
}

type ChatMessageCensorshipTrace struct {
	BaseModel
	RealmID        uint   `gorm:"not null;default:1;index:idx_chat_censor_trace_lookup,priority:1"`
	ChannelKey     string `gorm:"size:32;not null;index:idx_chat_censor_trace_lookup,priority:2"`
	AccountID      uint   `gorm:"not null;default:0;index:idx_chat_censor_trace_lookup,priority:3"`
	CharacterID    uint   `gorm:"not null;default:0;index"`
	MessageID      uint   `gorm:"not null;default:0;index"`
	MessageClass   string `gorm:"size:16;not null;default:'player'"`
	OriginalLength int64  `gorm:"not null;default:0"`
	CensoredCount  int64  `gorm:"not null;default:0"`
	MatchedRules   int64  `gorm:"not null;default:0"`
}

type AdminAuditEvent struct {
	BaseModel
	RealmID        uint   `gorm:"not null;default:1;index"`
	ActorAccountID uint   `gorm:"not null;index"`
	ActionKey      string `gorm:"size:64;not null;index"`
	ReasonCode     string `gorm:"size:64;not null;index"`
	Note           string `gorm:"size:500;not null;default:''"`
	BeforeJSON     string `gorm:"type:text;not null;default:'{}'"`
	AfterJSON      string `gorm:"type:text;not null;default:'{}'"`
	OccurredTick   int64  `gorm:"not null;default:0;index"`
}

func Models() []any {
	return []any{
		&Account{},
		&AccountRole{},
		&AccountSession{},
		&Player{},
		&Character{},
		&RealmConfig{},
		&RealmAccessGrant{},
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
		&ChatChannel{},
		&ChatChannelModeration{},
		&ChatChannelWordRule{},
		&ChatMessageCensorshipTrace{},
		&AdminAuditEvent{},
	}
}
