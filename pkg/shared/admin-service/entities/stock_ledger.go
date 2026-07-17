package entities

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

type StockLedgerEntry struct {
	ID                 string    `json:"_id,omitempty" bson:"_id,omitempty"`
	CreatedBy          string    `json:"created_by" bson:"created_by"`
	CreatedOn          time.Time `json:"created_on" bson:"created_on"`
	UpdatedBy          string    `json:"updated_by,omitempty" bson:"updated_by,omitempty"`
	UpdatedOn          time.Time `json:"updated_on,omitempty" bson:"updated_on,omitempty"`
	PurchaseID         string    `json:"purchase_id,omitempty" bson:"purchase_id,omitempty"`
	SaleId             string    `json:"sale_id,omitempty" bson:"sale_id,omitempty"`
	Origin             string    `json:"origin" bson:"origin"`
	StockType          string    `json:"stock_type" bson:"stock_type"`
	WarehouseId        string    `json:"warehouse_id,omitempty" bson:"warehouse_id,omitempty"`
	ProductId          string    `json:"product_id,omitempty" bson:"product_id,omitempty"`
	FactoryId          string    `json:"factory_id,omitempty" bson:"factory_id,omitempty"`
	ProductionId       string    `json:"production_id,omitempty" bson:"production_id,omitempty"`
	TransactionType    string    `json:"transaction_type" bson:"transaction_type"`
	TransactionDate    time.Time `json:"transaction_date" bson:"transaction_date"`
	CustomerName       string    `json:"customer_name,omitempty" bson:"customer_name,omitempty"`
	TransactionBalance float64   `json:"transaction_balance" bson:"transaction_balance"`
	OpeningBalance     float64   `json:"opening_balance" bson:"opening_balance"`
	ClosingBalance     float64   `json:"closing_balance" bson:"closing_balance"`
	Remarks            string    `json:"remarks,omitempty" bson:"remarks,omitempty"`
	ProcessID          int32     `json:"process_id,omitempty" bson:"process_id,omitempty"`
	ProcessType        string    `json:"process_type,omitempty" bson:"process_type,omitempty"`
	Location           string    `json:"location,omitempty" bson:"location,omitempty"`
	RefId              string    `json:"ref_id,omitempty" bson:"ref_id,omitempty"`
}

type StockBalance struct {
	ID            string    `json:"_id,omitempty" bson:"_id,omitempty"`
	PurchaseID    string    `json:"purchase_id" bson:"purchase_id"`
	WarehouseId   string    `json:"warehouse_id,omitempty" bson:"warehouse_id,omitempty"`
	Origin        string    `json:"origin" bson:"origin"`
	FactoryId     string    `json:"factory_id,omitempty" bson:"factory_id,omitempty"`
	StockType     string    `json:"stock_type" bson:"stock_type"`
	ProductId     string    `json:"product_id,omitempty" bson:"product_id,omitempty"`
	AvailableQty  float64   `json:"available_qty" bson:"available_qty"`
	LastUpdatedBy string    `json:"last_updated_by,omitempty" bson:"last_updated_by,omitempty"`
	LastUpdatedOn time.Time `json:"last_updated_on,omitempty" bson:"last_updated_on,omitempty"`
	CreatedBy     string    `json:"created_by" bson:"created_by"`
	CreatedOn     time.Time `json:"created_on" bson:"created_on"`
	ProcessID     int32     `json:"process_id,omitempty" bson:"process_id,omitempty"`
	ProcessType   string    `json:"process_type,omitempty" bson:"process_type,omitempty"`
	Location      string    `json:"location,omitempty" bson:"location,omitempty"`
}

type LedgerFilter struct {
	PurchaseID  string `json:"purchase_id" bson:"purchase_id"`
	ProductID   string `json:"product_id" bson:"product_id"`
	Origin      string `json:"origin" bson:"origin"`
	WarehouseID string `json:"warehouse_id" bson:"warehouse_id"`
	RefId       string `json:"ref_id" bson:"ref_id"`
}

func ledgerEntryExists(organizationID string, filter LedgerFilter) (bool, error) {
	database := database.GetConnection(organizationID)
	collection := database.Collection("stock_ledger")

	query := bson.M{
		"purchase_id":  filter.PurchaseID,
		"product_id":   filter.ProductID,
		"origin":       filter.Origin,
		"warehouse_id": filter.WarehouseID,
		"ref_id":       filter.RefId,
	}
	helper.UpdateDateObject(query)

	count, err := collection.CountDocuments(context.Background(), query)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
func UpdateLedgerEntryByIDs(organizationID string, ledgerFilter LedgerFilter, newQuantity float64, userID string, txnType string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		var targetEntry StockLedgerEntry
		filter := bson.M{
			"purchase_id":  ledgerFilter.PurchaseID,
			"product_id":   ledgerFilter.ProductID,
			"origin":       ledgerFilter.Origin,
			"warehouse_id": ledgerFilter.WarehouseID,
			"ref_id":       ledgerFilter.RefId,
		}
		err := database.Collection("stock_ledger").FindOne(sessionCtx, filter).Decode(&targetEntry)
		if err != nil {
			return nil, errors.New("ledger entry not found with given criteria")
		}

		oldQuantity := targetEntry.TransactionBalance
		quantityDiff := newQuantity - oldQuantity
		if quantityDiff == 0 {
			return nil, nil
		}

		filter = bson.M{
			"origin":       targetEntry.Origin,
			"purchase_id":  targetEntry.PurchaseID,
			"warehouse_id": targetEntry.WarehouseId,
			"product_id":   targetEntry.ProductId,
			"created_on":   bson.M{"$gte": targetEntry.CreatedOn},
		}

		opts := options.Find().SetSort(bson.M{"created_on": 1})

		cursor, err := database.Collection("stock_ledger").Find(sessionCtx, filter, opts)
		if err != nil {
			return nil, err
		}

		defer cursor.Close(sessionCtx)

		var entries []StockLedgerEntry
		if err = cursor.All(sessionCtx, &entries); err != nil {
			return nil, err
		}

		for i := range entries {
			if entries[i].ID == targetEntry.ID {
				// Update the transaction balance for the target entry
				entries[i].TransactionBalance = newQuantity
				// Keep the original opening balance (it should not change)
				// Only recalculate closing balance
				stockDelta := calculateStockDelta(entries[i].TransactionType, newQuantity)
				entries[i].ClosingBalance = entries[i].OpeningBalance + stockDelta
			} else {
				// For subsequent entries, update opening balance from previous entry's closing
				if i > 0 {
					entries[i].OpeningBalance = entries[i-1].ClosingBalance
				}
				stockDelta := calculateStockDelta(entries[i].TransactionType, entries[i].TransactionBalance)
				entries[i].ClosingBalance = entries[i].OpeningBalance + stockDelta
			}

			entries[i].UpdatedBy = userID
			entries[i].UpdatedOn = time.Now()

			update := bson.M{
				"$set": bson.M{
					"transaction_balance": entries[i].TransactionBalance,
					"opening_balance":     entries[i].OpeningBalance,
					"closing_balance":     entries[i].ClosingBalance,
					"updated_by":          userID,
					"updated_on":          time.Now(),
				},
			}

			_, err = database.Collection("stock_ledger").UpdateOne(context.Background(), bson.M{"_id": entries[i].ID}, update)
			if err != nil {
				return nil, err
			}
		}

		if targetEntry.TransactionType == "outWard-jobWork" || targetEntry.TransactionType == "sale" || targetEntry.TransactionType == "TRANSFER-OUT" {
			quantityDiff = -quantityDiff
		}
		if err := updateStockBalance(sessionCtx, organizationID, targetEntry.Origin, targetEntry.StockType, targetEntry.WarehouseId, targetEntry.FactoryId, targetEntry.PurchaseID, targetEntry.ProductId, quantityDiff, userID, "", targetEntry.TransactionType); err != nil {
			return nil, err
		}

		return nil, nil
	})

	return err
}

func updateStockBalance(
	sessionCtx mongo.SessionContext,
	organizationID, origin, stockType, warehouseID, factoryID, purchaseID, productID string,
	deltaAmount float64,
	userID string,
	processType string,
	transactionType string,
) error {
	collection := database.GetConnection(organizationID).Collection("stock_in_hand")
	filter := bson.M{
		"origin":      origin,
		"stock_type":  stockType,
		"purchase_id": purchaseID,
	}
	if productID != "" {
		filter["product_id"] = productID
	}
	if warehouseID != "" {
		filter["warehouse_id"] = warehouseID
	}
	if processType != "" {
		filter["process_type"] = processType
	}
	location := warehouseID
	if stockType == "WIP" {
		location = processType
	}
	if transactionType == "inWard-jobWork" || stockType == "outWard-jobWork" {
		location = "JOBWORK"
	}

	var existingStock StockBalance
	err := collection.FindOne(sessionCtx, filter).Decode(&existingStock)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			if deltaAmount < 0 {
				return errors.New("insufficient stock: cannot create negative stock_in_hand")
			}
			newStockRecord := StockBalance{
				ID:            uuid.New().String(),
				Origin:        origin,
				StockType:     stockType,
				WarehouseId:   warehouseID,
				AvailableQty:  deltaAmount,
				LastUpdatedBy: userID,
				PurchaseID:    purchaseID,
				FactoryId:     factoryID,
				ProductId:     productID,
				LastUpdatedOn: time.Now(),
				CreatedBy:     userID,
				CreatedOn:     time.Now(),
				Location:      location,
				ProcessType:   processType,
			}
			_, err = collection.InsertOne(sessionCtx, newStockRecord)
			return err
		}
		return err
	}

	newBalance := existingStock.AvailableQty + deltaAmount
	if newBalance < 0 {
		return errors.New("insufficient stock: operation would create negative balance")
	}

	update := bson.M{
		"$set": bson.M{
			"available_qty":   newBalance,
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
	}
	_, err = collection.UpdateOne(sessionCtx, filter, update)
	return err
}

func getLastLedgerBalance(sessionCtx mongo.SessionContext, organizationID, purchaseID string, wareHouseId string, orginId string, stockType string, productId string) (float64, error) {
	filter := bson.M{"origin": orginId, "purchase_id": purchaseID, "warehouse_id": wareHouseId, "stock_type": stockType, "product_id": productId}
	// Sort by created_on descending to get the most recent entry
	opts := options.FindOne().SetSort(bson.M{"created_on": -1})

	var lastEntry StockLedgerEntry
	err := database.GetConnection(organizationID).Collection("stock_ledger").FindOne(sessionCtx, filter, opts).Decode(&lastEntry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// No previous entries, starting balance is 0
			return 0, nil
		}
		return 0, err
	}
	return lastEntry.ClosingBalance, nil
}

func getAvailableBalance(sessionCtx mongo.SessionContext, organizationID, purchaseID string, wareHouseId string, orginId string, stockType string, productId string) (float64, error) {
	filter := bson.M{"origin": orginId, "purchase_id": purchaseID, "warehouse_id": wareHouseId, "stock_type": stockType, "product_id": productId}
	opts := options.FindOne().SetSort(bson.M{"created_on": -1})

	var lastEntry StockBalance
	err := database.GetConnection(organizationID).Collection("stock_in_hand").FindOne(sessionCtx, filter, opts).Decode(&lastEntry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return lastEntry.AvailableQty, nil
}

func getProductLastLedgerBalance(sessionCtx mongo.SessionContext, organizationID, productID string, purchaseID string, wareHouseId string, orginId string) (float64, error) {
	filter := bson.M{"origin": orginId, "purchase_id": purchaseID, "warehouse_id": wareHouseId, "product_id": productID}
	opts := options.FindOne().SetSort(bson.M{"created_on": -1})

	var lastEntry StockLedgerEntry
	err := database.GetConnection(organizationID).Collection("stock_ledger").FindOne(sessionCtx, filter, opts).Decode(&lastEntry)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return lastEntry.ClosingBalance, nil
}

func ProcessStockLedgerEntry(organizationID string, purchaseDocument StockLedgerEntry, userID string) error {

	database := database.GetConnection(organizationID)
	client := database.Client()
	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		return nil, processStockLedgerEntryWithSession(sessionCtx, organizationID, purchaseDocument, userID)
	})

	return err

}

// processStockLedgerEntryWithSession processes a stock ledger entry within an existing transaction session
func processStockLedgerEntryWithSession(sessionCtx mongo.SessionContext, organizationID string, purchaseDocument StockLedgerEntry, userID string) error {
	database := database.GetConnection(organizationID)
	
	helper.UpdateDateObject(purchaseDocument)
	_, err := database.Collection("stock_ledger").InsertOne(sessionCtx, purchaseDocument)
	if err != nil {
		return err
	}

	if purchaseDocument.TransactionType == "outWard-jobWork" || purchaseDocument.TransactionType == "sale" || purchaseDocument.TransactionType == "TRANSFER-OUT" {
		purchaseDocument.TransactionBalance = -purchaseDocument.TransactionBalance
	}
	err = updateStockBalance(sessionCtx, organizationID, purchaseDocument.Origin, purchaseDocument.StockType, purchaseDocument.WarehouseId, purchaseDocument.FactoryId, purchaseDocument.PurchaseID, purchaseDocument.ProductId, purchaseDocument.TransactionBalance, userID, "", purchaseDocument.TransactionType)
	if err != nil {
		return err
	}

	return nil
}

func DeleteLedgerEntryByRefID(organizationID string, refID string, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		// Find all entries with the given ref_id
		filter := bson.M{"ref_id": refID}
		cursor, err := database.Collection("stock_ledger").Find(sessionCtx, filter)
		if err != nil {
			return nil, err
		}
		defer cursor.Close(sessionCtx)

		var targetEntries []StockLedgerEntry
		if err = cursor.All(sessionCtx, &targetEntries); err != nil {
			return nil, err
		}

		if len(targetEntries) == 0 {
			return nil, nil // Nothing to delete
		}

		// Process each entry
		for _, targetEntry := range targetEntries {
			// Delete the entry
			_, err := database.Collection("stock_ledger").DeleteOne(sessionCtx, bson.M{"_id": targetEntry.ID})
			if err != nil {
				return nil, err
			}

			// Recalculate balances for subsequent entries
			filter = bson.M{
				"origin":       targetEntry.Origin,
				"purchase_id":  targetEntry.PurchaseID,
				"warehouse_id": targetEntry.WarehouseId,
				"product_id":   targetEntry.ProductId,
				"created_on":   bson.M{"$gt": targetEntry.CreatedOn},
			}
			opts := options.Find().SetSort(bson.M{"created_on": 1})

			subsequentCursor, err := database.Collection("stock_ledger").Find(sessionCtx, filter, opts)
			if err != nil {
				return nil, err
			}
			defer subsequentCursor.Close(sessionCtx)

			var entries []StockLedgerEntry
			if err = subsequentCursor.All(sessionCtx, &entries); err != nil {
				return nil, err
			}

			// Determine the new opening balance for the first subsequent entry
			// It should be the closing balance of the entry immediately preceding the deleted one
			// Or 0 if there are no preceding entries
			prevFilter := bson.M{
				"origin":       targetEntry.Origin,
				"purchase_id":  targetEntry.PurchaseID,
				"warehouse_id": targetEntry.WarehouseId,
				"product_id":   targetEntry.ProductId,
				"created_on":   bson.M{"$lt": targetEntry.CreatedOn},
			}
			prevOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
			var prevEntry StockLedgerEntry
			var currentOpeningBalance float64 = 0

			err = database.Collection("stock_ledger").FindOne(sessionCtx, prevFilter, prevOpts).Decode(&prevEntry)
			if err == nil {
				currentOpeningBalance = prevEntry.ClosingBalance
			} else if err != mongo.ErrNoDocuments {
				return nil, err
			}

			// Update subsequent entries
			for i := range entries {
				entries[i].OpeningBalance = currentOpeningBalance
				stockDelta := calculateStockDelta(entries[i].TransactionType, entries[i].TransactionBalance)
				entries[i].ClosingBalance = entries[i].OpeningBalance + stockDelta
				currentOpeningBalance = entries[i].ClosingBalance // Set for next iteration

				entries[i].UpdatedBy = userID
				entries[i].UpdatedOn = time.Now()

				update := bson.M{
					"$set": bson.M{
						"opening_balance": entries[i].OpeningBalance,
						"closing_balance": entries[i].ClosingBalance,
						"updated_by":      userID,
						"updated_on":      time.Now(),
					},
				}

				_, err = database.Collection("stock_ledger").UpdateOne(sessionCtx, bson.M{"_id": entries[i].ID}, update)
				if err != nil {
					return nil, err
				}
			}

			// Update Stock In Hand
			// We need to reverse the effect of the deleted entry
			// Calculate what the original delta was
			originalDelta := calculateStockDelta(targetEntry.TransactionType, targetEntry.TransactionBalance)
			// The reversal is the negative of that delta
			quantityDiff := -originalDelta

			if err := updateStockBalance(sessionCtx, organizationID, targetEntry.Origin, targetEntry.StockType, targetEntry.WarehouseId, targetEntry.FactoryId, targetEntry.PurchaseID, targetEntry.ProductId, quantityDiff, userID, "", targetEntry.TransactionType); err != nil {
				return nil, err
			}
		}

		return nil, nil
	})

	return err
}
