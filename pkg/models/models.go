package models

import "time"

// User represents user records in "users" collection
type User struct {
	ID                string    `json:"_id" bson:"_id"`
	Email             string    `json:"email" bson:"email"`
	Name              string    `json:"name" bson:"name"`
	Password          []byte    `json:"-" bson:"pwd,omitempty"`
	Role              string    `json:"role" bson:"role"`
	Points            int32     `json:"points" bson:"points"`
	IsProfileComplete bool      `json:"is_profile_complete" bson:"is_profile_complete"`
	ProfilePicture    string    `json:"profilePicture" bson:"profilePicture"`
	MobileNumber      string    `json:"mobile_number" bson:"mobile_number"`
	FirstLogin        bool      `json:"first_login" bson:"first_login"`
	IsRewardGiven     bool      `json:"isrewardgiven" bson:"isrewardgiven"`
	CreatedAt         time.Time `json:"created_on" bson:"created_on"`
}

// Post represents requirements or stocks in "post" collection
type Post struct {
	ID              string    `json:"_id" bson:"_id"`
	PostType        string    `json:"post_type" bson:"post_type"` // "requirements" or "stocks"
	BuyerId         string    `json:"buyerId,omitempty" bson:"buyerId,omitempty"`
	SellerId        string    `json:"sellerId,omitempty" bson:"sellerId,omitempty"`
	RequiredQty     int32     `json:"requiredqty,omitempty" bson:"requiredqty,omitempty"`
	AvailableQty    int32     `json:"availableqty,omitempty" bson:"availableqty,omitempty"`
	ConfirmedKg     int32     `json:"confirmedKg,omitempty" bson:"confirmedKg,omitempty"`
	ConfirmedUserId []string  `json:"confirmedUserId,omitempty" bson:"confirmedUserId,omitempty"`
	Grade           string    `json:"grade" bson:"grade"`
	YearOfCrop      string    `json:"yearOfCrop" bson:"yearOfCrop"`
	Status          string    `json:"status" bson:"status"` // "Active", "closed"
	Viewed          []string  `json:"viewed" bson:"viewed"`
	Favorite        []string  `json:"favorite" bson:"favorite"`
	CreatedOn       time.Time `json:"created_on" bson:"created_on"`
	CreatedBy       string    `json:"created_by" bson:"created_by"`
}

// Response represents quotes or stock_quotes in "response" collection
type Response struct {
	ID            string    `json:"_id" bson:"_id"`
	PostType      string    `json:"post_type" bson:"post_type"` // "quotes" or "stock_quotes"
	RequirementId string    `json:"requirementId,omitempty" bson:"requirementId,omitempty"`
	StockId       string    `json:"stockId,omitempty" bson:"stockId,omitempty"`
	BuyerId       string    `json:"buyerId,omitempty" bson:"buyerId,omitempty"`
	MerchantId    string    `json:"merchantId,omitempty" bson:"merchantId,omitempty"`
	SupplyQtyKg   int32     `json:"supplyQtyKg,omitempty" bson:"supplyQtyKg,omitempty"`
	Quantity      int32     `json:"quantity,omitempty" bson:"quantity,omitempty"`
	Status        string    `json:"status" bson:"status"` // "new", "viewed", "confirmed", "processing"
	CreatedOn     time.Time `json:"created_on" bson:"created_on"`
	CreatedBy     string    `json:"created_by" bson:"created_by"`
}

// WalletTxn represents transaction logs in "wallet_txn" collection
type WalletTxn struct {
	ID             string    `json:"_id" bson:"_id"`
	UserID         string    `json:"user_id" bson:"user_id"`
	Description    string    `json:"description" bson:"description"`
	RefID          string    `json:"ref_id" bson:"ref_id"`
	OpeningBalance int32     `json:"opening_balance" bson:"opening_balance"`
	ClosingBalance int32     `json:"closing_balance" bson:"closing_balance"`
	Type           string    `json:"type" bson:"type"` // "CR" or "DR"
	Amount         int32     `json:"amount" bson:"amount"`
	CreatedOn      time.Time `json:"created_on" bson:"created_on"`
}

// Settings represents payment and points config in "settings" collection
type Settings struct {
	ID                     string `json:"_id" bson:"_id"`
	PointRatio             int32  `json:"pointRatio" bson:"pointratio"`
	MoneyRatio             int32  `json:"moneyRatio" bson:"moneyratio"`
	PostDetectionPoint     int32  `json:"postDetectionPoint" bson:"postdetectionpoint"`
	EnquiresDetectionPoint int32  `json:"enquiresDetectionPoint" bson:"enquiresdetectionpoint"`
	SetReward              bool   `json:"setreward" bson:"setreward"`
	RewardPoint            int32  `json:"rewardpoint" bson:"rewardpoint"`
}

// TemporaryUser stores OTP codes in "temporary_user" collection
type TemporaryUser struct {
	ID       string    `json:"_id" bson:"_id"` // email
	Otp      int       `json:"otp" bson:"otp"`
	Verified bool      `json:"verified" bson:"verified"`
	IssuedOn time.Time `json:"issued_on" bson:"issued_on"`
}

// PaginationRequest represents query query payload from Flutter client
type PaginationRequest struct {
	Start                  int                  `json:"start" bson:"start"`
	End                    int                  `json:"end" bson:"end"`
	Filter                 []FilterCondition    `json:"filter,omitempty" bson:"filter,omitempty"`
	Sort                   []SortCriteria       `json:"sort,omitempty" bson:"sort,omitempty"`
	MultiFieldSearchFilter []NewFilterCondition `json:"multi_field_search_filter,omitempty" bson:"multi_field_search_filter,omitempty"`
}

// NewFilterCondition represents multi field search filter item
type NewFilterCondition struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"`
	Type     string      `json:"type,omitempty"`
	Value    interface{} `json:"value,omitempty"`
}

// SortCriteria represents a sort ordering rule
type SortCriteria struct {
	Sort  string `json:"sort"`
	ColID string `json:"colId"`
}

// FilterCondition defines a logical group of query condition groups
type FilterCondition struct {
	Clause     string           `json:"clause,omitempty" bson:"clause,omitempty"`
	Conditions []ConditionGroup `json:"conditions,omitempty" bson:"conditions,omitempty"`
}

// ConditionGroup defines details of query condition rules
type ConditionGroup struct {
	Operator             string           `json:"operator" bson:"operator"`
	Column               string           `json:"column" bson:"column"`
	ParentCollectionName string           `json:"parentCollectionName" bson:"parentCollectionName"`
	Value_type           string           `json:"value_type" bson:"value_type"`
	Type                 string           `json:"type" bson:"type"`
	Value                interface{}      `json:"value" bson:"value"`
	Clause               string           `json:"clause" bson:"clause"`
	Conditions           []ConditionGroup `json:"conditions" bson:"conditions"`
}
