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

func handleSteamedRCNReduction(orgId string, process_type string, inputData map[string]interface{}, templateProducts []bson.M, purDoc map[string]interface{}, productionId string, userID string, previousProcessType string, isUpdate bool) error {
	// Only apply for SHELL process
	if process_type != "SHELL" {
		return nil
	}

	database := database.GetConnection(orgId)
	purchaseId := inputData["purchase_id"].(string)
	warehouseId := getString(inputData["warehouse_id"])
	factoryId := inputData["factory_id"].(string)
	originId := purDoc["country_origin"].(string)
	productId := "STEAMEDRCN"

	// Calculate total kernel output (sum of all shell output fields)
	kernelOutput := 0.0
	for _, product := range templateProducts {
		if getString(product["type"]) == "output" {
			prodId := getString(product["product_id"])
			kernelOutput += helper.ToFloat64(inputData[prodId])
		}
	}

	// Get purchase_out_turn from purchase table
	purchaseOutTurn := helper.ToFloat64(purDoc["purchase_out_turn"])

	// Get OUT_TURN_LBS from environment
	outTurnLbs := utils.GetenvFloat("OUT_TURN_LBS")

	// If kernel output is 0, handle it specially for UPDATE mode
	if kernelOutput == 0 && isUpdate {

		// Check if ledger entry exists
		inputQuery := bson.M{
			"purchase_id":      purchaseId,
			"product_id":       productId,
			"origin":           originId,
			"warehouse_id":     warehouseId,
			"ref_id":           productionId,
			"transaction_type": "production",
		}

		var currentEntry StockLedgerEntry
		err := database.Collection("stock_ledger").FindOne(context.Background(), inputQuery).Decode(&currentEntry)

		if err == nil {
			// Entry exists, update it to 0
			oldQty := currentEntry.TransactionBalance
			stockDiff := -oldQty // Reverse the old quantity

			// Get stock filter
			stockFilter := bson.M{
				"product_id":   productId,
				"purchase_id":  purchaseId,
				"factory_id":   factoryId,
				"warehouse_id": warehouseId,
				"origin":       originId,
			}

			// Get previous process type for filter
			processPipeline := bson.A{
				bson.D{{"$match", bson.D{{"_id", process_type}}}},
				bson.D{
					{"$set",
						bson.D{
							{"previous_process_id",
								bson.D{
									{"$subtract",
										bson.A{
											"$process_id",
											1,
										},
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

			processData, err := helper.GetAggregateQueryResult(orgId, "process", processPipeline)
			if err == nil && len(processData) != 0 {
				prevProcessType := processData[0]["previous_process_type"].(string)
				if prevProcessType != "" && prevProcessType != "RCN" {
					stockFilter["process_type"] = prevProcessType
				}
			}

			// Update stock_in_hand
			stockUpdate := bson.M{
				"$inc": bson.M{"available_qty": stockDiff},
				"$set": bson.M{
					"last_updated_by": userID,
					"last_updated_on": time.Now(),
				},
			}
			database.Collection("stock_in_hand").UpdateOne(context.Background(), stockFilter, stockUpdate)

			// Update ledger entry to 0 (keep the entry, don't delete)
			consumed_openingBalance := currentEntry.OpeningBalance
			consumed_closingBalance := consumed_openingBalance // closing = opening when transaction is 0

			update := bson.M{"$set": bson.M{
				"transaction_balance": 0,
				"opening_balance":     consumed_openingBalance,
				"closing_balance":     consumed_closingBalance,
				"last_updated_by":     userID,
				"last_updated_on":     time.Now(),
			}}
			database.Collection("stock_ledger").UpdateOne(context.Background(), inputQuery, update)

			// Update subsequent entries - reduce by the old quantity
			balanceChange := -oldQty // This will reduce subsequent balances
			updateSubsequentEntries(database, purchaseId, productId, originId, warehouseId, currentEntry.CreatedOn, balanceChange)
			inputData["input_weight"] = 0
			// Update production table to set input_weight to 0
			database.Collection("productions").UpdateOne(
				context.Background(),
				bson.M{"_id": productionId},
				bson.M{"$set": bson.M{"input_weight": 0}})

		} else {
			// No entry exists, just update input_weight to 0
			database.Collection("productions").UpdateOne(
				context.Background(),
				bson.M{"_id": productionId},
				bson.M{"$set": bson.M{"input_weight": 0}})
		}

		return nil
	}

	// Calculate RCN reduce: (kernel output * OUT_TURN_LBS) / purchase_out_turn
	if purchaseOutTurn <= 0 || outTurnLbs <= 0 || kernelOutput <= 0 {
		// Update input_weight to 0 if calculation not possible
		if isUpdate {
			database.Collection("productions").UpdateOne(
				context.Background(),
				bson.M{"_id": productionId},
				bson.M{"$set": bson.M{"input_weight": 0}})
		}
		return nil // Skip if calculation not possible
	}

	qty := (kernelOutput * outTurnLbs) / purchaseOutTurn

	// Store the calculated RCN reduce value in inputData
	inputData["input_weight"] = qty

	// Upsert input_weight in production table
	updateOpts := options.Update().SetUpsert(false)
	updateResult, prodErr := database.Collection("productions").UpdateOne(
		context.Background(),
		bson.M{"_id": productionId},
		bson.M{
			"$set": bson.M{
				"input_weight": qty,
			},
		},
		updateOpts)
	if prodErr != nil {
		fmt.Printf("ERROR: Failed to update input_weight in productions collection for productionId %s: %v\n", productionId, prodErr)
		return fmt.Errorf("failed to update input_weight: %v", prodErr)
	}
	if updateResult.MatchedCount == 0 {
		fmt.Printf("WARNING: Production document not found for productionId %s\n", productionId)
	}

	// Handle stock reduction based on whether this is create or update
	if isUpdate {
		// UPDATE logic
		newQty := qty
		diff := newQty * -1

		// Get stock filter - STEAMEDRCN is WIP from COOKING process
		stockFilter := bson.M{
			"product_id":   productId,
			"purchase_id":  purchaseId,
			"factory_id":   factoryId,
			"warehouse_id": warehouseId,
			"origin":       originId,
			"process_type": previousProcessType, // COOKING
		}

		// Check if ledger entry exists
		inputQuery := bson.M{
			"purchase_id":      purchaseId,
			"product_id":       productId,
			"origin":           originId,
			"warehouse_id":     warehouseId,
			"ref_id":           productionId,
			"transaction_type": "production", // WIP consumption uses production type
		}

		var currentEntry StockLedgerEntry
		err := database.Collection("stock_ledger").FindOne(context.Background(), inputQuery).Decode(&currentEntry)
		oldQty := 0.0
		entryExists := false
		if err == nil {
			oldQty = currentEntry.TransactionBalance
			entryExists = true
		}

		// Calculate the difference from old consumption
		stockDiff := diff - oldQty

		// Skip if no change in quantity
		if stockDiff == 0 && entryExists {
			return nil
		}

		// Get opening balance
		consumed_openingBalance := 0.0
		if entryExists {
			// If updating existing entry, keep its original opening balance
			consumed_openingBalance = currentEntry.OpeningBalance
		} else {
			// If creating new entry, get from last ledger entry (excluding current production)
			var lastLedgerEntry StockLedgerEntry
			ledgerFilter := bson.M{
				"product_id":   productId, // STEAMEDRCN
				"purchase_id":  purchaseId,
				"warehouse_id": warehouseId,
				"origin":       originId,
				"location":     previousProcessType, // COOKING
				"stock_type":   "WIP",               // Must be WIP stock
				"ref_id":       bson.M{"$ne": productionId},
			}
			ledger_opts := options.FindOne().SetSort(bson.M{"created_on": -1})
			err = database.Collection("stock_ledger").FindOne(context.Background(), ledgerFilter, ledger_opts).Decode(&lastLedgerEntry)
			if err == nil {
				consumed_openingBalance = lastLedgerEntry.ClosingBalance
			}
		}

		// Update stock_in_hand
		stockUpdate := bson.M{
			"$inc": bson.M{"available_qty": stockDiff},
			"$set": bson.M{
				"last_updated_by": userID,
				"last_updated_on": time.Now(),
				"product_id":      productId, // STEAMEDRCN
				"purchase_id":     purchaseId,
				"warehouse_id":    warehouseId,
				"factory_id":      factoryId,
				"origin":          originId,
				"stock_type":      "WIP",               // STEAMEDRCN is WIP from COOKING
				"process_type":    previousProcessType, // COOKING
				"type":            "output",            // It's output from COOKING
			},
			"$setOnInsert": bson.M{
				"_id":        uuid.New().String(),
				"created_by": userID,
				"created_on": time.Now(),
			},
		}
		opts := options.Update().SetUpsert(true)
		_, err = database.Collection("stock_in_hand").UpdateOne(context.Background(), stockFilter, stockUpdate, opts)
		if err != nil {
			fmt.Printf("ERROR: STEAMEDRCN stock_in_hand update failed: %v\n", err)
			return err
		}

		// Calculate closing balance using the actual transaction amount (diff), not stockDiff
		consumed_closingBalance := consumed_openingBalance + diff

		// Get transaction date
		transactionDate := time.Now()
		if soldOnStr, ok := inputData["created_on"]; ok {
			transactionDate = helper.ParseDate(soldOnStr)
		}

		// Update or insert ledger entry
		if entryExists {
			update := bson.M{"$set": bson.M{
				"transaction_balance": diff,
				"opening_balance":     consumed_openingBalance,
				"closing_balance":     consumed_closingBalance,
				"last_updated_by":     userID,
				"last_updated_on":     time.Now(),
				"transaction_date":    transactionDate,
			}}
			database.Collection("stock_ledger").UpdateOne(context.Background(), inputQuery, update)

			// Update subsequent entries if balance changed
			if oldQty != diff {
				balanceChange := diff - oldQty
				updateSubsequentEntries(database, purchaseId, productId, originId, warehouseId, currentEntry.CreatedOn, balanceChange)
			}
		} else {
			// Create new ledger entry - STEAMEDRCN consumption from WIP
			consumptionEntry := StockLedgerEntry{
				ID:                 uuid.New().String(),
				PurchaseID:         purchaseId,
				Origin:             originId,
				StockType:          "WIP", // STEAMEDRCN is WIP stock
				WarehouseId:        warehouseId,
				ProductId:          productId, // STEAMEDRCN
				FactoryId:          factoryId,
				TransactionType:    "production", // WIP consumption uses production type
				TransactionDate:    transactionDate,
				TransactionBalance: diff,
				OpeningBalance:     consumed_openingBalance,
				ClosingBalance:     consumed_closingBalance,
				Remarks:            "Production input - STEAMEDRCN",
				CreatedBy:          userID,
				CreatedOn:          time.Now(),
				ProcessType:        process_type,        // SHELL
				Location:           previousProcessType, // COOKING (where it came from)
				RefId:              productionId,
			}
			database.Collection("stock_ledger").InsertOne(context.Background(), consumptionEntry)
		}

	} else {
		// CREATE logic
		diff := qty * -1

		// First, check if there's a legacy record with product_id="RCN" that needs to be migrated
		legacyFilter := bson.M{
			"product_id":   "RCN",
			"purchase_id":  purchaseId,
			"factory_id":   factoryId,
			"warehouse_id": warehouseId,
			"origin":       originId,
			"process_type": previousProcessType,
		}
		var legacyStock StockBalance
		legacyErr := database.Collection("stock_in_hand").FindOne(context.Background(), legacyFilter).Decode(&legacyStock)
		if legacyErr == nil {
			database.Collection("stock_in_hand").UpdateOne(context.Background(), legacyFilter, bson.M{
				"$set": bson.M{
					"product_id": productId,
					"stock_type": "WIP",
					"type":       "output",
				},
			})
		}

		// Get stock filter - STEAMEDRCN is WIP from COOKING process
		filter := bson.M{
			"product_id":   productId,
			"purchase_id":  purchaseId,
			"factory_id":   factoryId,
			"warehouse_id": warehouseId,
			"origin":       originId,
			"process_type": previousProcessType, // COOKING
		}

		update := bson.M{
			"$inc": bson.M{
				"available_qty": diff,
			},
			"$set": bson.M{
				"last_updated_by": userID,
				"last_updated_on": time.Now(),
				"product_id":      productId, // STEAMEDRCN
				"purchase_id":     purchaseId,
				"warehouse_id":    warehouseId,
				"factory_id":      factoryId,
				"origin":          originId,
				"stock_type":      "WIP",               // STEAMEDRCN is WIP from COOKING
				"process_type":    previousProcessType, // COOKING
				"type":            "output",            // It's output from COOKING
			},
			"$setOnInsert": bson.M{
				"_id":        uuid.New().String(),
				"created_by": userID,
				"created_on": time.Now(),
			},
		}

		opts := options.Update().SetUpsert(true)
		_, err := database.Collection("stock_in_hand").UpdateOne(context.Background(), filter, update, opts)
		if err != nil {
			fmt.Printf("ERROR: STEAMEDRCN stock_in_hand update failed: %v\n", err)
			return err
		}

		// Get last stock balance
		var lastConsumeEntry StockBalance
		con_opts := options.FindOne().SetSort(bson.M{"created_on": -1})
		err = database.Collection("stock_in_hand").FindOne(context.Background(), filter, con_opts).Decode(&lastConsumeEntry)
		consumed_openingBalance := 0.0
		if err == nil {
			consumed_openingBalance = lastConsumeEntry.AvailableQty + qty
		}

		// Get last ledger entry to fetch customer name
		var lastLedgerEntry StockLedgerEntry
		ledger_opts := options.FindOne().SetSort(bson.M{"created_on": -1})
		ledger_err := database.Collection("stock_ledger").FindOne(context.Background(), filter, ledger_opts).Decode(&lastLedgerEntry)
		customerName := ""
		if ledger_err == nil {
			customerName = lastLedgerEntry.CustomerName
		}

		transactionDate := time.Now()
		if soldOnStr, ok := inputData["created_on"]; ok {
			transactionDate = helper.ParseDate(soldOnStr)
		}

		// Calculate closing balance
		consumed_closingBalance := consumed_openingBalance - qty

		// STEAMEDRCN consumption from WIP
		consumptionEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         purchaseId,
			Origin:             originId,
			StockType:          "WIP", // STEAMEDRCN is WIP stock
			WarehouseId:        warehouseId,
			ProductId:          productId, // STEAMEDRCN
			FactoryId:          factoryId,
			TransactionType:    "production", // WIP consumption uses production type
			TransactionDate:    transactionDate,
			TransactionBalance: -qty,
			OpeningBalance:     consumed_openingBalance,
			ClosingBalance:     consumed_closingBalance,
			Remarks:            "Production input - STEAMEDRCN",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
			ProcessID:          helper.ToInt32(inputData["process_id"]),
			CustomerName:       customerName,
			ProcessType:        process_type,        // SHELL
			Location:           previousProcessType, // COOKING (where it came from)
			RefId:              productionId,
		}

		// Insert consumption ledger entry
		if _, err := database.Collection("stock_ledger").InsertOne(context.Background(), consumptionEntry); err != nil {
			return err
		}
	}

	return nil
}

// validateSteamedRCNReduction validates STEAMEDRCN stock availability for SHELL process
func validateSteamedRCNReduction(orgId string, process_type string, inputData map[string]interface{}, templateProducts []bson.M, purDoc map[string]interface{}, productionId string) error {
	// Only apply for SHELL process
	if process_type != "SHELL" {
		return nil
	}

	database := database.GetConnection(orgId)
	purchaseId := inputData["purchase_id"].(string)
	warehouseId := getString(inputData["warehouse_id"])
	factoryId := inputData["factory_id"].(string)
	originId := purDoc["country_origin"].(string)
	productId := "STEAMEDRCN"

	// Calculate total kernel output
	kernelOutput := 0.0
	for _, product := range templateProducts {
		if getString(product["type"]) == "output" {
			prodId := getString(product["product_id"])
			kernelOutput += helper.ToFloat64(inputData[prodId])
		}
	}

	purchaseOutTurn := helper.ToFloat64(purDoc["purchase_out_turn"])
	outTurnLbs := utils.GetenvFloat("OUT_TURN_LBS")

	if purchaseOutTurn <= 0 || outTurnLbs <= 0 || kernelOutput <= 0 {
		return nil
	}

	newQty := (kernelOutput * outTurnLbs) / purchaseOutTurn

	// Get previous process type
	var previousProcessType string
	processPipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", process_type}}}},
		bson.D{
			{"$set",
				bson.D{
					{"previous_process_id",
						bson.D{
							{"$subtract",
								bson.A{
									"$process_id",
									1,
								},
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

	processData, err := helper.GetAggregateQueryResult(orgId, "process", processPipeline)
	if err == nil && len(processData) != 0 {
		previousProcessType = processData[0]["previous_process_type"].(string)
	}

	// Validate input increase against previous process availability
	var currentInputEntry StockLedgerEntry
	txnType := "purchase"
	if getStockTypeForProcess(previousProcessType) != "RCN" {
		txnType = "production"
	}

	inputQuery := bson.M{
		"purchase_id":      purchaseId,
		"product_id":       productId,
		"origin":           originId,
		"warehouse_id":     warehouseId,
		"ref_id":           productionId,
		"transaction_type": txnType,
	}
	err = database.Collection("stock_ledger").FindOne(context.Background(), inputQuery).Decode(&currentInputEntry)

	oldInputQty := 0.0
	isCreate := false
	if err != nil {
		// Ledger entry doesn't exist - this is a CREATE operation
		isCreate = true
	} else {
		// This is an UPDATE operation
		oldInputQty = currentInputEntry.TransactionBalance
	}

	// Compare absolute values
	absNewQty := newQty
	absOldQty := -oldInputQty

	// Calculate additional quantity needed
	additionalQty := absNewQty - absOldQty

	// Only validate if we need more stock (CREATE or increasing UPDATE)
	if additionalQty > 0 {
		// Get available STEAMEDRCN stock
		var prevStockFilter bson.M
		if previousProcessType == "RCN" || previousProcessType == "" {
			prevStockFilter = bson.M{
				"purchase_id":  purchaseId,
				"product_id":   productId,
				"origin":       originId,
				"warehouse_id": warehouseId,
				"factory_id":   factoryId,
			}
		} else {
			prevStockFilter = bson.M{
				"purchase_id":  purchaseId,
				"product_id":   productId,
				"origin":       originId,
				"warehouse_id": warehouseId,
				"factory_id":   factoryId,
				"stock_type":   "WIP",
				"process_type": previousProcessType,
			}
		}

		var prevStock StockBalance
		err := database.Collection("stock_in_hand").FindOne(context.Background(), prevStockFilter).Decode(&prevStock)
		availableQty := 0.0
		if err == nil {
			availableQty = prevStock.AvailableQty
		}

		// Check if sufficient stock is available
		if availableQty < additionalQty {
			// Calculate maximum output that can be produced
			maxAdditionalSteamedRCN := availableQty
			maxTotalSteamedRCN := absOldQty + maxAdditionalSteamedRCN
			maxOutput := (maxTotalSteamedRCN * purchaseOutTurn) / outTurnLbs

			if isCreate {
				return fmt.Errorf("insufficient STEAMEDRCN stock for new production: required %.2f, available %.2f, shortage %.2f. Requested output: %.2f, Maximum output possible: %.2f",
					absNewQty, availableQty, absNewQty-availableQty, kernelOutput, maxOutput)
			} else {
				return fmt.Errorf("insufficient STEAMEDRCN stock for update: required %.2f (additional %.2f), available %.2f, shortage %.2f. Current output: %.2f, Maximum total output possible: %.2f",
					absNewQty, additionalQty, availableQty, additionalQty-availableQty, kernelOutput, maxOutput)
			}
		}
	}

	return nil
}

func ValidateProductionStockUpdate(orgId string, inputData map[string]interface{}, productionId string) error {
	database := database.GetConnection(orgId)
	purchaseId := inputData["purchase_id"].(string)
	process_type := helper.ToString(inputData["process_type"])
	warehouseId := getString(inputData["warehouse_id"])
	factoryId := inputData["factory_id"].(string)

	purDoc, _ := GetDataById(orgId, purchaseId, "purchase")
	originId := purDoc["country_origin"].(string)

	processTypetemplateId := getString(inputData["template_id"])
	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"template_id", processTypetemplateId}}}},
	}

	templateProducts, err := helper.GetAggregateQueryResult(orgId, "process_product", pipeline)
	if err != nil || len(templateProducts) == 0 {
		return err
	}

	// Validate STEAMEDRCN for SHELL process
	if err := validateSteamedRCNReduction(orgId, process_type, inputData, templateProducts, purDoc, productionId); err != nil {
		return err
	}

	for _, obj := range templateProducts {
		// Check if ignore_stock is true, skip if not
		allowAdjust := true
		if val, ok := obj["ignore_stock"]; ok {
			if v, ok := val.(bool); ok && v == true {
				allowAdjust = false
			}
		}

		if !allowAdjust {
			continue // Skip this product
		}

		var productId string
		var ParentProductId string

		// Handle different process types for product ID determination
		// if process_type == "BORM" || process_type == "COOL" {
		// 	productId = getString(obj["parent_id"])
		// } else if process_type == "PEEL" {
		if process_type == "PEEL" {
			var setParent bool
			if obj["set_parent"] != nil {
				setParent = obj["set_parent"].(bool)
			}
			if setParent {
				productId = getString(obj["parent_id"])
				ParentProductId = getString(obj["product_id"])
			} else {
				productId = getString(obj["product_id"])
			}
		} else {
			productId = getString(obj["product_id"])
		}

		// Skip STEAMEDRCN as it's handled separately for SHELL process
		if productId == "STEAMEDRCN" && process_type == "SHELL" {
			continue
		}

		productType := getString(obj["type"])
		var newQty float64

		// Get quantity based on process type
		if process_type == "PEEL" && ParentProductId != "" {
			newQty = helper.ToFloat64(inputData[ParentProductId])
		} else {
			newQty = helper.ToFloat64(inputData[productId])
		}

		// Allow zero quantities during validation - they will be handled in update logic
		if productType == "output" {
			// Validate output reduction ONLY if decreasing
			var currentEntry StockLedgerEntry
			query := bson.M{
				"purchase_id":      purchaseId,
				"product_id":       productId,
				"origin":           originId,
				"warehouse_id":     warehouseId,
				"production_id":    productionId, // Use production_id for output, not ref_id
				"transaction_type": "production",
			}
			err := database.Collection("stock_ledger").FindOne(context.Background(), query).Decode(&currentEntry)
			if err != nil {
				// No existing entry - this is a new product, skip validation
				continue
			}

			oldQty := currentEntry.TransactionBalance

			// ONLY check next process if DECREASING output (including reducing to zero)
			if newQty < oldQty {
				reduction := oldQty - newQty

				// Check if next process has this product in stock_in_hand
				nextProcessType := GetNextProductType(process_type)
				if nextProcessType != "" {
					nextStockFilter := bson.M{
						"purchase_id":  purchaseId,
						"product_id":   productId,
						"origin":       originId,
						"warehouse_id": warehouseId,
						"factory_id":   factoryId,
						"process_type": nextProcessType,
					}

					var nextStock StockBalance
					err := database.Collection("stock_in_hand").FindOne(context.Background(), nextStockFilter).Decode(&nextStock)

					// If next process has stock, it means they consumed from current process
					// Check if reduction would make it impossible
					if err == nil && nextStock.AvailableQty > 0 {
						// Next process has stock, check if we can reduce
						// We can only reduce if the reduction doesn't exceed what's available in current process
						var currentStock StockBalance
						currentStockFilter := bson.M{
							"purchase_id":  purchaseId,
							"product_id":   productId,
							"origin":       originId,
							"warehouse_id": warehouseId,
							"factory_id":   factoryId,
							"process_type": process_type,
						}
						err2 := database.Collection("stock_in_hand").FindOne(context.Background(), currentStockFilter).Decode(&currentStock)

						if err2 == nil {
							// Current available stock in this process
							currentAvailable := currentStock.AvailableQty

							// If reducing by more than what's currently available, it means next process consumed it
							if reduction > currentAvailable {
								return fmt.Errorf("%s has %.2f of this product (consumed from %s), can't reduce %s output by %.2f (only %.2f available)",
									nextProcessType, nextStock.AvailableQty, process_type, process_type, reduction, currentAvailable)
							}
						}
					}
				}
			}
			// If INCREASING output (newQty > oldQty), no need to check next process
		} else if productType == "input" {
			// Allow zero for input - it means removing the consumption
			if newQty == 0 {
				// Check if there's an existing entry to validate we can remove it
				var currentInputEntry StockLedgerEntry
				inputQuery := bson.M{
					"purchase_id":      purchaseId,
					"product_id":       productId,
					"origin":           originId,
					"warehouse_id":     warehouseId,
					"ref_id":           productionId,
					"transaction_type": "purchase",
				}
				err := database.Collection("stock_ledger").FindOne(context.Background(), inputQuery).Decode(&currentInputEntry)
				if err == nil {
					// Entry exists, will be removed - no validation needed
				}
				continue
			}

			// Validate input increase against previous process availability
			var currentInputEntry StockLedgerEntry
			inputQuery := bson.M{
				"purchase_id":      purchaseId,
				"product_id":       productId,
				"origin":           originId,
				"warehouse_id":     warehouseId,
				"ref_id":           productionId,
				"transaction_type": "purchase",
			}
			err := database.Collection("stock_ledger").FindOne(context.Background(), inputQuery).Decode(&currentInputEntry)

			// If ledger entry doesn't exist, skip validation
			// This happens when a new product field is added to an existing production
			if err != nil {
				continue
			}

			oldInputQty := currentInputEntry.TransactionBalance

			// Compare absolute values since both are consumption (negative)
			// newQty is positive input, oldInputQty is negative consumption
			// Convert to absolute for comparison
			absNewQty := newQty
			absOldQty := -oldInputQty // Convert negative consumption to positive

			if absNewQty > absOldQty {
				previousProcessType := getPreviousProductType(process_type)
				additionalQty := absNewQty - absOldQty

				var prevStockFilter bson.M
				if previousProcessType == "RCN" {
					prevStockFilter = bson.M{
						"purchase_id":  purchaseId,
						"product_id":   productId,
						"origin":       originId,
						"warehouse_id": warehouseId,
						"factory_id":   factoryId,
					}
				} else {
					prevStockFilter = bson.M{
						"purchase_id":  purchaseId,
						"product_id":   productId,
						"origin":       originId,
						"warehouse_id": warehouseId,
						"factory_id":   factoryId,
						"stock_type":   "WIP",
						"process_type": previousProcessType, // Need process_type to identify which process's output
					}
				}

				var prevStock StockBalance
				err := database.Collection("stock_in_hand").FindOne(context.Background(), prevStockFilter).Decode(&prevStock)
				availableQty := 0.0
				if err == nil {
					availableQty = prevStock.AvailableQty
				}

				if availableQty < additionalQty {
					return fmt.Errorf("insufficient %s stock: available %.2f, additional required %.2f", previousProcessType, availableQty, additionalQty)
				}
			}
		}
	}

	// Cross-process validation for process chain integrity
	if err := validateProcessChainIntegrity(orgId, purchaseId, process_type, warehouseId, factoryId, originId, inputData, productionId); err != nil {
		return err
	}

	return nil
}

func GetNextProductType(processType string) string {
	switch processType {
	case "COOK":
		return "SHELL"
	case "SHELL":
		return "BORM"
	case "BORM":
		return "COOL"
	case "COOL":
		return "PEEL"
	case "PEEL":
		return "GRAD"
	case "GRAD":
		return "PACK"
	default:
		return ""
	}
}
func validateProcessChainIntegrity(orgId, purchaseId, processType, warehouseId, factoryId, originId string, inputData map[string]interface{}, productionId string) error {
	database := database.GetConnection(orgId)

	// Get all processes in the chain after current process
	processChain := []string{"COOK", "SHELL", "BORM", "COOL", "PEEL", "GRAD", "PACK"}
	currentIndex := -1
	for i, p := range processChain {
		if p == processType {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return nil
	}

	// Check if any subsequent processes exist and validate their dependencies
	for i := currentIndex + 1; i < len(processChain); i++ {
		nextProcess := processChain[i]

		// Check if this next process has any productions
		nextProcessFilter := bson.M{
			"purchase_id":  purchaseId,
			"process_type": nextProcess,
			"warehouse_id": warehouseId,
			"factory_id":   factoryId,
		}

		var nextProcessProductions []map[string]interface{}
		cursor, err := database.Collection("productions").Find(context.Background(), nextProcessFilter)
		if err == nil {
			cursor.All(context.Background(), &nextProcessProductions)
			if len(nextProcessProductions) > 0 {
				// Validate that current process changes don't break next process requirements
				for _, nextProd := range nextProcessProductions {
					nextProdId := nextProd["_id"].(string)

					// Get input requirements for next process
					nextInputFilter := bson.M{
						"ref_id":           nextProdId,
						"transaction_type": "purchase",
						"purchase_id":      purchaseId,
						"origin":           originId,
					}

					var nextInputEntries []StockLedgerEntry
					cursor, err := database.Collection("stock_ledger").Find(context.Background(), nextInputFilter)
					if err == nil {
						cursor.All(context.Background(), &nextInputEntries)
						for _, entry := range nextInputEntries {
							requiredQty := -entry.TransactionBalance

							// Check if current process output reduction affects this requirement
							if productId, exists := inputData[entry.ProductId]; exists {
								newOutputQty := helper.ToFloat64(productId)

								// Get current output from current process
								currentOutputFilter := bson.M{
									"ref_id":           productionId,
									"transaction_type": "production",
									"product_id":       entry.ProductId,
								}

								var currentOutputEntry StockLedgerEntry
								err := database.Collection("stock_ledger").FindOne(context.Background(), currentOutputFilter).Decode(&currentOutputEntry)
								if err == nil {
									currentOutputQty := currentOutputEntry.TransactionBalance
									if newOutputQty < currentOutputQty {
										reduction := currentOutputQty - newOutputQty
										if reduction >= requiredQty {
											return fmt.Errorf("reducing %s output by %.2f would affect %s process requirement of %.2f", processType, reduction, nextProcess, requiredQty)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

func updateSubsequentEntries(database *mongo.Database, purchaseId, productId, originId, warehouseId string, afterTime time.Time, balanceChange float64) {
	// Only update subsequent entries for the SAME product
	filter := bson.M{
		"purchase_id":  purchaseId,
		"product_id":   productId, // This ensures only same product entries are updated
		"origin":       originId,
		"warehouse_id": warehouseId,
		"created_on":   bson.M{"$gt": afterTime},
	}

	update := bson.M{"$inc": bson.M{
		"opening_balance": balanceChange,
		"closing_balance": balanceChange,
	}}

	database.Collection("stock_ledger").UpdateMany(context.Background(), filter, update)
}

// It checks if sufficient GRAD stock is available before allowing the update
func ValidatePackProductionStockUpdate(orgId string, inputData map[string]interface{}, productionId string) error {
	database := database.GetConnection(orgId)
	purchaseId := inputData["purchase_id"].(string)
	warehouseId := getString(inputData["warehouse_id"])
	factoryId := inputData["factory_id"].(string)
	productId := getString(inputData["product_id"])

	purDoc, _ := GetDataById(orgId, purchaseId, "purchase")
	originId := purDoc["country_origin"].(string)

	// Calculate new quantity based on packing
	packingType := getString(inputData["type_of_packing"])
	filledTins := helper.ToInt(inputData["filled_tins"])

	packingTypeDoc, _ := GetDataById(orgId, packingType, "lookup")
	packingValue := helper.ToInt(packingTypeDoc["value"])
	newQty := float64(filledTins * packingValue)

	// Get current PACK production ledger entry to find old quantity
	var currentEntry StockLedgerEntry
	ledgerQuery := bson.M{
		"purchase_id":      purchaseId,
		"product_id":       productId,
		"origin":           originId,
		"warehouse_id":     warehouseId,
		"ref_id":           productionId,
		"transaction_type": bson.M{"$in": []string{"kernel", "KERNEL"}}, // Support both lowercase and uppercase
	}

	err := database.Collection("stock_ledger").FindOne(context.Background(), ledgerQuery).Decode(&currentEntry)
	oldQty := 0.0
	if err == nil {
		// TransactionBalance is negative for consumption
		oldQty = -currentEntry.TransactionBalance
	}

	// If increasing quantity, check GRAD stock availability
	if newQty > oldQty {
		additionalQty := newQty - oldQty

		// Check GRAD stock availability
		gradStockFilter := bson.M{
			"purchase_id":  purchaseId,
			"product_id":   productId,
			"origin":       originId,
			"warehouse_id": warehouseId,
			"factory_id":   factoryId,
			"stock_type":   "WIP",
			"process_type": "GRAD",
		}

		var gradStock StockBalance
		err := database.Collection("stock_in_hand").FindOne(context.Background(), gradStockFilter).Decode(&gradStock)
		availableQty := 0.0
		if err == nil {
			availableQty = gradStock.AvailableQty
		}

		if availableQty < additionalQty {
			return fmt.Errorf("insufficient GRAD stock: available %.2f, additional required %.2f", availableQty, additionalQty)
		}
	}

	// If decreasing quantity, check if KERNELs have been sold
	if newQty < oldQty {
		reduction := oldQty - newQty

		// Check kernel inventory sales
		kernelStockFilter := bson.M{
			"purchase_id":  purchaseId,
			"product_id":   productId,
			"origin":       originId,
			"warehouse_id": warehouseId,
			"factory_id":   factoryId,
			"stock_type":   "kernel",
		}

		var kernelStock StockBalance
		err := database.Collection("stock_in_hand").FindOne(context.Background(), kernelStockFilter).Decode(&kernelStock)

		// Calculate how much has been sold (original - current)
		if err == nil {
			// If reducing PACK quantity would make kernel stock negative, it means kernels have been sold
			if kernelStock.AvailableQty < reduction {
				return fmt.Errorf("cannot reduce PACK quantity by %.2f: %.2f kernels already sold or consumed", reduction, oldQty-kernelStock.AvailableQty)
			}
		}
	}

	return nil
}

// UpdatePackProductionStockByRefId updates stock for PACK process
// It reduces GRAD stock and increases KERNEL stock
func UpdatePackProductionStockByRefId(orgId string, inputData map[string]interface{}, productionId string, userID string) error {
	database := database.GetConnection(orgId)
	purchaseId := inputData["purchase_id"].(string)
	warehouseId := getString(inputData["warehouse_id"])
	factoryId := inputData["factory_id"].(string)
	productId := getString(inputData["product_id"])

	purDoc, _ := GetDataById(orgId, purchaseId, "purchase")
	originId := purDoc["country_origin"].(string)

	// Calculate new quantity based on packing
	packingType := getString(inputData["type_of_packing"])
	filledTins := helper.ToInt(inputData["filled_tins"])

	packingTypeDoc, _ := GetDataById(orgId, packingType, "lookup")
	packingValue := helper.ToInt(packingTypeDoc["value"])
	newQty := float64(filledTins * packingValue)

	// Get transaction date
	transactionDate := time.Now()
	if soldOnStr, ok := inputData["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	// Check if ledger entry exists for this production
	ledgerQuery := bson.M{
		"purchase_id":      purchaseId,
		"product_id":       productId,
		"origin":           originId,
		"warehouse_id":     warehouseId,
		"ref_id":           productionId,
		"transaction_type": bson.M{"$in": []string{"kernel", "KERNEL"}}, // Support both lowercase and uppercase
	}

	var currentEntry StockLedgerEntry
	err := database.Collection("stock_ledger").FindOne(context.Background(), ledgerQuery).Decode(&currentEntry)
	oldQty := 0.0
	entryExists := false
	if err == nil {
		// TransactionBalance is negative for consumption
		oldQty = -currentEntry.TransactionBalance
		entryExists = true
	}

	// Calculate the difference
	diff := newQty - oldQty

	// Update GRAD stock_in_hand (reduce by diff)
	gradStockFilter := bson.M{
		"purchase_id":  purchaseId,
		"product_id":   productId,
		"origin":       originId,
		"warehouse_id": warehouseId,
		"factory_id":   factoryId,
		"stock_type":   "WIP",
		"process_type": "GRAD",
	}

	gradStockUpdate := bson.M{
		"$inc": bson.M{"available_qty": -diff}, // Reduce GRAD stock
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
	}
	database.Collection("stock_in_hand").UpdateOne(context.Background(), gradStockFilter, gradStockUpdate)

	// Update KERNEL stock_in_hand (increase by diff)
	kernelStockFilter := bson.M{
		"purchase_id":  purchaseId,
		"product_id":   productId,
		"origin":       originId,
		"warehouse_id": warehouseId,
		"factory_id":   factoryId,
		"stock_type":   "kernel",
	}

	kernelStockUpdate := bson.M{
		"$inc": bson.M{"available_qty": diff}, // Increase kernel stock
		"$set": bson.M{
			"last_updated_by": userID,
			"last_updated_on": time.Now(),
		},
		"$setOnInsert": bson.M{
			"_id":        uuid.New().String(),
			"created_by": userID,
			"created_on": time.Now(),
		},
	}
	opts := options.Update().SetUpsert(true)
	database.Collection("stock_in_hand").UpdateOne(context.Background(), kernelStockFilter, kernelStockUpdate, opts)

	// Get updated GRAD stock for balance calculation (AFTER the update)
	var gradStock StockBalance
	gradOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
	err = database.Collection("stock_in_hand").FindOne(context.Background(), gradStockFilter, gradOpts).Decode(&gradStock)

	gradOpeningBalance := 0.0
	if err == nil {
		// Opening balance = current stock (after update) - transaction
		// Transaction is -newQty (consumption), so we subtract it (which adds back)
		gradOpeningBalance = gradStock.AvailableQty - (-diff)
	}
	gradClosingBalance := gradOpeningBalance + (-newQty)

	// Update or insert GRAD consumption ledger entry
	if entryExists {
		// Update existing entry
		update := bson.M{"$set": bson.M{
			"transaction_balance": -newQty,
			"opening_balance":     gradOpeningBalance,
			"closing_balance":     gradClosingBalance,
			"last_updated_by":     userID,
			"last_updated_on":     time.Now(),
			"transaction_date":    transactionDate,
		}}
		database.Collection("stock_ledger").UpdateOne(context.Background(), ledgerQuery, update)

		// Update subsequent entries if balance changed
		if oldQty != newQty {
			balanceChange := -(newQty - oldQty)
			updateSubsequentEntries(database, purchaseId, productId, originId, warehouseId, currentEntry.CreatedOn, balanceChange)
		}
	} else {
		// Create new GRAD consumption entry
		consumptionEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         purchaseId,
			Origin:             originId,
			StockType:          "WIP",
			WarehouseId:        warehouseId,
			ProductId:          productId,
			FactoryId:          factoryId,
			TransactionType:    "kernel",
			TransactionDate:    transactionDate,
			TransactionBalance: -newQty,
			OpeningBalance:     gradOpeningBalance,
			ClosingBalance:     gradClosingBalance,
			Remarks:            "PACK process GRAD consumption",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
			ProcessType:        "PACK",
			Location:           warehouseId,
			RefId:              productionId,
		}
		database.Collection("stock_ledger").InsertOne(context.Background(), consumptionEntry)
	}

	// Get updated KERNEL stock for balance calculation (AFTER the update)
	var kernelStock StockBalance
	kernelOpts := options.FindOne().SetSort(bson.M{"created_on": -1})
	err = database.Collection("stock_in_hand").FindOne(context.Background(), kernelStockFilter, kernelOpts).Decode(&kernelStock)

	kernelOpeningBalance := 0.0
	if err == nil {
		// Opening balance = current stock (after update) - transaction
		// Transaction is +diff (production), so we subtract it
		kernelOpeningBalance = kernelStock.AvailableQty - diff
	}
	kernelClosingBalance := kernelOpeningBalance + newQty

	// Update or insert KERNEL production ledger entry
	kernelLedgerQuery := bson.M{
		"purchase_id":      purchaseId,
		"product_id":       productId,
		"origin":           originId,
		"warehouse_id":     warehouseId,
		"production_id":    productionId,
		"transaction_type": "production",
		"process_type":     "PACK",
	}

	var kernelEntry StockLedgerEntry
	err = database.Collection("stock_ledger").FindOne(context.Background(), kernelLedgerQuery).Decode(&kernelEntry)

	if err == nil {
		// Update existing kernel production entry
		update := bson.M{"$set": bson.M{
			"transaction_balance": newQty,
			"opening_balance":     kernelOpeningBalance,
			"closing_balance":     kernelClosingBalance,
			"last_updated_by":     userID,
			"last_updated_on":     time.Now(),
			"transaction_date":    transactionDate,
		}}
		database.Collection("stock_ledger").UpdateOne(context.Background(), kernelLedgerQuery, update)
	} else {
		// Create new kernel production entry
		packedEntry := StockLedgerEntry{
			ID:                 uuid.New().String(),
			PurchaseID:         purchaseId,
			Origin:             originId,
			StockType:          "kernel",
			WarehouseId:        warehouseId,
			ProductId:          productId,
			FactoryId:          factoryId,
			ProductionId:       productionId,
			TransactionType:    "production",
			TransactionDate:    transactionDate,
			TransactionBalance: newQty,
			OpeningBalance:     kernelOpeningBalance,
			ClosingBalance:     kernelClosingBalance,
			Remarks:            "KERNEL Production",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
			ProcessType:        "PACK",
			Location:           warehouseId,
			RefId:              productionId,
		}
		database.Collection("stock_ledger").InsertOne(context.Background(), packedEntry)
	}

	return nil
}
