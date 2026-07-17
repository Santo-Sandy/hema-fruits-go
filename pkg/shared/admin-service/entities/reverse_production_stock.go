package entities

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

// ReverseProductionStock completely reverses all stock entries for a production record
// This is used when template changes - we reverse old stock entries and start fresh
// If the output has been consumed by next processes, those must be reversed first
func ReverseProductionStock(orgId string, oldData map[string]interface{}, productionId string, userId string) error {
	db := database.GetConnection(orgId)
	ctx := context.Background()

	log.Printf("Reversing production stock for production %s", productionId)
	log.Printf("oldData keys: %v", getMapKeys(oldData))

	// Try to get old template details to check for consumption
	// Check multiple possible keys for template_id
	oldTemplateId := getString(oldData["template_id"])
	if oldTemplateId == "" {
		oldTemplateId = getString(oldData["template"])
	}
	if oldTemplateId == "" {
		oldTemplateId = getString(oldData["production_template_id"])
	}

	log.Printf("Looking for template with ID: %s", oldTemplateId)

	var oldOutputProducts []string

	if oldTemplateId != "" {
		// Fetch all process_product records for the old template
		cursor, err := db.Collection("process_product").Find(ctx, bson.M{"template_id": oldTemplateId})
		if err != nil {
			log.Printf("Warning: Could not fetch old template products for %s: %v - proceeding with reversal", oldTemplateId, err)
		} else {
			defer cursor.Close(ctx)

			var templateProducts []map[string]interface{}
			if err := cursor.All(ctx, &templateProducts); err != nil {
				log.Printf("Warning: Could not decode old template products: %v - proceeding with reversal", err)
			} else if len(templateProducts) == 0 {
				log.Printf("Warning: No products found for old template %s - proceeding with reversal", oldTemplateId)
			} else {
				log.Printf("Found %d products for old template %s", len(templateProducts), oldTemplateId)

				// Extract output products from template
				for _, product := range templateProducts {
					if getString(product["type"]) == "output" {
						if productId := getString(product["product_id"]); productId != "" {
							oldOutputProducts = append(oldOutputProducts, productId)
						}
					}
				}

				log.Printf("Old template - Output products: %v", oldOutputProducts)

				// Check if output products were consumed by next production
				nextProductionConsumed, err := checkNextProductionConsumption(db, ctx, productionId, oldOutputProducts)
				if err != nil {
					log.Printf("Warning: Failed to check next production consumption: %v", err)
				}
				if nextProductionConsumed {
					return fmt.Errorf("cannot reverse: output products from this production were consumed by subsequent productions")
				}
			}
		}
	} else {
		log.Printf("Warning: No template_id found in oldData - proceeding with reversal without consumption check")
	}

	// Get all stock ledger entries for this production
	ledgerFilter := bson.M{
		"$or": []bson.M{
			{"production_id": productionId},
			{"ref_id": productionId},
		},
	}

	cursor, err := db.Collection("stock_ledger").Find(ctx, ledgerFilter)
	if err != nil {
		return fmt.Errorf("failed to find stock ledger entries: %v", err)
	}
	defer cursor.Close(ctx)

	var ledgerEntries []map[string]interface{}
	if err := cursor.All(ctx, &ledgerEntries); err != nil {
		return fmt.Errorf("failed to decode ledger entries: %v", err)
	}

	log.Printf("Found %d stock ledger entries to reverse", len(ledgerEntries))

	// First pass: Check stock_in_hand for sufficient available_qty
	for _, entry := range ledgerEntries {
		productId := getString(entry["product_id"])
		purchaseId := getString(entry["purchase_id"])
		warehouseId := getString(entry["warehouse_id"])
		origin := getString(entry["origin"])
		processType := getString(entry["process_type"])
		transactionBalance := helper.ToFloat64(entry["transaction_balance"])
		reversalAmount := -transactionBalance

		log.Printf("Checking ledger entry - Product: %s, ProcessType: %s, TransactionBalance: %.2f, ReversalAmount: %.2f",
			productId, processType, transactionBalance, reversalAmount)

		// Only check if we're reducing stock (reversalAmount is negative)
		// If reversalAmount is positive, we're adding stock back, which doesn't need validation
		if reversalAmount < 0 {
			// Build filter to find stock_in_hand record
			stockFilter := bson.M{
				"product_id":   productId,
				"warehouse_id": warehouseId,
				"origin":       origin,
			}

			if purchaseId != "" {
				stockFilter["purchase_id"] = purchaseId
			}

			if processType != "" && processType != "RCN" {
				stockFilter["process_type"] = processType
			}

			// Check current available_qty in stock_in_hand
			var stockRecord map[string]interface{}
			err := db.Collection("stock_in_hand").FindOne(ctx, stockFilter).Decode(&stockRecord)
			if err != nil {
				// Stock record not found - for outputs being reversed, this is expected
				log.Printf("⚠️ Stock record not found for product %s (will be created during reversal)", productId)
				continue
			}

			availableQty := helper.ToFloat64(stockRecord["available_qty"])
			requiredQty := -reversalAmount // Convert to positive for comparison

			if availableQty < requiredQty {
				return fmt.Errorf("insufficient stock for product %s: available %.2f, required %.2f (warehouse: %s, origin: %s)",
					productId, availableQty, requiredQty, warehouseId, origin)
			}

			log.Printf("✓ Stock check passed for product %s: available %.2f >= required %.2f",
				productId, availableQty, requiredQty)
		} else {
			log.Printf("✓ Reversal will add stock back for product %s: +%.2f (no validation needed)", productId, reversalAmount)
		}
	}

	// Second pass: Perform the actual reversals
	for _, entry := range ledgerEntries {
		productId := getString(entry["product_id"])
		purchaseId := getString(entry["purchase_id"])
		warehouseId := getString(entry["warehouse_id"])
		origin := getString(entry["origin"])
		factoryId := getString(entry["factory_id"])
		processType := getString(entry["process_type"])
		transactionBalance := helper.ToFloat64(entry["transaction_balance"])
		stockType := getString(entry["stock_type"])
		location := getString(entry["location"])
		reversalAmount := -transactionBalance

		// Build stock filter
		// For stock_in_hand, we need to match where the stock actually is
		// - For inputs (negative transaction): stock came from 'location' (previous process)
		// - For outputs (positive transaction): stock is in current 'process_type'
		stockFilter := bson.M{
			"product_id":   productId,
			"warehouse_id": warehouseId,
			"origin":       origin,
		}

		if purchaseId != "" {
			stockFilter["purchase_id"] = purchaseId
		}

		// Determine which process_type to use for the filter
		var stockProcessType string
		if transactionBalance < 0 {
			// Input product - stock came from 'location' (previous process)
			stockProcessType = location
			if location == warehouseId {
				// Location is warehouse, means it's RCN - don't add process_type to filter
				stockProcessType = ""
			}
		} else {
			// Output product - stock is in current process
			stockProcessType = processType
		}

		if stockProcessType != "" && stockProcessType != "RCN" {
			stockFilter["process_type"] = stockProcessType
		}

		log.Printf("Reversing %s: transactionBalance=%.2f, reversalAmount=%.2f, stockProcessType=%s",
			productId, transactionBalance, reversalAmount, stockProcessType)

		stockUpdate := bson.M{
			"$inc": bson.M{"available_qty": reversalAmount},
			"$set": bson.M{
				"last_updated_by": userId,
				"last_updated_on": time.Now(),
			},
		}

		result, err := db.Collection("stock_in_hand").UpdateOne(ctx, stockFilter, stockUpdate)
		if err != nil {
			return fmt.Errorf("failed to update stock_in_hand for product %s: %v", productId, err)
		}

		if result.MatchedCount == 0 {
			// Stock record doesn't exist - create it with the reversal amount
			log.Printf("⚠️ Stock record not found for product %s - creating new record", productId)

			stockRecord := bson.M{
				"_id":             uuid.New().String(),
				"product_id":      productId,
				"purchase_id":     purchaseId,
				"warehouse_id":    warehouseId,
				"origin":          origin,
				"factory_id":      factoryId,
				"stock_type":      stockType,
				"available_qty":   reversalAmount,
				"created_by":      userId,
				"created_on":      time.Now(),
				"last_updated_by": userId,
				"last_updated_on": time.Now(),
			}

			if stockProcessType != "" && stockProcessType != "RCN" {
				stockRecord["process_type"] = stockProcessType
			}

			if location != "" {
				stockRecord["location"] = location
			}

			_, err = db.Collection("stock_in_hand").InsertOne(ctx, stockRecord)
			if err != nil {
				return fmt.Errorf("failed to create stock_in_hand record for product %s: %v", productId, err)
			}
			log.Printf("Created stock record for product %s with qty: %.2f, process_type: %s", productId, reversalAmount, stockProcessType)
		} else {
			log.Printf("Updated stock for product %s: %+.2f", productId, reversalAmount)
		}

		// Get the last ledger entry to calculate opening balance
		// Query for the most recent NON-REVERSAL ledger entry for this product/location
		// We want to find the ORIGINAL production entry (before this reversal) to get its closing balance
		ledgerQueryFilter := bson.M{
			"product_id":       productId,
			"warehouse_id":     warehouseId,
			"origin":           origin,
			"transaction_type": bson.M{"$ne": "production_reversal"}, // Only exclude reversal entries
		}

		if purchaseId != "" {
			ledgerQueryFilter["purchase_id"] = purchaseId
		}

		// For ledger entries, match by location (where the stock is)
		// - For outputs: location = current process_type
		// - For inputs: location = previous process (from location field)
		var ledgerLocation string
		if transactionBalance > 0 {
			// Output product - location is current process
			if processType != "" && processType != "RCN" {
				ledgerLocation = processType
			} else {
				ledgerLocation = warehouseId
			}
		} else {
			// Input product - location is where stock came from
			if location != "" && location != warehouseId {
				ledgerLocation = location
			} else {
				ledgerLocation = warehouseId
			}
		}

		ledgerQueryFilter["location"] = ledgerLocation

		log.Printf("Querying last ledger for %s with location=%s, excluding production_id=%s",
			productId, ledgerLocation, productionId)

		// IMPORTANT: Use bson.D for sort (ordered), not bson.M (unordered)
		opts := options.FindOne().SetSort(bson.D{
			{Key: "transaction_date", Value: -1},
			{Key: "created_on", Value: -1},
		})
		var lastLedgerEntry map[string]interface{}
		err = db.Collection("stock_ledger").FindOne(ctx, ledgerQueryFilter, opts).Decode(&lastLedgerEntry)

		openingBalance := 0.0
		if err == nil {
			openingBalance = helper.ToFloat64(lastLedgerEntry["closing_balance"])
			log.Printf("✓ Found last ledger entry for %s (location=%s): closing_balance=%.2f, entry_id=%s, date=%v",
				productId, ledgerLocation, openingBalance, getString(lastLedgerEntry["_id"]), lastLedgerEntry["transaction_date"])
		} else if err == mongo.ErrNoDocuments {
			log.Printf("⚠️ No previous ledger entry found for %s (location=%s), starting with opening_balance=0", productId, ledgerLocation)
		} else {
			log.Printf("❌ Error fetching last ledger entry for %s: %v", productId, err)
		}

		closingBalance := openingBalance + reversalAmount

		log.Printf("Creating reversal ledger: product=%s, opening=%.2f, transaction=%.2f, closing=%.2f",
			productId, openingBalance, reversalAmount, closingBalance)

		// Create reversal ledger entry
		reversalEntry := bson.M{
			"_id":                 uuid.New().String(),
			"purchase_id":         purchaseId,
			"origin":              origin,
			"stock_type":          stockType,
			"warehouse_id":        warehouseId,
			"product_id":          productId,
			"factory_id":          factoryId,
			"production_id":       productionId,
			"transaction_type":    "production_reversal",
			"transaction_date":    time.Now(),
			"transaction_balance": reversalAmount,
			"opening_balance":     openingBalance,
			"closing_balance":     closingBalance,
			"remarks":             fmt.Sprintf("Reversal due to template change - original entry: %s", getString(entry["_id"])),
			"created_by":          userId,
			"created_on":          time.Now(),
			"process_type":        processType,
			"location":            location,
			"ref_id":              productionId,
			"original_entry_id":   getString(entry["_id"]),
		}

		_, err = db.Collection("stock_ledger").InsertOne(ctx, reversalEntry)
		if err != nil {
			return fmt.Errorf("failed to create reversal ledger entry: %v", err)
		}

		log.Printf("Created reversal ledger entry for %s: opening=%.2f, transaction=%.2f, closing=%.2f",
			productId, openingBalance, reversalAmount, closingBalance)
	}

	log.Printf("✅ Successfully reversed production stock for %s", productionId)
	return nil
}

// getMapKeys returns all keys from a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// checkNextProductionConsumption checks if output products were consumed by subsequent productions
func checkNextProductionConsumption(db *mongo.Database, ctx context.Context, currentProductionId string, outputProducts []string) (bool, error) {
	if len(outputProducts) == 0 {
		return false, nil
	}

	// Get current production date to find next productions
	var currentProduction map[string]interface{}
	err := db.Collection("productions").FindOne(ctx, bson.M{"_id": currentProductionId}).Decode(&currentProduction)
	if err != nil {
		return false, err
	}

	productionDate := currentProduction["production_date"]

	// Find stock ledger entries where these output products were consumed after this production
	consumptionFilter := bson.M{
		"product_id":          bson.M{"$in": outputProducts},
		"transaction_type":    bson.M{"$in": []string{"production_input", "production"}},
		"transaction_date":    bson.M{"$gt": productionDate},
		"transaction_balance": bson.M{"$lt": 0}, // Negative balance means consumption
		"production_id":       bson.M{"$ne": currentProductionId},
	}

	count, err := db.Collection("stock_ledger").CountDocuments(ctx, consumptionFilter)
	if err != nil {
		return false, err
	}

	if count > 0 {
		log.Printf("⚠️ Found %d consumption entries for output products in subsequent productions", count)
	}

	return count > 0, nil
}
