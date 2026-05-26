package store

import "time"

type User struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Username     string    `json:"username"`
	Phone        string    `json:"phone"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Session struct {
	Token     string    `json:"-"`
	UserID    int64     `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type Item struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	Owner      string    `json:"owner"`
	Brand      string    `json:"brand"`
	Spec       string    `json:"spec"`
	Unit       string    `json:"unit"`
	Barcode    string    `json:"barcode"`
	MinStock   float64   `json:"minStock"`
	Note       string    `json:"note"`
	TotalStock float64   `json:"totalStock"`
	TotalCost  float64   `json:"totalCost"`
	IsLowStock bool      `json:"isLowStock"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Location struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PhotoPath   string    `json:"photoPath"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Batch struct {
	ID              int64      `json:"id"`
	ItemID          int64      `json:"itemId"`
	ItemName        string     `json:"itemName,omitempty"`
	LocationID      int64      `json:"locationId"`
	LocationName    string     `json:"locationName"`
	LocationPhoto   string     `json:"locationPhoto"`
	PhotoPath       string     `json:"photoPath"`
	InitialQuantity float64    `json:"initialQuantity"`
	CurrentQuantity float64    `json:"currentQuantity"`
	Cost            float64    `json:"cost"`
	PurchasedAt     time.Time  `json:"purchasedAt"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	Status          string     `json:"status"`
	Note            string     `json:"note"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type Movement struct {
	ID             int64     `json:"id"`
	ItemID         int64     `json:"itemId"`
	BatchID        int64     `json:"batchId"`
	UserID         int64     `json:"userId"`
	UserName       string    `json:"userName"`
	Type           string    `json:"type"`
	Quantity       float64   `json:"quantity"`
	FromLocationID *int64    `json:"fromLocationId,omitempty"`
	ToLocationID   *int64    `json:"toLocationId,omitempty"`
	Note           string    `json:"note"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Dashboard struct {
	LowStock []Item     `json:"lowStock"`
	Recent   []Movement `json:"recent"`
	Items    []Item     `json:"items"`
}
