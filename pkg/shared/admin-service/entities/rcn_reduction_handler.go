package entities

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

func HandleRCNReductionForProduction(orgID, processType string, inputData map[string]interface{},
	productionID, userID string, isUpdate bool) error {

	if processType != "COOK" {
		return nil
	}

	db := database.GetConnection(orgID)
	purchaseID := getString(inputData["purchase_id"])
	warehouseID := getString(inputData["warehouse_id"])
	factoryID := getString(inputData["factory_id"])

	// Get origin from purchase
	purDoc, err := GetDataById(orgID, purchaseID, "purchase")
	if err != nil {
		return fmt.Errorf("purchase not found: %v", err)
	}
	originID := getString(purDoc["country_origin"])

	// Get STEAMEDRCN output quantity from input data
	steamedRCNQty := helper.ToFloat64(inputData["STEAMEDRCN"])

	if steamedRCNQty == 0 && !isUpdate {
		return nil // No STEAMEDRCN produced, nothing to reduce
	}

	// RCN reduction logic
	if isUpdate {
		return updateRCNReduction(db, purchaseID, warehouseID, factoryID, originID,
			productionID, userID, steamedRCNQty, inputData)
	} else {
		return createRCNReduction(db, purchaseID, warehouseID, factoryID, originID,
			productionID, userID, steamedRCNQty, inputData)
	}
}

func createRCNReduction(db *mongo.Database, purchaseID, warehouseID, factoryID, originID,
	productionID, userID string, steamedRCNQty float64, inputData map[string]interface{}) error {

	ctx := context.Background()
	rcnProductID := "RCN"
	steamedRCNProductID := "STEAMEDRCN"

	rcnConsumption := -steamedRCNQty

	rcnStockFilter := bson.M{
		"product_id":   rcnProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"factory_id":   factoryID,
		"origin":       originID,
		"stock_type":   "RCN",
	}

	rcnStockUpdate := bson.M{
		"$inc": bson.M{"available_qty": rcnConsumption},
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
	}

	_, err := db.Collection("stock_in_hand").UpdateOne(ctx, rcnStockFilter, rcnStockUpdate)
	if err != nil {
		return fmt.Errorf("failed to update RCN stock_in_hand: %v", err)
	}

	steamedRCNStockFilter := bson.M{
		"product_id":   steamedRCNProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"factory_id":   factoryID,
		"origin":       originID,
		"stock_type":   "WIP",
		"process_type": "COOK",
	}

	steamedRCNStockUpdate := bson.M{
		"$inc": bson.M{"available_qty": steamedRCNQty},
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
		"$setOnInsert": bson.M{
			"_id":        uuid.New().String(),
			"created_by": userID,
			"created_on": time.Now(),
			"location":   "COOK",
			"type":       "output",
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err = db.Collection("stock_in_hand").UpdateOne(ctx, steamedRCNStockFilter, steamedRCNStockUpdate, opts)
	if err != nil {
		return fmt.Errorf("failed to update STEAMEDRCN stock_in_hand: %v", err)
	}

	transactionDate := time.Now()
	if createdOn, ok := inputData["created_on"]; ok {
		transactionDate = helper.ParseDate(createdOn)
	}

	var lastRCNLedgerEntry StockLedgerEntry
	rcnLedgerFilter := bson.M{
		"product_id":   rcnProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"origin":       originID,
		"stock_type":   "RCN",
	}

	ledgerOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
	err = db.Collection("stock_ledger").FindOne(ctx, rcnLedgerFilter, ledgerOpts).Decode(&lastRCNLedgerEntry)

	rcnOpeningBalance := 0.0
	if err == nil {
		rcnOpeningBalance = lastRCNLedgerEntry.ClosingBalance
	}

	rcnClosingBalance := rcnOpeningBalance + rcnConsumption

	rcnLedgerEntry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         purchaseID,
		Origin:             originID,
		StockType:          "RCN",
		WarehouseId:        warehouseID,
		ProductId:          rcnProductID,
		FactoryId:          factoryID,
		TransactionType:    "production",
		TransactionDate:    transactionDate,
		TransactionBalance: rcnConsumption,
		OpeningBalance:     rcnOpeningBalance,
		ClosingBalance:     rcnClosingBalance,
		Remarks:            "RCN consumption for STEAMEDRCN production",
		CreatedBy:          userID,
		CreatedOn:          time.Now(),
		ProcessType:        "COOK",
		Location:           warehouseID,
		RefId:              productionID,
	}

	_, err = db.Collection("stock_ledger").InsertOne(ctx, rcnLedgerEntry)
	if err != nil {
		return fmt.Errorf("failed to create RCN ledger entry: %v", err)
	}

	var lastSteamedRCNLedgerEntry StockLedgerEntry
	steamedRCNLedgerFilter := bson.M{
		"product_id":   steamedRCNProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"origin":       originID,
		"stock_type":   "WIP",
		"location":     "COOK",
	}

	err = db.Collection("stock_ledger").FindOne(ctx, steamedRCNLedgerFilter, ledgerOpts).Decode(&lastSteamedRCNLedgerEntry)

	steamedRCNOpeningBalance := 0.0
	if err == nil {
		steamedRCNOpeningBalance = lastSteamedRCNLedgerEntry.ClosingBalance
	}

	steamedRCNClosingBalance := steamedRCNOpeningBalance + steamedRCNQty

	steamedRCNLedgerEntry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         purchaseID,
		Origin:             originID,
		StockType:          "WIP",
		WarehouseId:        warehouseID,
		ProductId:          steamedRCNProductID,
		FactoryId:          factoryID,
		ProductionId:       productionID,
		TransactionType:    "production",
		TransactionDate:    transactionDate,
		TransactionBalance: steamedRCNQty,
		OpeningBalance:     steamedRCNOpeningBalance,
		ClosingBalance:     steamedRCNClosingBalance,
		Remarks:            "STEAMEDRCN production output",
		CreatedBy:          userID,
		CreatedOn:          time.Now(),
		ProcessType:        "COOK",
		Location:           "COOK",
	}

	_, err = db.Collection("stock_ledger").InsertOne(ctx, steamedRCNLedgerEntry)
	if err != nil {
		return fmt.Errorf("failed to create STEAMEDRCN ledger entry: %v", err)
	}

	return nil
}

func updateRCNReduction(db *mongo.Database, purchaseID, warehouseID, factoryID, originID,
	productionID, userID string, newSteamedRCNQty float64, inputData map[string]interface{}) error {

	ctx := context.Background()
	rcnProductID := "RCN"
	steamedRCNProductID := "STEAMEDRCN"

	rcnLedgerQuery := bson.M{
		"purchase_id":      purchaseID,
		"product_id":       rcnProductID,
		"origin":           originID,
		"warehouse_id":     warehouseID,
		"ref_id":           productionID,
		"transaction_type": "production",
		"stock_type":       "RCN",
	}

	var existingRCNEntry StockLedgerEntry
	err := db.Collection("stock_ledger").FindOne(ctx, rcnLedgerQuery).Decode(&existingRCNEntry)

	oldRCNConsumption := 0.0
	rcnEntryExists := false

	if err == nil {
		rcnEntryExists = true
		oldRCNConsumption = existingRCNEntry.TransactionBalance
	}

	// Find existing STEAMEDRCN ledger entry
	steamedRCNLedgerQuery := bson.M{
		"purchase_id":      purchaseID,
		"product_id":       steamedRCNProductID,
		"origin":           originID,
		"warehouse_id":     warehouseID,
		"production_id":    productionID,
		"transaction_type": "production",
		"stock_type":       "WIP",
	}

	var existingSteamedRCNEntry StockLedgerEntry
	err = db.Collection("stock_ledger").FindOne(ctx, steamedRCNLedgerQuery).Decode(&existingSteamedRCNEntry)

	oldSteamedRCNQty := 0.0
	steamedRCNEntryExists := false

	if err == nil {
		steamedRCNEntryExists = true
		oldSteamedRCNQty = existingSteamedRCNEntry.TransactionBalance
	}

	newRCNConsumption := -newSteamedRCNQty

	if newSteamedRCNQty == 0 {
		if rcnEntryExists {
			rcnStockDiff := -oldRCNConsumption

			rcnStockFilter := bson.M{
				"product_id":   rcnProductID,
				"purchase_id":  purchaseID,
				"warehouse_id": warehouseID,
				"factory_id":   factoryID,
				"origin":       originID,
				"stock_type":   "RCN",
			}

			rcnStockUpdate := bson.M{
				"$inc": bson.M{"available_qty": rcnStockDiff},
				"$set": bson.M{
					"last_updated_by": userID,
					"last_updated_on": time.Now(),
				},
			}

			db.Collection("stock_in_hand").UpdateOne(ctx, rcnStockFilter, rcnStockUpdate)

			db.Collection("stock_ledger").DeleteOne(ctx, rcnLedgerQuery)

			updateSubsequentEntries(db, purchaseID, rcnProductID, originID, warehouseID,
				existingRCNEntry.CreatedOn, -oldRCNConsumption)
		}

		if steamedRCNEntryExists {
			steamedRCNStockDiff := -oldSteamedRCNQty

			steamedRCNStockFilter := bson.M{
				"product_id":   steamedRCNProductID,
				"purchase_id":  purchaseID,
				"warehouse_id": warehouseID,
				"factory_id":   factoryID,
				"origin":       originID,
				"stock_type":   "WIP",
				"process_type": "COOK",
			}

			steamedRCNStockUpdate := bson.M{
				"$inc": bson.M{"available_qty": steamedRCNStockDiff},
				"$set": bson.M{
					"last_updated_by": userID,
					"last_updated_on": time.Now(),
				},
			}

			db.Collection("stock_in_hand").UpdateOne(ctx, steamedRCNStockFilter, steamedRCNStockUpdate)

			db.Collection("stock_ledger").DeleteOne(ctx, steamedRCNLedgerQuery)
			updateSubsequentEntries(db, purchaseID, steamedRCNProductID, originID, warehouseID,
				existingSteamedRCNEntry.CreatedOn, -oldSteamedRCNQty)
		}

		return nil
	}

	// Calculate the differences
	rcnStockDiff := newRCNConsumption - oldRCNConsumption
	steamedRCNStockDiff := newSteamedRCNQty - oldSteamedRCNQty

	if rcnStockDiff == 0 && steamedRCNStockDiff == 0 && rcnEntryExists && steamedRCNEntryExists {
		return nil
	}
	transactionDate := time.Now()
	if createdOn, ok := inputData["created_on"]; ok {
		transactionDate = helper.ParseDate(createdOn)
	}

	rcnStockFilter := bson.M{
		"product_id":   rcnProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"factory_id":   factoryID,
		"origin":       originID,
		"stock_type":   "RCN",
	}

	rcnStockUpdate := bson.M{
		"$inc": bson.M{"available_qty": rcnStockDiff},
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
	}

	_, err = db.Collection("stock_in_hand").UpdateOne(ctx, rcnStockFilter, rcnStockUpdate)
	if err != nil {
		return fmt.Errorf("failed to update RCN stock_in_hand: %v", err)
	}

	steamedRCNStockFilter := bson.M{
		"product_id":   steamedRCNProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"factory_id":   factoryID,
		"origin":       originID,
		"stock_type":   "WIP",
		"process_type": "COOK",
	}

	steamedRCNStockUpdate := bson.M{
		"$inc": bson.M{"available_qty": steamedRCNStockDiff},
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
		"$setOnInsert": bson.M{
			"_id":        uuid.New().String(),
			"created_by": userID,
			"created_on": time.Now(),
			"location":   "COOK",
			"type":       "output",
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err = db.Collection("stock_in_hand").UpdateOne(ctx, steamedRCNStockFilter, steamedRCNStockUpdate, opts)
	if err != nil {
		return fmt.Errorf("failed to update STEAMEDRCN stock_in_hand: %v", err)
	}

	// Update or create RCN ledger entry
	if rcnEntryExists {
		rcnOpeningBalance := existingRCNEntry.OpeningBalance
		rcnClosingBalance := rcnOpeningBalance + newRCNConsumption

		update := bson.M{"$set": bson.M{
			"transaction_balance": newRCNConsumption,
			"closing_balance":     rcnClosingBalance,
			"last_updated_by":     userID,
			"last_updated_on":     time.Now(),
			"transaction_date":    transactionDate,
		}}

		_, err = db.Collection("stock_ledger").UpdateOne(ctx, rcnLedgerQuery, update)
		if err != nil {
			return fmt.Errorf("failed to update RCN ledger entry: %v", err)
		}

		// Update subsequent RCN entries
		balanceChange := newRCNConsumption - oldRCNConsumption
		updateSubsequentEntries(db, purchaseID, rcnProductID, originID, warehouseID,
			existingRCNEntry.CreatedOn, balanceChange)

	} else {
		// Create new RCN ledger entry
		var lastRCNLedgerEntry StockLedgerEntry
		rcnLedgerFilter := bson.M{
			"product_id":   rcnProductID,
			"purchase_id":  purchaseID,
			"warehouse_id": warehouseID,
			"origin":       originID,
			"stock_type":   "RCN",
			"ref_id":       bson.M{"$ne": productionID},
		}

		ledgerOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
		err = db.Collection("stock_ledger").FindOne(ctx, rcnLedgerFilter, ledgerOpts).Decode(&lastRCNLedgerEntry)

		rcnOpeningBalance := 0.0
		if err == nil {
			rcnOpeningBalance = lastRCNLedgerEntry.ClosingBalance
		}

		rcnClosingBalance := rcnOpeningBalance + newRCNConsumption

		rcnLedgerEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         purchaseID,
			Origin:             originID,
			StockType:          "RCN",
			WarehouseId:        warehouseID,
			ProductId:          rcnProductID,
			FactoryId:          factoryID,
			TransactionType:    "production",
			TransactionDate:    transactionDate,
			TransactionBalance: newRCNConsumption,
			OpeningBalance:     rcnOpeningBalance,
			ClosingBalance:     rcnClosingBalance,
			Remarks:            "RCN consumption for STEAMEDRCN production",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
			ProcessType:        "COOK",
			Location:           warehouseID,
			RefId:              productionID,
		}

		_, err = db.Collection("stock_ledger").InsertOne(ctx, rcnLedgerEntry)
		if err != nil {
			return fmt.Errorf("failed to create RCN ledger entry: %v", err)
		}
	}

	if steamedRCNEntryExists {
		steamedRCNOpeningBalance := existingSteamedRCNEntry.OpeningBalance
		steamedRCNClosingBalance := steamedRCNOpeningBalance + newSteamedRCNQty

		update := bson.M{"$set": bson.M{
			"transaction_balance": newSteamedRCNQty,
			"closing_balance":     steamedRCNClosingBalance,
			"last_updated_by":     userID,
			"last_updated_on":     time.Now(),
			"transaction_date":    transactionDate,
		}}

		_, err = db.Collection("stock_ledger").UpdateOne(ctx, steamedRCNLedgerQuery, update)
		if err != nil {
			return fmt.Errorf("failed to update STEAMEDRCN ledger entry: %v", err)
		}

		// Update subsequent STEAMEDRCN entries
		balanceChange := newSteamedRCNQty - oldSteamedRCNQty
		updateSubsequentEntries(db, purchaseID, steamedRCNProductID, originID, warehouseID,
			existingSteamedRCNEntry.CreatedOn, balanceChange)

	} else {
		// Create new STEAMEDRCN ledger entry
		var lastSteamedRCNLedgerEntry StockLedgerEntry
		steamedRCNLedgerFilter := bson.M{
			"product_id":    steamedRCNProductID,
			"purchase_id":   purchaseID,
			"warehouse_id":  warehouseID,
			"origin":        originID,
			"stock_type":    "WIP",
			"location":      "COOK",
			"production_id": bson.M{"$ne": productionID},
		}

		ledgerOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
		err = db.Collection("stock_ledger").FindOne(ctx, steamedRCNLedgerFilter, ledgerOpts).Decode(&lastSteamedRCNLedgerEntry)

		steamedRCNOpeningBalance := 0.0
		if err == nil {
			steamedRCNOpeningBalance = lastSteamedRCNLedgerEntry.ClosingBalance
		}

		steamedRCNClosingBalance := steamedRCNOpeningBalance + newSteamedRCNQty

		steamedRCNLedgerEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         purchaseID,
			Origin:             originID,
			StockType:          "WIP",
			WarehouseId:        warehouseID,
			ProductId:          steamedRCNProductID,
			FactoryId:          factoryID,
			ProductionId:       productionID,
			TransactionType:    "production",
			TransactionDate:    transactionDate,
			TransactionBalance: newSteamedRCNQty,
			OpeningBalance:     steamedRCNOpeningBalance,
			ClosingBalance:     steamedRCNClosingBalance,
			Remarks:            "STEAMEDRCN production output",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
			ProcessType:        "COOK",
			Location:           "COOK",
		}

		_, err = db.Collection("stock_ledger").InsertOne(ctx, steamedRCNLedgerEntry)
		if err != nil {
			return fmt.Errorf("failed to create STEAMEDRCN ledger entry: %v", err)
		}
	}

	return nil
}

func HandleShellProcessRCNReduction(orgID, processType string, inputData map[string]interface{},
	templateProducts []bson.M, purDoc map[string]interface{}, productionID, userID string, isUpdate bool) error {

	if processType != "SHELL" {
		return nil
	}

	db := database.GetConnection(orgID)
	purchaseID := getString(inputData["purchase_id"])
	warehouseID := getString(inputData["warehouse_id"])
	factoryID := getString(inputData["factory_id"])
	originID := getString(purDoc["country_origin"])
	steamedRCNProductID := "STEAMEDRCN"

	kernelOutput := 0.0
	for _, product := range templateProducts {
		if getString(product["type"]) == "output" {
			prodID := getString(product["product_id"])
			kernelOutput += helper.ToFloat64(inputData[prodID])
		}
	}

	purchaseOutTurn := helper.ToFloat64(purDoc["purchase_out_turn"])
	outTurnLbs := utils.GetenvFloat("OUT_TURN_LBS")

	if purchaseOutTurn <= 0 || outTurnLbs <= 0 || kernelOutput <= 0 {
		if isUpdate {
			db.Collection("productions").UpdateOne(
				context.Background(),
				bson.M{"_id": productionID},
				bson.M{"$set": bson.M{"input_weight": 0}})
		}
		return nil
	}

	steamedRCNQty := (kernelOutput * outTurnLbs) / purchaseOutTurn

	inputData["input_weight"] = steamedRCNQty
	db.Collection("productions").UpdateOne(
		context.Background(),
		bson.M{"_id": productionID},
		bson.M{"$set": bson.M{"input_weight": steamedRCNQty}})

	ctx := context.Background()
	steamedRCNConsumption := -steamedRCNQty

	ledgerQuery := bson.M{
		"purchase_id":      purchaseID,
		"product_id":       steamedRCNProductID,
		"origin":           originID,
		"warehouse_id":     warehouseID,
		"ref_id":           productionID,
		"transaction_type": "production",
		"stock_type":       "WIP",
	}

	var existingEntry StockLedgerEntry
	err := db.Collection("stock_ledger").FindOne(ctx, ledgerQuery).Decode(&existingEntry)

	oldConsumption := 0.0
	entryExists := false

	if err == nil {
		entryExists = true
		oldConsumption = existingEntry.TransactionBalance
	}

	if steamedRCNQty == 0 && entryExists {
		stockDiff := -oldConsumption

		stockFilter := bson.M{
			"product_id":   steamedRCNProductID,
			"purchase_id":  purchaseID,
			"warehouse_id": warehouseID,
			"factory_id":   factoryID,
			"origin":       originID,
			"stock_type":   "WIP",
			"process_type": "COOK",
		}

		stockUpdate := bson.M{
			"$inc": bson.M{"available_qty": stockDiff},
			"$set": bson.M{
				"last_updated_by": userID,
				"last_updated_on": time.Now(),
			},
		}

		db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)

		// Delete ledger entry
		db.Collection("stock_ledger").DeleteOne(ctx, ledgerQuery)

		// Update subsequent entries
		updateSubsequentEntries(db, purchaseID, steamedRCNProductID, originID, warehouseID,
			existingEntry.CreatedOn, -oldConsumption)

		return nil
	}

	// Calculate the difference
	stockDiff := steamedRCNConsumption - oldConsumption

	// Skip if no change
	if stockDiff == 0 && entryExists {
		return nil
	}

	// Update STEAMEDRCN stock_in_hand
	stockFilter := bson.M{
		"product_id":   steamedRCNProductID,
		"purchase_id":  purchaseID,
		"warehouse_id": warehouseID,
		"factory_id":   factoryID,
		"origin":       originID,
		"stock_type":   "WIP",
		"process_type": "COOK",
	}

	stockUpdate := bson.M{
		"$inc": bson.M{"available_qty": stockDiff},
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
	}

	_, err = db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)
	if err != nil {
		return fmt.Errorf("failed to update STEAMEDRCN stock_in_hand: %v", err)
	}

	// Get transaction date
	transactionDate := time.Now()
	if createdOn, ok := inputData["created_on"]; ok {
		transactionDate = helper.ParseDate(createdOn)
	}

	if entryExists {
		// Update existing ledger entry
		openingBalance := existingEntry.OpeningBalance
		closingBalance := openingBalance + steamedRCNConsumption

		update := bson.M{"$set": bson.M{
			"transaction_balance": steamedRCNConsumption,
			"closing_balance":     closingBalance,
			"last_updated_by":     userID,
			"last_updated_on":     time.Now(),
			"transaction_date":    transactionDate,
		}}

		_, err = db.Collection("stock_ledger").UpdateOne(ctx, ledgerQuery, update)
		if err != nil {
			return fmt.Errorf("failed to update STEAMEDRCN ledger entry: %v", err)
		}

		// Update subsequent entries
		balanceChange := steamedRCNConsumption - oldConsumption
		updateSubsequentEntries(db, purchaseID, steamedRCNProductID, originID, warehouseID,
			existingEntry.CreatedOn, balanceChange)

	} else {
		// Create new STEAMEDRCN consumption ledger entry
		var lastLedgerEntry StockLedgerEntry
		ledgerFilter := bson.M{
			"product_id":   steamedRCNProductID,
			"purchase_id":  purchaseID,
			"warehouse_id": warehouseID,
			"origin":       originID,
			"stock_type":   "WIP",
			"location":     "COOK",
			"ref_id":       bson.M{"$ne": productionID},
		}

		ledgerOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
		err = db.Collection("stock_ledger").FindOne(ctx, ledgerFilter, ledgerOpts).Decode(&lastLedgerEntry)

		openingBalance := 0.0
		if err == nil {
			openingBalance = lastLedgerEntry.ClosingBalance
		}

		closingBalance := openingBalance + steamedRCNConsumption

		ledgerEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         purchaseID,
			Origin:             originID,
			StockType:          "WIP",
			WarehouseId:        warehouseID,
			ProductId:          steamedRCNProductID,
			FactoryId:          factoryID,
			TransactionType:    "production",
			TransactionDate:    transactionDate,
			TransactionBalance: steamedRCNConsumption,
			OpeningBalance:     openingBalance,
			ClosingBalance:     closingBalance,
			Remarks:            "STEAMEDRCN consumption for SHELL process",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
			ProcessType:        "SHELL",
			Location:           "COOK",
			RefId:              productionID,
		}

		_, err = db.Collection("stock_ledger").InsertOne(ctx, ledgerEntry)
		if err != nil {
			return fmt.Errorf("failed to create STEAMEDRCN ledger entry: %v", err)
		}
	}

	return nil
}
