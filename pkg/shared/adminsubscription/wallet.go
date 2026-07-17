package adminsubscription

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

type UsageType string

const (
	UsageTypePosts     UsageType = "posts"
	UsageTypeEnquiries UsageType = "enquiries"
)

type TxnType string

const (
	TxnCR TxnType = "CR"
	TxnDR TxnType = "DR"
)

type WalletTxn struct {
	ID             string    `bson:"_id"`
	UserID         string    `bson:"user_id"`
	Description    string    `bson:"description"`
	RefID          string    `bson:"ref_id"`
	OpeningBalance int32     `bson:"opening_balance"`
	ClosingBalance int32     `bson:"closing_balance"`
	Type           TxnType   `bson:"type"`
	Amount         int32     `bson:"amount"`
	CreatedOn      time.Time `bson:"created_on"`
}

var (
	balanceCache      = make(map[string]int32)
	balanceCacheMutex sync.RWMutex

	cachedSettings     *walletSettings
	settingsCacheMutex sync.RWMutex
)

type walletSettings struct {
	PostDetectionPoint     int32 `bson:"postdetectionpoint"`
	EnquiresDetectionPoint int32 `bson:"enquiresdetectionpoint"`
	PointRatio             int32 `bson:"pointratio"`
	MoneyRatio             int32 `bson:"moneyratio"`
}

func loadSettings(ctx context.Context, settingsCol *mongo.Collection) (*walletSettings, error) {
	settingsCacheMutex.RLock()
	if cachedSettings != nil {
		settingsCacheMutex.RUnlock()
		return cachedSettings, nil
	}
	settingsCacheMutex.RUnlock()

	var s walletSettings
	if err := settingsCol.FindOne(ctx, bson.M{}).Decode(&s); err != nil {
		return nil, errors.New("failed to load settings")
	}

	settingsCacheMutex.Lock()
	cachedSettings = &s
	settingsCacheMutex.Unlock()

	return &s, nil
}

// InvalidateSettingsCache clears the settings cache so the next call reloads from DB.
func InvalidateSettingsCache() {
	settingsCacheMutex.Lock()
	cachedSettings = nil
	settingsCacheMutex.Unlock()
}

// LoadBalance returns the user's points balance from cache,
// falling back to DB if not present, and stores it in cache.
func LoadBalance(ctx context.Context, usersCol *mongo.Collection, userID string) (int32, error) {
	balanceCacheMutex.RLock()
	if val, ok := balanceCache[userID]; ok {
		balanceCacheMutex.RUnlock()
		return val, nil
	}
	balanceCacheMutex.RUnlock()

	var userData struct {
		Points int32 `bson:"points"`
	}
	if err := usersCol.FindOne(ctx, bson.M{"_id": userID}).Decode(&userData); err != nil {
		return 0, errors.New("user not found")
	}

	balanceCacheMutex.Lock()
	balanceCache[userID] = userData.Points
	balanceCacheMutex.Unlock()

	return userData.Points, nil
}

// It should give input with exiting value + new points, so that it can keep in meomry
func setBalanceCache(userID string, value int32) {
	balanceCacheMutex.Lock()
	defer balanceCacheMutex.Unlock()
	balanceCache[userID] = value
}

// ValidatePoints checks if the user has sufficient points balance.
func ValidatePoints(ctx context.Context, usersCol *mongo.Collection, userID string, requiredPoints int32) error {
	balance, err := LoadBalance(ctx, usersCol, userID)
	if err != nil {
		return err
	}
	if balance < requiredPoints {
		return errors.New("insufficient balance")
	}
	return nil
}

// WriteWalletTxn inserts a new wallet transaction entry into the given collection.
func WriteWalletTxn(ctx context.Context, walletCol *mongo.Collection, txn WalletTxn) error {
	txn.ID = helper.Generateuniquekey()
	txn.CreatedOn = time.Now().UTC()
	_, err := walletCol.InsertOne(ctx, txn)
	return err
}

// CreditUserPoints adds points to the user, updates cache, and writes a CR wallet txn.
func CreditUserPoints(ctx context.Context, db *mongo.Database, userID string, amount int32) error {
	currentBalance, err := LoadBalance(ctx, db.Collection("users"), userID)
	if err != nil {
		return err
	}
	settings, _ := loadSettings(ctx, db.Collection("settings"))
	points := amount * settings.PointRatio / settings.MoneyRatio
	_, err = db.Collection("users").UpdateOne(
		ctx,
		bson.M{"_id": userID},
		bson.M{
			"$inc": bson.M{"points": points},
			"$set": bson.M{"updated_on": time.Now().UTC()},
		},
	)
	if err != nil {
		return errors.New("failed to credit user points: " + err.Error())
	}

	setBalanceCache(userID, currentBalance+points)

	return WriteWalletTxn(ctx, db.Collection("wallet_txn"), WalletTxn{
		UserID: userID,

		OpeningBalance: currentBalance,
		ClosingBalance: currentBalance + points,
		Type:           TxnCR,
		Amount:         points,
	})
}

// CheckAndDeduct resolves deduction points from settings for the given usageType,
// validates the user has sufficient balance, then deducts and writes a wallet txn.
func ChekSufficentPoint(ctx context.Context, db *mongo.Database, userID string, usageType UsageType) error {
	settings, err := loadSettings(ctx, db.Collection("settings"))
	if err != nil {
		return err
	}

	var deductionPoints int32
	switch usageType {
	case UsageTypePosts:
		deductionPoints = settings.PostDetectionPoint
	case UsageTypeEnquiries:
		deductionPoints = settings.EnquiresDetectionPoint
	default:
		return errors.New("invalid usage type")
	}

	if err := ValidatePoints(ctx, db.Collection("users"), userID, deductionPoints); err != nil {
		return err
	}

	return nil
}

// DeductUserPoints reads the deduction point from settings based on usageType,
// verifies the user has sufficient balance, deducts from the user document,
// and writes a wallet transaction entry.
func DeductUserPoints(ctx context.Context, usersCol *mongo.Collection, settingsCol *mongo.Collection, walletCol *mongo.Collection, userID string, usageType UsageType, description string, refID string) error {
	// 1. Fetch deduction points from settings (cache-first)
	settings, err := loadSettings(ctx, settingsCol)
	if err != nil {
		return err
	}

	var deductionPoints int32
	switch usageType {
	case UsageTypePosts:
		deductionPoints = settings.PostDetectionPoint
	case UsageTypeEnquiries:
		deductionPoints = settings.EnquiresDetectionPoint
	default:
		return errors.New("invalid usage type")
	}

	if deductionPoints <= 0 {
		return errors.New("deduction points not configured in settings")
	}

	// 2. Load current balance (cache-first)
	currentBalance, err := LoadBalance(ctx, usersCol, userID)
	if err != nil {
		return err
	}

	// 3. Verify sufficient balance
	if err := ValidatePoints(ctx, usersCol, userID, deductionPoints); err != nil {
		return err
	}

	closingBalance := currentBalance - deductionPoints

	// 4. Deduct from user
	_, err = usersCol.UpdateOne(
		ctx,
		bson.M{"_id": userID},
		bson.M{
			"$inc": bson.M{"points": -deductionPoints},
			"$set": bson.M{"updated_on": time.Now().UTC()},
		},
	)
	if err != nil {
		return errors.New("failed to deduct user points: " + err.Error())
	}

	// 5. Update cache after successful deduction
	setBalanceCache(userID, closingBalance)

	// 6. Write wallet transaction
	return WriteWalletTxn(ctx, walletCol, WalletTxn{
		UserID:         userID,
		Description:    description,
		RefID:          refID,
		OpeningBalance: currentBalance,
		ClosingBalance: closingBalance,
		Type:           TxnDR,
		Amount:         deductionPoints,
	})
}
