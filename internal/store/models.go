package store

import "time"

type Account struct {
	ID          int64     `json:"id"`
	AccountType string    `json:"accountType"`
	Role        string    `json:"role"`
	Email       string    `json:"email,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Character struct {
	ID                int64      `json:"id"`
	AccountID         int64      `json:"-"`
	Name              string     `json:"name"`
	AvatarKey         string     `json:"avatarId"`
	WorldID           string     `json:"worldId"`
	X                 float64    `json:"x"`
	Y                 float64    `json:"y"`
	Z                 float64    `json:"z"`
	Yaw               float64    `json:"yaw"`
	Experience        int64      `json:"experience"`
	CurrentHP         int        `json:"currentHp"`
	MaxHP             int        `json:"maxHp"`
	Yen               int64      `json:"yen"`
	LastLoginAt       *time.Time `json:"lastLoginAt,omitempty"`
	TimePlayedSeconds int64      `json:"timePlayedSeconds"`
	LocationUpdatedAt *time.Time `json:"locationUpdatedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type ItemDefinition struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	MaxStack    int    `json:"maxStack"`
	Usable      bool   `json:"usable"`
	EffectKind  string `json:"effectKind,omitempty"`
	EffectValue int    `json:"effectValue,omitempty"`
}

type InventoryItem struct {
	ItemDefinition
	Quantity int `json:"quantity"`
}

type CharacterState struct {
	Character Character       `json:"character"`
	Inventory []InventoryItem `json:"inventory"`
}

type Location struct {
	WorldID string
	X       float64
	Y       float64
	Z       float64
	Yaw     float64
}
