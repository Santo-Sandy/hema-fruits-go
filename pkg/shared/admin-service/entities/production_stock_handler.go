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
)

type ProductionStockContext struct {
	OrgID               string
	ProductionID        string
	UserID              string
	PurchaseID          string
	WarehouseID         string
	FactoryID           string
	OriginID            string
	ProcessType         string
	ProcessID           int32
	TemplateID          string
	PreviousProcessType string
	InputData           map[string]interface{}
	TemplateProducts    []bson.M
	Database            *mongo.Database
}

type ProductStockEntry struct {
	ProductID       string
	Quantity        float64
	Expression      int    // -1 = subtract, 0 = ignore, +1 = add
	Type            string // "input" or "output"
	OldQuantity     float64
	StockType       string
	Location        string
	TransactionType string
}

func PostProductionStock(orgID, productionID, userID string, inputData map[string]interface{}) error {
	ctx, err := buildProductionContext(orgID, productionID, userID, inputData)
	if err != nil {
		return fmt.Errorf("failed to build context: %v", err)
	}

	if err := HandleRCNReductionForProduction(ctx.OrgID, ctx.ProcessType, ctx.InputData,
		ctx.ProductionID, ctx.UserID, false); err != nil {
		return fmt.Errorf("RCN reduction failed: %v", err)
	}

	purDoc := map[string]interface{}{"country_origin": ctx.OriginID}
	if ctx.ProcessType == "SHELL" {
		purDoc, _ = GetDataById(ctx.OrgID, ctx.PurchaseID, "purchase")
		if err := HandleShellProcessRCNReduction(ctx.OrgID, ctx.ProcessType, ctx.InputData,
			ctx.TemplateProducts, purDoc, ctx.ProductionID, ctx.UserID, false); err != nil {
			return fmt.Errorf("SHELL process RCN reduction failed: %v", err)
		}
	}

	// Process all products from template
	for _, productConfig := range ctx.TemplateProducts {
		entry, skip, err := buildProductStockEntry(ctx, productConfig, 0.0)
		if err != nil {
			return err
		}
		if skip {
			continue
		}

		if entry.Type == "input" {
			if err := processInputStock(ctx, entry, false); err != nil {
				return err
			}
		} else if entry.Type == "output" {
			if err := processOutputStock(ctx, entry, false); err != nil {
				return err
			}
		}
	}

	return nil
}

func PutProductionStock(orgID, productionID, userID string, inputData map[string]interface{}) error {
	ctx, err := buildProductionContext(orgID, productionID, userID, inputData)
	if err != nil {
		return fmt.Errorf("failed to build context: %v", err)
	}
	oldTemplateID, templateChanged, err := detectTemplateChange(ctx)
	if err != nil {
		return fmt.Errorf("failed to detect template change: %v", err)
	}

	if templateChanged {
		if err := reverseTemplateStock(ctx, oldTemplateID); err != nil {
			return fmt.Errorf("failed to reverse old template: %v", err)
		}
		return PostProductionStock(orgID, productionID, userID, inputData)
	}

	if err := HandleRCNReductionForProduction(ctx.OrgID, ctx.ProcessType, ctx.InputData,
		ctx.ProductionID, ctx.UserID, true); err != nil {
		return fmt.Errorf("RCN reduction failed: %v", err)
	}

	purDoc := map[string]interface{}{"country_origin": ctx.OriginID}
	if ctx.ProcessType == "SHELL" {
		purDoc, _ = GetDataById(ctx.OrgID, ctx.PurchaseID, "purchase")
		if err := HandleShellProcessRCNReduction(ctx.OrgID, ctx.ProcessType, ctx.InputData,
			ctx.TemplateProducts, purDoc, ctx.ProductionID, ctx.UserID, true); err != nil {
			return fmt.Errorf("SHELL process RCN reduction failed: %v", err)
		}
	}

	for _, productConfig := range ctx.TemplateProducts {
		oldQty := getOldQuantityFromLedger(ctx, productConfig)
		entry, skip, err := buildProductStockEntry(ctx, productConfig, oldQty)
		if err != nil {
			return err
		}
		if skip {
			continue
		}

		if entry.Type == "input" {
			if err := processInputStock(ctx, entry, true); err != nil {
				return err
			}
		} else if entry.Type == "output" {
			if err := processOutputStock(ctx, entry, true); err != nil {
				return err
			}
		}
	}

	return nil
}

func DeleteProductionStock(orgID, productionID, userID string) error {
	db := database.GetConnection(orgID)
	ctx := context.Background()

	var production map[string]interface{}
	err := db.Collection("productions").FindOne(ctx, bson.M{"_id": productionID}).Decode(&production)
	if err != nil {
		return fmt.Errorf("production not found: %v", err)
	}

	processType := getString(production["process_type"])

	if processType == "COOK" {
		rcnFilter := bson.M{
			"ref_id":     productionID,
			"product_id": "RCN",
			"stock_type": "RCN",
		}

		var rcnEntries []StockLedgerEntry
		cursor, err := db.Collection("stock_ledger").Find(ctx, rcnFilter)
		if err == nil {
			cursor.All(ctx, &rcnEntries)
			cursor.Close(ctx)

			for _, entry := range rcnEntries {
				// Reverse RCN stock
				reverseAmount := -entry.TransactionBalance
				stockFilter := bson.M{
					"product_id":   entry.ProductId,
					"purchase_id":  entry.PurchaseID,
					"warehouse_id": entry.WarehouseId,
					"factory_id":   entry.FactoryId,
					"origin":       entry.Origin,
					"stock_type":   "RCN",
				}

				stockUpdate := bson.M{
					"$inc": bson.M{"available_qty": reverseAmount},
					"$set": bson.M{
						"last_updated_by": userID,
						"last_updated_on": time.Now(),
					},
				}
				db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)

				updateSubsequentEntries(db, entry.PurchaseID, entry.ProductId, entry.Origin,
					entry.WarehouseId, entry.CreatedOn, -entry.TransactionBalance)
			}
		}

		steamedRCNFilter := bson.M{
			"production_id": productionID,
			"product_id":    "STEAMEDRCN",
			"stock_type":    "WIP",
		}

		var steamedRCNEntries []StockLedgerEntry
		cursor, err = db.Collection("stock_ledger").Find(ctx, steamedRCNFilter)
		if err == nil {
			cursor.All(ctx, &steamedRCNEntries)
			cursor.Close(ctx)

			for _, entry := range steamedRCNEntries {
				reverseAmount := -entry.TransactionBalance
				stockFilter := bson.M{
					"product_id":   entry.ProductId,
					"purchase_id":  entry.PurchaseID,
					"warehouse_id": entry.WarehouseId,
					"factory_id":   entry.FactoryId,
					"origin":       entry.Origin,
					"stock_type":   "WIP",
					"process_type": "COOK",
				}

				stockUpdate := bson.M{
					"$inc": bson.M{"available_qty": reverseAmount},
					"$set": bson.M{
						"last_updated_by": userID,
						"last_updated_on": time.Now(),
					},
				}
				db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)

				updateSubsequentEntries(db, entry.PurchaseID, entry.ProductId, entry.Origin,
					entry.WarehouseId, entry.CreatedOn, -entry.TransactionBalance)
			}
		}

		// Delete RCN and STEAMEDRCN ledger entries
		db.Collection("stock_ledger").DeleteMany(ctx, rcnFilter)
		db.Collection("stock_ledger").DeleteMany(ctx, steamedRCNFilter)
	}

	// Handle STEAMEDRCN deletion for SHELL process
	if processType == "SHELL" {
		steamedRCNFilter := bson.M{
			"ref_id":     productionID,
			"product_id": "STEAMEDRCN",
			"stock_type": "WIP",
		}

		var steamedRCNEntries []StockLedgerEntry
		cursor, err := db.Collection("stock_ledger").Find(ctx, steamedRCNFilter)
		if err == nil {
			cursor.All(ctx, &steamedRCNEntries)
			cursor.Close(ctx)

			for _, entry := range steamedRCNEntries {
				// Reverse STEAMEDRCN stock
				reverseAmount := -entry.TransactionBalance
				stockFilter := bson.M{
					"product_id":   entry.ProductId,
					"purchase_id":  entry.PurchaseID,
					"warehouse_id": entry.WarehouseId,
					"factory_id":   entry.FactoryId,
					"origin":       entry.Origin,
					"stock_type":   "WIP",
					"process_type": "COOK",
				}

				stockUpdate := bson.M{
					"$inc": bson.M{"available_qty": reverseAmount},
					"$set": bson.M{
						"last_updated_by": userID,
						"last_updated_on": time.Now(),
					},
				}
				db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)

				updateSubsequentEntries(db, entry.PurchaseID, entry.ProductId, entry.Origin,
					entry.WarehouseId, entry.CreatedOn, -entry.TransactionBalance)
			}
		}

		// Delete STEAMEDRCN ledger entries
		db.Collection("stock_ledger").DeleteMany(ctx, steamedRCNFilter)
	}

	// Get all other ledger entries for this production
	filter := bson.M{
		"$or": []bson.M{
			{"ref_id": productionID},
			{"production_id": productionID},
		},
	}

	cursor, err := db.Collection("stock_ledger").Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to find ledger entries: %v", err)
	}
	defer cursor.Close(ctx)

	var ledgerEntries []StockLedgerEntry
	if err := cursor.All(ctx, &ledgerEntries); err != nil {
		return fmt.Errorf("failed to decode ledger entries: %v", err)
	}

	for _, entry := range ledgerEntries {
		if entry.ProductId == "RCN" || entry.ProductId == "STEAMEDRCN" {
			continue
		}

		// Reverse stock_in_hand
		reverseAmount := -entry.TransactionBalance
		stockFilter := bson.M{
			"product_id":   entry.ProductId,
			"purchase_id":  entry.PurchaseID,
			"warehouse_id": entry.WarehouseId,
			"factory_id":   entry.FactoryId,
			"origin":       entry.Origin,
		}

		// For WIP stock, use the location (where the stock actually is)
		// For input entries: location is the previous process (where it came from)
		// For output entries: location is the current process (where it was produced)
		if entry.StockType == "WIP" && entry.Location != "" {
			stockFilter["process_type"] = entry.Location
		}

		stockUpdate := bson.M{
			"$inc": bson.M{"available_qty": reverseAmount},
			"$set": bson.M{
				"last_updated_by": userID,
				"last_updated_on": time.Now(),
			},
		}
		db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)

		updateSubsequentEntries(db, entry.PurchaseID, entry.ProductId, entry.Origin,
			entry.WarehouseId, entry.CreatedOn, -entry.TransactionBalance)
	}

	result, err := db.Collection("stock_ledger").DeleteMany(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete ledger entries: %v", err)
	}

	fmt.Printf("Deleted %d ledger entries for production %s\n", result.DeletedCount, productionID)
	return nil
}

func UpdateStockInHand(ctx *ProductionStockContext, entry *ProductStockEntry, diff float64) error {
	filter := bson.M{
		"product_id":   entry.ProductID,
		"purchase_id":  ctx.PurchaseID,
		"warehouse_id": ctx.WarehouseID,
		"factory_id":   ctx.FactoryID,
		"origin":       ctx.OriginID,
	}

	if entry.StockType == "WIP" && entry.Location != "" {
		filter["process_type"] = entry.Location
	}

	update := bson.M{
		"$inc": bson.M{"available_qty": diff},
		"$set": bson.M{
			"last_updated_by": ctx.UserID,
			"last_updated_on": time.Now(),
		},
		"$setOnInsert": bson.M{
			"_id":          uuid.New().String(),
			"product_id":   entry.ProductID,
			"purchase_id":  ctx.PurchaseID,
			"warehouse_id": ctx.WarehouseID,
			"factory_id":   ctx.FactoryID,
			"origin":       ctx.OriginID,
			"stock_type":   entry.StockType,
			"location":     entry.Location,
			"created_by":   ctx.UserID,
			"created_on":   time.Now(),
		},
	}

	if entry.StockType == "WIP" && entry.Location != "" {
		update["$setOnInsert"].(bson.M)["process_type"] = entry.Location
	}

	opts := options.Update().SetUpsert(true)
	_, err := ctx.Database.Collection("stock_in_hand").UpdateOne(context.Background(), filter, update, opts)
	if err != nil {
		return fmt.Errorf("stock_in_hand update failed for %s: %v", entry.ProductID, err)
	}

	return nil
}

func CreateOrUpdateStockLedger(ctx *ProductionStockContext, entry *ProductStockEntry, isUpdate bool) error {
	transactionDate := time.Now()
	if createdOn, ok := ctx.InputData["created_on"]; ok {
		transactionDate = helper.ParseDate(createdOn)
	}

	// Get opening balance
	openingBalance := 0.0
	var existingEntry StockLedgerEntry
	entryExists := false

	ledgerFilter := buildLedgerFilter(ctx, entry)
	err := ctx.Database.Collection("stock_ledger").FindOne(context.Background(), ledgerFilter).Decode(&existingEntry)
	if err == nil {
		entryExists = true
		openingBalance = existingEntry.OpeningBalance
	} else if isUpdate {
		openingBalance = getLastClosingBalance(ctx, entry)
	} else {
		openingBalance = getLastClosingBalance(ctx, entry)
	}

	transactionBalance := entry.Quantity * float64(entry.Expression)
	closingBalance := openingBalance + transactionBalance

	if entryExists {
		// Update existing entry
		update := bson.M{"$set": bson.M{
			"transaction_balance": transactionBalance,
			"opening_balance":     openingBalance,
			"closing_balance":     closingBalance,
			"last_updated_by":     ctx.UserID,
			"last_updated_on":     time.Now(),
			"transaction_date":    transactionDate,
		}}
		_, err := ctx.Database.Collection("stock_ledger").UpdateOne(context.Background(), ledgerFilter, update)
		if err != nil {
			return fmt.Errorf("failed to update ledger: %v", err)
		}

		// Update subsequent entries if balance changed
		if existingEntry.TransactionBalance != transactionBalance {
			balanceChange := transactionBalance - existingEntry.TransactionBalance
			updateSubsequentEntries(ctx.Database, ctx.PurchaseID, entry.ProductID, ctx.OriginID,
				ctx.WarehouseID, existingEntry.CreatedOn, balanceChange)
		}
	} else {
		// Create new entry
		customerName := ""
		if custID, ok := ctx.InputData["customer_id"].(string); ok {
			customerName = custID
		}
		newEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         ctx.PurchaseID,
			Origin:             ctx.OriginID,
			StockType:          entry.StockType,
			WarehouseId:        ctx.WarehouseID,
			ProductId:          entry.ProductID,
			FactoryId:          ctx.FactoryID,
			TransactionType:    entry.TransactionType,
			TransactionDate:    transactionDate,
			CustomerName:       customerName,
			TransactionBalance: transactionBalance,
			OpeningBalance:     openingBalance,
			ClosingBalance:     closingBalance,
			Remarks:            fmt.Sprintf("Production %s", entry.Type),
			CreatedBy:          ctx.UserID,
			CreatedOn:          time.Now(),
			ProcessID:          ctx.ProcessID,
			ProcessType:        ctx.ProcessType,
			Location:           entry.Location,
		}

		if entry.Type == "input" {
			newEntry.RefId = ctx.ProductionID
		} else {
			newEntry.ProductionId = ctx.ProductionID
		}

		_, err := ctx.Database.Collection("stock_ledger").InsertOne(context.Background(), newEntry)
		if err != nil {
			return fmt.Errorf("failed to create ledger: %v", err)
		}
	}

	return nil
}

func buildProductionContext(orgID, productionID, userID string, inputData map[string]interface{}) (*ProductionStockContext, error) {
	db := database.GetConnection(orgID)

	purchaseID := getString(inputData["purchase_id"])
	processType := helper.ToString(inputData["process_type"])
	templateID := getString(inputData["template_id"])
	warehouseID := getString(inputData["warehouse_id"])
	factoryID := getString(inputData["factory_id"])
	processID := helper.ToInt32(inputData["process_id"])

	purDoc, err := GetDataById(orgID, purchaseID, "purchase")
	if err != nil {
		return nil, fmt.Errorf("purchase not found: %v", err)
	}
	originID := getString(purDoc["country_origin"])

	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"template_id", templateID}}}},
	}
	templateProducts, err := helper.GetAggregateQueryResult(orgID, "process_product", pipeline)
	if err != nil || len(templateProducts) == 0 {
		return nil, fmt.Errorf("no template products found for template %s", templateID)
	}

	// Get previous process type
	previousProcessType := getPreviousProcessType(orgID, processType)

	return &ProductionStockContext{
		OrgID:               orgID,
		ProductionID:        productionID,
		UserID:              userID,
		PurchaseID:          purchaseID,
		WarehouseID:         warehouseID,
		FactoryID:           factoryID,
		OriginID:            originID,
		ProcessType:         processType,
		ProcessID:           processID,
		TemplateID:          templateID,
		PreviousProcessType: previousProcessType,
		InputData:           inputData,
		TemplateProducts:    templateProducts,
		Database:            db,
	}, nil
}

func buildProductStockEntry(ctx *ProductionStockContext, productConfig bson.M, oldQty float64) (*ProductStockEntry, bool, error) {
	if ignoreStock, ok := productConfig["ignore_stock"].(bool); ok && ignoreStock {
		return nil, true, nil
	}

	expression := 1
	productId := getString(productConfig["product_id"])
	if exp, ok := productConfig[productId]; ok {
		expression = helper.ToInt(exp)
	}
	if expression == 0 {
		return nil, true, nil
	}

	// Get product ID
	productID := getString(productConfig["product_id"])
	productType := getString(productConfig["type"])

	if productID == "STEAMEDRCN" && ctx.ProcessType == "COOK" {
		return nil, true, nil
	}

	if productID == "STEAMEDRCN" && ctx.ProcessType == "SHELL" {
		return nil, true, nil
	}

	// Get quantity from input data
	qty := helper.ToFloat64(ctx.InputData[productID])

	// Determine stock type and location
	stockType := "WIP"
	location := ctx.ProcessType
	transactionType := "production"

	if productType == "input" {
		actualLocation := findProductLocation(ctx, productID)
		if actualLocation != "" {
			location = actualLocation
			stockType = "WIP"
			transactionType = "production"
		} else if ctx.PreviousProcessType == "" || ctx.PreviousProcessType == "RCN" {
			stockType = "RCN"
			location = ctx.WarehouseID
			transactionType = "purchase"
		} else {
			location = ctx.PreviousProcessType
		}
	}

	return &ProductStockEntry{
		ProductID:       productID,
		Quantity:        qty,
		Expression:      expression,
		Type:            productType,
		OldQuantity:     oldQty,
		StockType:       stockType,
		Location:        location,
		TransactionType: transactionType,
	}, false, nil
}

func processInputStock(ctx *ProductionStockContext, entry *ProductStockEntry, isUpdate bool) error {
	if entry.Quantity == 0 && !isUpdate {
		return nil
	}

	// Calculate diff for stock adjustment
	diff := entry.Quantity * float64(entry.Expression)
	if isUpdate {
		oldDiff := entry.OldQuantity * float64(entry.Expression)
		diff = diff - oldDiff

		// Skip if no change
		if diff == 0 {
			return nil
		}
	}

	// Update stock in hand
	if err := UpdateStockInHand(ctx, entry, diff); err != nil {
		return err
	}

	// Create or update ledger
	if err := CreateOrUpdateStockLedger(ctx, entry, isUpdate); err != nil {
		return err
	}

	return nil
}

func processOutputStock(ctx *ProductionStockContext, entry *ProductStockEntry, isUpdate bool) error {
	if entry.Quantity == 0 && !isUpdate {
		return nil
	}

	// Calculate diff for stock adjustment
	diff := entry.Quantity * float64(entry.Expression)
	if isUpdate {
		oldDiff := entry.OldQuantity * float64(entry.Expression)
		diff = diff - oldDiff

		// Skip if no change
		if diff == 0 {
			return nil
		}
	}

	// Update stock in hand
	if err := UpdateStockInHand(ctx, entry, diff); err != nil {
		return err
	}

	// Create or update ledger
	if err := CreateOrUpdateStockLedger(ctx, entry, isUpdate); err != nil {
		return err
	}

	return nil
}

func detectTemplateChange(ctx *ProductionStockContext) (string, bool, error) {
	var production map[string]interface{}
	err := ctx.Database.Collection("productions").FindOne(context.Background(),
		bson.M{"_id": ctx.ProductionID}).Decode(&production)
	if err != nil {
		return "", false, err
	}

	oldTemplateID := getString(production["template_id"])
	if oldTemplateID != "" && oldTemplateID != ctx.TemplateID {
		return oldTemplateID, true, nil
	}

	return oldTemplateID, false, nil
}

func reverseTemplateStock(ctx *ProductionStockContext, oldTemplateID string) error {
	// Get old template products
	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"template_id", oldTemplateID}}}},
	}
	oldProducts, err := helper.GetAggregateQueryResult(ctx.OrgID, "process_product", pipeline)
	if err != nil {
		return err
	}

	// Reverse each product
	for _, productConfig := range oldProducts {
		productID := getString(productConfig["product_id"])

		filter := bson.M{
			"$or": []bson.M{
				{"ref_id": ctx.ProductionID, "product_id": productID},
				{"production_id": ctx.ProductionID, "product_id": productID},
			},
		}

		var entries []StockLedgerEntry
		cursor, err := ctx.Database.Collection("stock_ledger").Find(context.Background(), filter)
		if err != nil {
			continue
		}
		cursor.All(context.Background(), &entries)
		cursor.Close(context.Background())

		for _, entry := range entries {
			// Reverse stock_in_hand
			reverseAmount := -entry.TransactionBalance
			stockFilter := bson.M{
				"product_id":   entry.ProductId,
				"purchase_id":  entry.PurchaseID,
				"warehouse_id": entry.WarehouseId,
				"factory_id":   entry.FactoryId,
				"origin":       entry.Origin,
			}
			if entry.StockType == "WIP" && entry.Location != "" {
				stockFilter["process_type"] = entry.Location
			}

			stockUpdate := bson.M{
				"$inc": bson.M{"available_qty": reverseAmount},
			}
			ctx.Database.Collection("stock_in_hand").UpdateOne(context.Background(), stockFilter, stockUpdate)

			// Update subsequent entries
			updateSubsequentEntries(ctx.Database, entry.PurchaseID, entry.ProductId, entry.Origin,
				entry.WarehouseId, entry.CreatedOn, -entry.TransactionBalance)
		}

		// Delete ledger entries
		ctx.Database.Collection("stock_ledger").DeleteMany(context.Background(), filter)
	}

	return nil
}

func getOldQuantityFromLedger(ctx *ProductionStockContext, productConfig bson.M) float64 {
	productID := getString(productConfig["product_id"])
	productType := getString(productConfig["type"])

	ledgerFilter := bson.M{
		"purchase_id":  ctx.PurchaseID,
		"product_id":   productID,
		"origin":       ctx.OriginID,
		"warehouse_id": ctx.WarehouseID,
	}

	if productType == "input" {
		ledgerFilter["ref_id"] = ctx.ProductionID
		if ctx.PreviousProcessType == "" || ctx.PreviousProcessType == "RCN" {
			ledgerFilter["transaction_type"] = "purchase"
		} else {
			ledgerFilter["transaction_type"] = "production"
		}
	} else {
		ledgerFilter["production_id"] = ctx.ProductionID
		ledgerFilter["transaction_type"] = "production"
	}

	var entry StockLedgerEntry
	err := ctx.Database.Collection("stock_ledger").FindOne(context.Background(), ledgerFilter).Decode(&entry)
	if err != nil {
		return 0.0
	}

	// Get expression to reverse the sign
	productId := getString(productConfig["product_id"])
	expression := 1
	if exp, ok := productConfig[productId]; ok {
		expression = helper.ToInt(exp)
	}

	// Return absolute quantity (reverse the expression effect)
	if expression != 0 {
		return entry.TransactionBalance / float64(expression)
	}
	return 0.0
}

func getLastClosingBalance(ctx *ProductionStockContext, entry *ProductStockEntry) float64 {
	ledgerFilter := bson.M{
		"product_id":   entry.ProductID,
		"purchase_id":  ctx.PurchaseID,
		"warehouse_id": ctx.WarehouseID,
		"origin":       ctx.OriginID,
		"location":     entry.Location,
	}

	// Exclude current production
	if entry.Type == "input" {
		ledgerFilter["ref_id"] = bson.M{"$ne": ctx.ProductionID}
	} else {
		ledgerFilter["production_id"] = bson.M{"$ne": ctx.ProductionID}
	}

	var lastEntry StockLedgerEntry
	opts := options.FindOne().SetSort(bson.M{"created_on": -1})
	err := ctx.Database.Collection("stock_ledger").FindOne(context.Background(), ledgerFilter, opts).Decode(&lastEntry)
	if err == nil {
		return lastEntry.ClosingBalance
	}
	return 0.0
}

func buildLedgerFilter(ctx *ProductionStockContext, entry *ProductStockEntry) bson.M {
	filter := bson.M{
		"purchase_id":      ctx.PurchaseID,
		"product_id":       entry.ProductID,
		"origin":           ctx.OriginID,
		"warehouse_id":     ctx.WarehouseID,
		"transaction_type": entry.TransactionType,
	}

	if entry.Type == "input" {
		filter["ref_id"] = ctx.ProductionID
	} else {
		filter["production_id"] = ctx.ProductionID
	}

	return filter
}

func getPreviousProcessType(orgID, processType string) string {
	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", processType}}}},
		bson.D{
			{"$set",
				bson.D{
					{"previous_process_id",
						bson.D{
							{"$subtract",
								bson.A{"$process_id", 1},
							},
						},
					},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "process"},
					{"localField", "previous_process_id"},
					{"foreignField", "process_id"},
					{"as", "previous_process_result"},
				},
			},
		},
		bson.D{
			{"$unwind",
				bson.D{
					{"path", "$previous_process_result"},
					{"preserveNullAndEmptyArrays", false},
				},
			},
		},
		bson.D{{"$set", bson.D{{"previous_process_type", "$previous_process_result._id"}}}},
	}

	processData, err := helper.GetAggregateQueryResult(orgID, "process", pipeline)
	if err == nil && len(processData) != 0 {
		return getString(processData[0]["previous_process_type"])
	}
	return ""
}

func findProductLocation(ctx *ProductionStockContext, productID string) string {
	filter := bson.M{
		"product_id":    productID,
		"purchase_id":   ctx.PurchaseID,
		"warehouse_id":  ctx.WarehouseID,
		"factory_id":    ctx.FactoryID,
		"origin":        ctx.OriginID,
		"stock_type":    "WIP",
		"available_qty": bson.M{"$gt": 0},
	}

	opts := options.FindOne().SetSort(bson.M{"created_on": -1})

	var stockEntry map[string]interface{}
	err := ctx.Database.Collection("stock_in_hand").FindOne(context.Background(), filter, opts).Decode(&stockEntry)
	if err == nil {
		if processType, ok := stockEntry["process_type"].(string); ok && processType != "" {
			return processType
		}
		if location, ok := stockEntry["location"].(string); ok && location != "" {
			return location
		}
	}

	return ""
}
