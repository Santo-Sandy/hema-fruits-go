package entities

import (
	"context"
	"errors" // Added for math.Min
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

func calculateStockDelta(transactionType string, quantity float64) float64 {
	switch transactionType {
	case "purchase", "purchase_return":
		return quantity
	case "sale", "sale_return":
		return -quantity
	case "adjustment":
		return quantity
	case "operation":
		return -quantity
	case "production":
		return -quantity // Production consumes stock
	case "inWard-jobWork":
		return quantity
	case "outWard-jobWork":
		return -quantity
	case "TRANSFER-IN":
		return quantity
	case "TRANSFER-OUT":
		return -quantity
	default:
		return quantity
	}
}

func ProcessInternationAndDomesticRCNPurchase(organizationID string, purchaseType string, purchaseDocument map[string]interface{}, wareHouseDocument map[string]interface{}, userID string, stockMovementId string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	// var purchaseType string
	var quantity float64
	stockType := "RCN"
	productID := ""
	factoryID := ""
	warehouseID := ""
	purchaseId := ""
	originId := ""
	refId := ""
	// purchaseType, _ = purchaseDocument["purchasetype"].(string)
	// if purchaseType == "" {
	// 	purchaseType = "domestic"
	// }

	if purchaseType == "domestic" {
		// For domestic purchases, wareHouseDocument is invoice_details
		if cnoTo, ok := wareHouseDocument["cno_to"].(string); ok && cnoTo == "Warehouse" {
			if whID, ok := wareHouseDocument["warehouse_id"].(string); ok {
				warehouseID = whID
				isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
				if isfactoryWarehouse {
					factoryID = factoryId
				}
			}
		}

		if invoiceQuantity, ok := wareHouseDocument["quantity"].(float64); ok {
			quantity = invoiceQuantity
		}

		if quantity == 0 {
			return errors.New("domestic purchase: missing or zero quantity")
		}
		purchaseId = purchaseDocument["_id"].(string)
		originId = purchaseDocument["country_origin"].(string)
		productID = "RCN"
		refId = stockMovementId
	} else if purchaseType == "International" {
		refId = stockMovementId
		warehouseID = wareHouseDocument["warehouse"].(string)

		isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}

		if netWeight, ok := wareHouseDocument["weight"].(float64); ok {
			quantity = netWeight
		}

		if quantity == 0 {
			return errors.New("purchase: missing or zero net_weight in quality_reports")
		}
		purchaseId = purchaseDocument["_id"].(string)
		originId = purchaseDocument["country_origin"].(string)
		productID = "RCN"
	} else if purchaseType == "kernel" {
		refId = stockMovementId
		stockType = "kernel"
		productID = purchaseDocument["product_id"].(string)
		warehouseID = purchaseDocument["warehouse"].(string)
		purchaseDocument["country_origin"] = purchaseDocument["origin"].(string)
		isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}
		purchaseId = purchaseDocument["template_id"].(string)
		originId = purchaseDocument["origin"].(string)
		quantity = helper.ToFloat64(purchaseDocument["quantity"])
	}

	customerName := ""
	if companyName, ok := purchaseDocument["customer_id"].(string); ok {
		customerName = companyName
	}
	transactionDate := time.Now()
	if soldOnStr, ok := purchaseDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	insertID := uuid.New().String()
	ledgerEntry := StockLedgerEntry{
		ID:              insertID,
		PurchaseID:      purchaseId,
		Origin:          originId,
		StockType:       stockType,
		WarehouseId:     warehouseID,
		ProductId:       productID,
		FactoryId:       factoryID,
		TransactionType: "purchase",
		TransactionDate: transactionDate,
		CustomerName:    customerName,
		Remarks:         "Purchase arrival ledger",
		CreatedBy:       userID,
		CreatedOn:       time.Now(),
		Location:        warehouseID,
		RefId:           refId,
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}

	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		openingBalance, err := getLastLedgerBalance(sessionCtx, organizationID, purchaseId, warehouseID, ledgerEntry.Origin, stockType, productID)
		if err != nil {
			return nil, err
		}
		stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
		closingBalance := openingBalance + stockDelta

		ledgerEntry.OpeningBalance = openingBalance
		ledgerEntry.TransactionBalance = quantity
		ledgerEntry.ClosingBalance = closingBalance

		if err := ProcessStockLedgerEntry(organizationID, ledgerEntry, userID); err != nil {
			return nil, err
		}

		return nil, nil
	})
	return err
}

func ProcessKernelSTockInUpdate(organizationID string, purchaseDocument map[string]interface{}, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	// var purchaseType string
	var quantity float64
	stockType := ""
	productID := ""
	factoryID := ""
	warehouseID := ""
	purchaseId := ""
	originId := ""
	productionId := ""
	filledTins := 0
	packingType := ""
	// purchaseType, _ = purchaseDocument["purchasetype"].(string)
	// if purchaseType == "" {
	// 	purchaseType = "domestic"
	// }

	stockType = "kernel"
	productID = purchaseDocument["product_id"].(string)
	factoryID = purchaseDocument["factory_id"].(string)
	packingType = purchaseDocument["type_of_packing"].(string)
	filledTins = helper.ToInt(purchaseDocument["filled_tins"])
	isfactoryWarehouse, warehID := WareHouseByFacCheck(organizationID, factoryID)
	if isfactoryWarehouse {
		warehouseID = warehID
	}
	purchaseId = purchaseDocument["purchase_id"].(string)
	purDoc, _ := GetDataById(organizationID, purchaseId, "purchase")

	packingTypeDoc, _ := GetDataById(organizationID, packingType, "lookup")

	packingValue := helper.ToInt(packingTypeDoc["value"])
	originId = purDoc["country_origin"].(string)
	totalQTY := filledTins * packingValue
	quantity = helper.ToFloat64(totalQTY)

	customerName := ""
	if companyName, ok := purchaseDocument["customer_id"].(string); ok {
		customerName = companyName
	}

	transactionDate := time.Now()
	if soldOnStr, ok := purchaseDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	productionId = purchaseDocument["_id"].(string)
	insertID := uuid.New().String()
	ledgerEntry := StockLedgerEntry{
		ID:              insertID,
		PurchaseID:      purchaseId,
		Origin:          originId,
		StockType:       stockType,
		WarehouseId:     warehouseID,
		ProductId:       productID,
		FactoryId:       factoryID,
		TransactionType: "purchase",
		TransactionDate: transactionDate,
		CustomerName:    customerName,
		Remarks:         "Purchase arrival ledger",
		CreatedBy:       userID,
		ProductionId:    productionId,
		CreatedOn:       time.Now(),
		Location:        warehouseID,
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}

	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		openingBalance, err := getProductLastLedgerBalance(sessionCtx, organizationID, productID, purchaseId, warehouseID, ledgerEntry.Origin)
		if err != nil {
			return nil, err
		}
		stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
		closingBalance := openingBalance + stockDelta

		ledgerEntry.OpeningBalance = openingBalance
		ledgerEntry.TransactionBalance = quantity
		ledgerEntry.ClosingBalance = closingBalance

		if err := ProcessStockLedgerEntry(organizationID, ledgerEntry, userID); err != nil {
			return nil, err
		}

		return nil, nil
	})
	return err
}

func ProductionKernelSTockInUpdate(organizationID string, input map[string]interface{}, userID string, avoidConsumtion bool) error {
	db := database.GetConnection(organizationID)
	client := db.Client()

	productID := helper.ToString(input["product_id"])
	factoryID := helper.ToString(input["factory_id"])
	packingType := helper.ToString(input["type_of_packing"])
	purchaseID := helper.ToString(input["purchase_id"])
	productionID := helper.ToString(input["_id"])
	// filledTins := helper.ToInt(input["filled_tins"])
	customerName := helper.ToString(input["customer_id"])

	if productID == "" || factoryID == "" || purchaseID == "" || packingType == "" {
		return errors.New("missing required fields for ledger creation")
	}

	isWarehouse, whID := WareHouseByFacCheck(organizationID, factoryID)
	if !isWarehouse {
		return errors.New("no warehouse mapped to this factory")
	}
	warehouseID := whID

	purchaseDoc, _ := GetDataById(organizationID, purchaseID, "purchase")
	originID := helper.ToString(purchaseDoc["country_origin"])

	// packingDoc, _ := GetDataById(organizationID, packingType, "lookup")
	// packingValue := helper.ToInt(packingDoc["value"])
	totalQty := float64(input["weight"].(float64))

	transactionDate := time.Now()
	if dt, ok := input["created_on"]; ok {
		transactionDate = helper.ParseDate(dt)
	}

	// Check if ledger entry exists for this production
	ledgerFilter := LedgerFilter{
		ProductID:   productID,
		PurchaseID:  purchaseID,
		Origin:      originID,
		WarehouseID: warehouseID,
		RefId:       productionID,
	}

	exists, _ := ledgerEntryExists(organizationID, ledgerFilter)
	if exists {
		return UpdateLedgerEntryByIDs(organizationID, ledgerFilter, totalQty, userID, "KERNEL")
	}

	entry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         purchaseID,
		Origin:             originID,
		StockType:          "kernel",
		WarehouseId:        warehouseID,
		ProductId:          productID,
		FactoryId:          factoryID,
		ProductionId:       productionID,
		TransactionType:    "KERNEL",
		TransactionDate:    transactionDate,
		CustomerName:       customerName,
		Remarks:            "Packed Kernel",
		CreatedOn:          time.Now(),
		CreatedBy:          userID,
		ProcessType:        "KERNEL",
		Location:           warehouseID,
		TransactionBalance: -totalQty,
		RefId:              productionID,
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(ctx mongo.SessionContext) (interface{}, error) {
		// First update the stock
		gradFilter := bson.M{
			"product_id":   productID,
			"process_type": "GRAD",
			"purchase_id":  purchaseID,
			"factory_id":   factoryID,
			"warehouse_id": warehouseID,
			"origin":       originID,
		}
		
		gradUpdate := bson.M{
			"$inc": bson.M{"available_qty": -totalQty},
			"$set": bson.M{
				"last_updated_by": userID,
				"last_updated_on": time.Now(),
			},
		}
		db.Collection("stock_in_hand").UpdateOne(ctx, gradFilter, gradUpdate)
		
		// Then get the updated stock to calculate opening balance
		var last StockBalance
		opts := options.FindOne().SetSort(bson.M{"created_on": -1})
		findErr := db.Collection("stock_in_hand").FindOne(ctx, gradFilter, opts).Decode(&last)

		openingBalance := 0.0
		if findErr == nil {
			// Opening balance = current stock (after update) - transaction
			// Transaction is -totalQty (consumption), so we subtract it (which adds back)
			openingBalance = last.AvailableQty - (-totalQty)
		}

		entry.OpeningBalance = openingBalance
		entry.ClosingBalance = openingBalance + entry.TransactionBalance

		if err := ProcessProductionKernelStock(organizationID, entry, userID, avoidConsumtion); err != nil {
			return nil, err
		}

		return nil, nil
	})

	return err
}

func ProcessProductionKernelStock(organizationID string, ledger StockLedgerEntry, userID string, avoidConsumtion bool) error {
	db := database.GetConnection(organizationID)
	ctx := context.Background()

	// Insert consumption ledger entry
	if !avoidConsumtion {
		if _, err := helper.InsertDataDb(organizationID, ledger, "stock_ledger"); err != nil {
			return err
		}
	}

	updateOpts := options.Update().SetUpsert(true)

	// 1️⃣ Reduce GRAD WIP stock
	gradFilter := bson.M{
		"product_id":   ledger.ProductId,
		"purchase_id":  ledger.PurchaseID,
		"factory_id":   ledger.FactoryId,
		"warehouse_id": ledger.WarehouseId,
		"origin":       ledger.Origin,
		"stock_type":   "WIP",
		"process_type": "GRAD",
	}

	gradUpdate := bson.M{
		"$inc": bson.M{"available_qty": ledger.TransactionBalance},
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

	if !avoidConsumtion {
		_, err := db.Collection("stock_in_hand").UpdateOne(ctx, gradFilter, gradUpdate, updateOpts)
		if err != nil {
			return err
		}
	}

	// 2️⃣ Increase packed kernel stock
	kernelFilter := bson.M{
		"product_id":   ledger.ProductId,
		"purchase_id":  ledger.PurchaseID,
		"factory_id":   ledger.FactoryId,
		"warehouse_id": ledger.WarehouseId,
		"origin":       ledger.Origin,
		"stock_type":   "kernel",
	}

	kernelUpdate := bson.M{
		"$inc": bson.M{"available_qty": -ledger.TransactionBalance},
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
	_, err1 := db.Collection("stock_in_hand").UpdateOne(ctx, kernelFilter, kernelUpdate, updateOpts)
	if err1 != nil {
		return err1
	}

	// 3️⃣ Create packed kernel ledger entry
	packedEntry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         ledger.PurchaseID,
		Origin:             ledger.Origin,
		StockType:          "kernel",
		WarehouseId:        ledger.WarehouseId,
		ProductId:          ledger.ProductId,
		FactoryId:          ledger.FactoryId,
		ProductionId:       ledger.ProductionId,
		TransactionType:    "production",
		TransactionDate:    ledger.TransactionDate,
		CustomerName:       ledger.CustomerName,
		Remarks:            "Packed Kernel Production",
		CreatedOn:          time.Now(),
		CreatedBy:          userID,
		ProcessType:        "PACK",
		Location:           ledger.WarehouseId,
		TransactionBalance: -ledger.TransactionBalance,
		RefId:              ledger.RefId,
	}

	var kernelStock StockBalance
	findErr := db.Collection("stock_in_hand").FindOne(ctx, kernelFilter).Decode(&kernelStock)

	packedEntry.OpeningBalance = 0.0
	if findErr == nil {
		packedEntry.OpeningBalance = kernelStock.AvailableQty - (-ledger.TransactionBalance)
	}
	packedEntry.ClosingBalance = packedEntry.OpeningBalance + packedEntry.TransactionBalance

	if _, err := helper.InsertDataDb(organizationID, packedEntry, "stock_ledger"); err != nil {
		return err
	}

	return nil
}

func ProcessRCNCooking(organizationID string, purchaseDocument map[string]interface{}, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	stockType := "RCN"
	productID := "RCN"
	warehouseID := ""
	purchaseId := ""
	factoryID := purchaseDocument["factory_id"].(string)
	originId := ""
	var quantity float64
	filledTins := helper.ToInt(purchaseDocument["filled_tins"])
	productionId := ""
	isfactoryWarehouse, warehID := WareHouseByFacCheck(organizationID, factoryID)
	if isfactoryWarehouse {
		warehouseID = warehID
	}
	purchaseId = purchaseDocument["purchase_id"].(string)
	purDoc, _ := GetDataById(organizationID, purchaseId, "purchase")

	if origin, ok := purDoc["country_origin"].(string); ok {
		originId = origin
	}
	quantity = helper.ToFloat64(filledTins)

	customerName := ""
	if companyName, ok := purchaseDocument["customer_id"].(string); ok {
		customerName = companyName
	}
	productionId = purchaseDocument["_id"].(string)
	insertID := uuid.New().String()

	transactionDate := time.Now()
	if soldOnStr, ok := purchaseDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}
	ledgerEntry := StockLedgerEntry{
		ID:              insertID,
		PurchaseID:      purchaseId,
		Origin:          originId,
		StockType:       stockType,
		WarehouseId:     warehouseID,
		ProductId:       productID,
		FactoryId:       factoryID,
		TransactionType: "production",
		TransactionDate: transactionDate,
		CustomerName:    customerName,
		Remarks:         "production ",
		CreatedBy:       userID,
		ProductionId:    productionId,
		CreatedOn:       time.Now(),
		Location:        warehouseID,
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}

	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		openingBalance, err := getProductLastLedgerBalance(sessionCtx, organizationID, productID, purchaseId, warehouseID, ledgerEntry.Origin)
		if err != nil {
			return nil, err
		}
		stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
		closingBalance := openingBalance + stockDelta

		ledgerEntry.OpeningBalance = openingBalance
		ledgerEntry.TransactionBalance = quantity
		ledgerEntry.ClosingBalance = closingBalance

		if err := ProcessStockLedgerEntry(organizationID, ledgerEntry, userID); err != nil {
			return nil, err
		}

		return nil, nil
	})
	return err
}

func WareHouseCheck(orgId string, wareHouseId string) (bool, string) {
	var warehouse map[string]interface{}
	var factoryId string
	err := database.GetConnection(orgId).Collection("company").FindOne(
		context.Background(),
		bson.M{"_id": wareHouseId},
	).Decode(&warehouse)

	if err != nil {
		return false, ""
	}

	if warehouse["factory_id"] == nil || warehouse["factory_id"] == "" {
		return false, ""
	} else {
		factoryId = warehouse["factory_id"].(string)
	}

	return true, factoryId

}

func GetPurchaseDetail(orgId string, purchase_id string) (bool, string) {
	var purchase map[string]interface{}
	var originId string
	err := database.GetConnection(orgId).Collection("purchase").FindOne(
		context.Background(),
		bson.M{"_id": purchase_id},
	).Decode(&purchase)

	if err != nil {
		return false, ""
	}

	if purchase["country_origin"] == nil || purchase["country_origin"] == "" {
		return false, ""
	} else {
		originId = purchase["country_origin"].(string)
	}

	return true, originId

}

func GetSaleDetail(orgId string, sale_id string) (bool, string) {
	var sale map[string]interface{}
	var customerId string
	err := database.GetConnection(orgId).Collection("sale").FindOne(
		context.Background(),
		bson.M{"_id": sale_id},
	).Decode(&sale)

	if err != nil {
		return false, ""
	}

	if sale["customer_id"] == nil || sale["customer_id"] == "" {
		return false, ""
	} else {
		customerId = sale["customer_id"].(string)
	}

	return true, customerId

}

func GetProcessDetail(orgId string, processID int) (bool, string) {
	var process map[string]interface{}
	var process_id string
	err := database.GetConnection(orgId).Collection("process").FindOne(
		context.Background(),
		bson.M{"process_id": processID},
	).Decode(&process)

	if err != nil {
		return false, ""
	}

	if process["_id"] == nil || process["_id"] == "" {
		return false, ""
	} else {
		process_id = process["_id"].(string)
	}

	return true, process_id

}

func GetProductionWarehouse(orgId string, production_id string) (bool, string) {
	var production map[string]interface{}
	var warehouseID string
	err := database.GetConnection(orgId).Collection("productions").FindOne(
		context.Background(),
		bson.M{"_id": production_id},
	).Decode(&production)

	if err != nil {
		return false, ""
	}

	if production["warehouse_id"] == nil || production["warehouse_id"] == "" {
		return false, ""
	} else {
		warehouseID = production["warehouse_id"].(string)
	}

	return true, warehouseID
}

func WareHouseByFacCheck(orgId string, factoryId string) (bool, string) {
	var warehouse map[string]interface{}
	var warehouseId string
	err := database.GetConnection(orgId).Collection("company").FindOne(
		context.Background(),
		bson.M{"factory_id": factoryId},
	).Decode(&warehouse)

	if err != nil {
		return false, ""
	}

	if warehouse["_id"] == nil || warehouse["_id"] == "" {
		return false, ""
	} else {
		warehouseId = warehouse["_id"].(string)
	}

	return true, warehouseId

}

func ProcessKernelSale(organizationID string, saleDocument map[string]interface{}, userID string, i int) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	var quantity float64
	if quantityFloat, ok := saleDocument["stock_quantity"].(float64); ok {
		quantity = quantityFloat
	} else if quantityInt, ok := saleDocument["stock_quantity"].(int); ok {
		quantity = float64(quantityInt)
	}
	if quantity == 0 {
		return errors.New("kernel sale: missing or zero quantity")
	}

	stockType := "kernel"
	productID := ""
	factoryID := ""
	if productValue, ok := saleDocument["product_id"].(string); ok && productValue != "" {
		productID = productValue
	}

	origin := ""
	if _, ok := saleDocument["purchase_id"].(string); ok {
		isPurchase, originVal := GetPurchaseDetail(organizationID, saleDocument["purchase_id"].(string))
		if isPurchase {
			origin = originVal
		} else {
			origin = saleDocument["origin_id"].(string)
		}
	}

	customerName := ""
	if _, ok := saleDocument["template_id"].(string); ok {
		isSale, originVal := GetSaleDetail(organizationID, saleDocument["template_id"].(string))
		if isSale {
			customerName = originVal
		}
	}

	warehouseID := ""
	if _, ok := saleDocument["production_id"].(string); ok {
		isWarehouse, originVal := GetProductionWarehouse(organizationID, saleDocument["production_id"].(string))
		if isWarehouse {
			warehouseID = originVal
		}
	} else {
		if _, ok := saleDocument["warehouse_id"].(string); ok {
			warehouseID = saleDocument["warehouse_id"].(string)
		} else {
			warehouseID = ""
		}
	}

	transactionDate := time.Now()
	if soldOnStr, ok := saleDocument["sold_on"].(string); ok {
		if parsedTime, err := time.Parse(time.RFC3339, soldOnStr); err == nil {
			transactionDate = parsedTime
		}
	}
	insertID := uuid.New().String()
	ledgerEntry := StockLedgerEntry{
		ID:                 insertID,
		PurchaseID:         saleDocument["purchase_id"].(string),
		Origin:             origin,
		StockType:          stockType,
		WarehouseId:        warehouseID,
		ProductId:          productID,
		FactoryId:          factoryID,
		TransactionType:    "sale",
		SaleId:             saleDocument["sale_id"].(string),
		TransactionDate:    transactionDate,
		CustomerName:       customerName,
		Remarks:            "Sale ledger",
		CreatedBy:          userID,
		CreatedOn:          time.Now(),
		TransactionBalance: quantity,
		Location:           warehouseID,
		RefId:              saleDocument["sale_id"].(string),
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		// Fetch the latest balance with read concern to ensure we get the most recent committed data
		openingBalance, err := getLastLedgerBalance(sessionCtx, organizationID, saleDocument["purchase_id"].(string), warehouseID, ledgerEntry.Origin, stockType, productID)
		if err != nil {
			return nil, err
		}
		stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
		closingBalance := openingBalance + stockDelta

		if closingBalance < 0 {
			return nil, fmt.Errorf("insufficient stock for sale: opening balance %.2f, quantity %.2f, closing balance %.2f", openingBalance, quantity, closingBalance)
		}

		ledgerEntry.OpeningBalance = openingBalance
		ledgerEntry.TransactionBalance = quantity
		ledgerEntry.ClosingBalance = closingBalance

		if err := ProcessStockLedgerEntry(organizationID, ledgerEntry, userID); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

// ProcessKernelSaleBatch processes multiple kernel sale items in a single transaction
// This ensures the available balance is correctly updated for each subsequent item
func ProcessKernelSaleBatch(organizationID string, saleDocuments []map[string]interface{}, userID string) error {
	if len(saleDocuments) == 0 {
		return nil
	}

	db := database.GetConnection(organizationID)
	client := db.Client()

	// Create session with read concern majority to ensure we read committed data
	sessionOpts := options.Session().SetDefaultReadConcern(readconcern.Majority())
	session, err := client.StartSession(sessionOpts)
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	// Transaction options with retry on write conflicts
	txnOpts := options.Transaction().
		SetReadConcern(readconcern.Majority()).
		SetWriteConcern(writeconcern.New(writeconcern.WMajority()))

	// Retry logic for write conflicts
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		// Track running balance per purchase/warehouse/origin/product combination
		balanceCache := make(map[string]float64)
		balanceInitialized := make(map[string]bool)
		
		// Track stock_in_hand updates to batch them at the end
		stockUpdates := make(map[string]float64) // key -> total delta

		for i, saleDocument := range saleDocuments {
			var quantity float64
			if quantityFloat, ok := saleDocument["stock_quantity"].(float64); ok {
				quantity = quantityFloat
			} else if quantityInt, ok := saleDocument["stock_quantity"].(int); ok {
				quantity = float64(quantityInt)
			}
			if quantity == 0 {
				return nil, fmt.Errorf("item %d: missing or zero quantity", i)
			}

			stockType := "kernel"
			productID := ""
			if productValue, ok := saleDocument["product_id"].(string); ok && productValue != "" {
				productID = productValue
			}

			origin := ""
			if _, ok := saleDocument["purchase_id"].(string); ok {
				isPurchase, originVal := GetPurchaseDetail(organizationID, saleDocument["purchase_id"].(string))
				if isPurchase {
					origin = originVal
				} else if originID, ok := saleDocument["origin_id"].(string); ok {
					origin = originID
				}
			}

			customerName := ""
			if _, ok := saleDocument["template_id"].(string); ok {
				isSale, originVal := GetSaleDetail(organizationID, saleDocument["template_id"].(string))
				if isSale {
					customerName = originVal
				}
			}

			warehouseID := ""
			if _, ok := saleDocument["production_id"].(string); ok {
				isWarehouse, originVal := GetProductionWarehouse(organizationID, saleDocument["production_id"].(string))
				if isWarehouse {
					warehouseID = originVal
				}
			} else if wID, ok := saleDocument["warehouse_id"].(string); ok {
				warehouseID = wID
			}

			transactionDate := time.Now()
			if soldOnStr, ok := saleDocument["sold_on"].(string); ok {
				if parsedTime, err := time.Parse(time.RFC3339, soldOnStr); err == nil {
					transactionDate = parsedTime
				}
			}

			purchaseID := saleDocument["purchase_id"].(string)
			
			// Create cache key for this stock combination
			cacheKey := fmt.Sprintf("%s|%s|%s|%s|%s", purchaseID, warehouseID, origin, stockType, productID)

			// Get opening balance - either from cache (previous item in this batch) or from DB
			var openingBalance float64
			if balanceInitialized[cacheKey] {
				// Use the closing balance from the previous item in this batch
				openingBalance = balanceCache[cacheKey]
			} else {
				// First item for this stock combination - fetch from DB
				openingBalance, err = getLastLedgerBalance(sessionCtx, organizationID, purchaseID, warehouseID, origin, stockType, productID)
				if err != nil {
					return nil, fmt.Errorf("item %d: failed to get balance: %v", i, err)
				}
				balanceInitialized[cacheKey] = true
			}

			stockDelta := calculateStockDelta("sale", quantity)
			closingBalance := openingBalance + stockDelta

			if closingBalance < 0 {
				return nil, fmt.Errorf("item %d: insufficient stock - opening: %.2f, quantity: %.2f, closing: %.2f", i, openingBalance, quantity, closingBalance)
			}

			// Create ledger entry
			insertID := uuid.New().String()
			ledgerEntry := StockLedgerEntry{
				ID:                 insertID,
				PurchaseID:         purchaseID,
				Origin:             origin,
				StockType:          stockType,
				WarehouseId:        warehouseID,
				ProductId:          productID,
				FactoryId:          "",
				TransactionType:    "sale",
				SaleId:             saleDocument["sale_id"].(string),
				TransactionDate:    transactionDate,
				CustomerName:       customerName,
				Remarks:            fmt.Sprintf("Sale ledger - item %d", i+1),
				CreatedBy:          userID,
				CreatedOn:          time.Now(),
				OpeningBalance:     openingBalance,
				TransactionBalance: quantity,
				ClosingBalance:     closingBalance,
				Location:           warehouseID,
				RefId:              saleDocument["sale_id"].(string),
			}

			// Insert ledger entry (without updating stock_in_hand yet)
			helper.UpdateDateObject(ledgerEntry)
			_, err := database.GetConnection(organizationID).Collection("stock_ledger").InsertOne(sessionCtx, ledgerEntry)
			if err != nil {
				return nil, fmt.Errorf("item %d: failed to insert ledger: %v", i, err)
			}

			// Accumulate stock update for this combination
			stockUpdates[cacheKey] += stockDelta

			// Update cache with new closing balance for next item
			balanceCache[cacheKey] = closingBalance
		}

		// Now batch update all stock_in_hand records at once
		for cacheKey, totalDelta := range stockUpdates {
			parts := strings.Split(cacheKey, "|")
			if len(parts) != 5 {
				continue
			}
			purchaseID := parts[0]
			warehouseID := parts[1]
			origin := parts[2]
			stockType := parts[3]
			productID := parts[4]

			err := updateStockBalance(sessionCtx, organizationID, origin, stockType, warehouseID, "", purchaseID, productID, totalDelta, userID, "", "sale")
			if err != nil {
				return nil, fmt.Errorf("failed to update stock balance for %s: %v", cacheKey, err)
			}
		}

		return nil, nil
	}, txnOpts)

		// Check if we should retry
		if err == nil {
			return nil // Success
		}

		// Check if it's a write conflict error
		if mongo.IsTimeout(err) || strings.Contains(err.Error(), "WriteConflict") {
			if attempt < maxRetries {
				// Exponential backoff: 10ms, 20ms, 40ms
				backoff := time.Duration(10*(1<<uint(attempt))) * time.Millisecond
				log.Printf("Write conflict on attempt %d, retrying after %v: %v", attempt+1, backoff, err)
				time.Sleep(backoff)
				continue
			}
		}

		// Non-retryable error or max retries exceeded
		return err
	}

	return err
}

func ProcessSale(organizationID string, saleDocument map[string]interface{}, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	var quantity float64
	if quantityFloat, ok := saleDocument["quantity"].(float64); ok {
		quantity = quantityFloat
	} else if quantityInt, ok := saleDocument["quantity"].(int); ok {
		quantity = float64(quantityInt)
	}
	if quantity == 0 {
		return errors.New("sale: missing or zero quantity")
	}

	stockType := "RCN"
	productID := "RCN"
	factoryID := ""
	if productValue, ok := saleDocument["product_id"].(string); ok && productValue != "" {
		stockType = "kernel"
		productID = productValue
	}

	origin := ""
	if originValue, ok := saleDocument["origin"].(string); ok {
		origin = originValue
	}

	warehouseID := ""
	if warehouseValue, ok := saleDocument["warehouse"].(string); ok {
		warehouseID = warehouseValue
	}
	if warehouseID != "" {
		isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}
	}

	customerName := ""
	if customerValue, ok := saleDocument["customer_id"].(string); ok {
		customerName = customerValue
	} else if customerValue, ok := saleDocument["customer_name"].(string); ok {
		customerName = customerValue
	}

	transactionDate := time.Now()
	if soldOnStr, ok := saleDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	ledgerEntry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         saleDocument["purchase_id"].(string),
		Origin:             origin,
		StockType:          stockType,
		WarehouseId:        warehouseID,
		ProductId:          productID,
		FactoryId:          factoryID,
		TransactionType:    "sale",
		SaleId:             saleDocument["_id"].(string),
		TransactionDate:    transactionDate,
		CustomerName:       customerName,
		Remarks:            "Sale ledger",
		CreatedBy:          userID,
		CreatedOn:          time.Now(),
		TransactionBalance: quantity,
		Location:           warehouseID,
		RefId:              saleDocument["_id"].(string),
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		openingBalance, err := getLastLedgerBalance(sessionCtx, organizationID, saleDocument["purchase_id"].(string), saleDocument["warehouse"].(string), ledgerEntry.Origin, stockType, productID)
		if err != nil {
			return nil, err
		}
		stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
		closingBalance := openingBalance + stockDelta

		if closingBalance < 0 {
			return nil, errors.New("insufficient stock for sale")
		}

		ledgerEntry.OpeningBalance = openingBalance
		ledgerEntry.TransactionBalance = quantity
		ledgerEntry.ClosingBalance = closingBalance

		if err := processStockLedgerEntryWithSession(sessionCtx, organizationID, ledgerEntry, userID); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func ProcessJobwork(organizationID string, jobworkdDocument map[string]interface{}, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	factoryID := ""
	warehouseID := ""
	customerName := ""
	countryOrigin := ""
	purchaseID := ""
	quantity := 0.0
	transactionType := ""
	stockType := ""
	productId := ""
	processID := ""
	refId := ""

	productIdValue, ok := jobworkdDocument["product_id"].(string)
	if ok {
		productId = productIdValue
	}

	jobIdValue, ok := jobworkdDocument["job_id"].(string)
	if ok {
		refId = jobIdValue
	}

	processIDValue, ok := jobworkdDocument["process_id"].(string)
	if ok {
		processID = processIDValue
		stockType = getOutputProduct(processID)
	}
	// if processType == "production" {
	// 	isProcess, originVal := GetProcessDetail(organizationID, jobworkdDocument["process_id"].(int))
	// 	if isProcess {
	// 		processID = originVal
	// 		stockType = getOutputProduct(processID)
	// 	}
	// } else if processType == "rcn" {
	// 	stockType = "rcn"
	// } else {
	// 	stockType = "kernel"
	// }

	jobworkType := jobworkdDocument["jobwork_type"].(string)
	if jobworkType == "inWard-jobWork" {
		transactionType = "inWard-jobWork"
	} else if jobworkType == "outWard-jobWork" {
		transactionType = "outWard-jobWork"
	}

	if quantityValue, ok := jobworkdDocument["quantity"].(float64); ok {
		quantity = quantityValue
	}

	if purchaseIDValue, ok := jobworkdDocument["purchase_id"].(string); ok {
		purchaseID = purchaseIDValue
	}
	if factoryValue, ok := jobworkdDocument["factory_id"].(string); ok {
		factoryID = factoryValue
	}

	if companyName, ok := jobworkdDocument["customer_id"].(string); ok {
		customerName = companyName
	}
	if warehouseValue, ok := jobworkdDocument["warehouse_id"].(string); ok {
		warehouseID = warehouseValue
	}
	if countryOriginValue, ok := jobworkdDocument["country_origin"].(string); ok {
		countryOrigin = countryOriginValue
	}
	transactionDate := time.Now()
	if soldOnStr, ok := jobworkdDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}
	var ledgreID = uuid.New().String()
	ledgerEntry := StockLedgerEntry{
		ID:              ledgreID,
		PurchaseID:      purchaseID,
		Origin:          countryOrigin,
		StockType:       stockType,
		WarehouseId:     warehouseID,
		ProductId:       productId,
		FactoryId:       factoryID,
		TransactionType: transactionType,
		TransactionDate: transactionDate,
		CustomerName:    customerName,
		Remarks:         "jobwork ledger",
		CreatedBy:       userID,
		CreatedOn:       time.Now(),
		Location:        "JOBWORK",
		RefId:           refId,
		ProcessType:     "Jobwork",
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		openingBalance, err := getLastLedgerBalance(sessionCtx, organizationID, purchaseID, warehouseID, countryOrigin, stockType, productId)
		if err != nil {
			return nil, err
		}
		stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
		closingBalance := 0.0
		closingBalance = openingBalance + stockDelta

		// Check for insufficient stock on outward jobwork
		if transactionType == "outWard-jobWork" && closingBalance < 0 {
			return nil, errors.New("insufficient stock for jobwork transfer. Available: " + fmt.Sprintf("%.2f", openingBalance) + ", Required: " + fmt.Sprintf("%.2f", quantity))
		}

		ledgerEntry.OpeningBalance = openingBalance
		ledgerEntry.TransactionBalance = quantity
		ledgerEntry.ClosingBalance = closingBalance

		ledgerUpdateFilter := LedgerFilter{
			ProductID:   productId,
			PurchaseID:  purchaseID,
			Origin:      countryOrigin,
			RefId:       refId,
			WarehouseID: warehouseID,
		}

		exists, _ := ledgerEntryExists(organizationID, ledgerUpdateFilter)
		if exists {
			go UpdateLedgerEntryByIDs(organizationID, ledgerUpdateFilter, quantity, userID, productId)
		} else {
			if err := ProcessStockLedgerEntry(organizationID, ledgerEntry, userID); err != nil {
				return nil, err
			}
		}
		return nil, nil

	})
	return err
}

func ProcessInwardJobwork(organizationID string, jobworkTemplate map[string]interface{}, jobworkdDocument map[string]interface{}, userID string, processType string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	factoryID := ""
	warehouseID := ""
	customerName := ""
	countryOrigin := ""
	purchaseID := ""
	quantity := 0.0
	transactionType := ""
	// stockType := "WIP"
	refId := ""
	// processID := ""

	jobIdValue, ok := jobworkdDocument["job_id"].(string)
	if ok {
		refId = jobIdValue
	}
	jobworkType := jobworkdDocument["jobwork_type"].(string)
	if jobworkType == "inWard-jobWork" {
		transactionType = "inWard-jobWork"
	} else if jobworkType == "outWard-jobWork" {
		transactionType = "outWard-jobWork"
	}
	// if quantityValue, ok := jobworkdDocument["quantity"].(float64); ok {
	// 	quantity = quantityValue
	// }

	if purchaseIDValue, ok := jobworkdDocument["purchase_id"].(string); ok {
		purchaseID = purchaseIDValue
	}
	if factoryValue, ok := jobworkdDocument["factory_id"].(string); ok {
		factoryID = factoryValue
	}

	if companyName, ok := jobworkdDocument["customer_id"].(string); ok {
		customerName = companyName
	}
	if warehouseValue, ok := jobworkdDocument["warehouse_id"].(string); ok {
		warehouseID = warehouseValue
	} else {
		var jobworkDoc map[string]interface{}
		filter := bson.M{"_id": jobworkdDocument["parent_template_id"].(string)}
		err := database.Collection("job_work").FindOne(context.Background(), filter).Decode(&jobworkDoc)
		if err == nil {
			if whID, ok := jobworkDoc["warehouse_id"].(string); ok {
				warehouseID = whID
			}
		}
	}
	if countryOriginValue, ok := jobworkdDocument["country_origin"].(string); ok {
		countryOrigin = countryOriginValue
	}

	transactionDate := time.Now()
	if soldOnStr, ok := jobworkTemplate["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	jobworkInwardProducts := helper.ToStringSlice(jobworkTemplate["product_id"])

	for _, product := range jobworkInwardProducts {
		quantity = helper.ToFloat64(jobworkdDocument[product])
		refId = jobworkdDocument["_id"].(string) + product
		ledgerUpdateFilter := LedgerFilter{
			ProductID:   product,
			PurchaseID:  purchaseID,
			Origin:      countryOrigin,
			RefId:       refId,
			WarehouseID: warehouseID,
		}

		exists, _ := ledgerEntryExists(organizationID, ledgerUpdateFilter)
		if exists {
			go UpdateLedgerEntryByIDs(organizationID, ledgerUpdateFilter, quantity, userID, product)
		} else {
			ledgerEntry := StockLedgerEntry{
				ID:              uuid.New().String(),
				PurchaseID:      purchaseID,
				Origin:          countryOrigin,
				StockType:       processType,
				WarehouseId:     warehouseID,
				ProductId:       product,
				FactoryId:       factoryID,
				TransactionType: transactionType,
				TransactionDate: transactionDate,
				CustomerName:    customerName,
				Remarks:         "jobwork ledger",
				CreatedBy:       userID,
				CreatedOn:       time.Now(),
				Location:        processType,
				RefId:           refId,
				ProcessType:     processType,
			}

			session, err := client.StartSession()
			if err != nil {
				return err
			}
			defer session.EndSession(context.Background())

			_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
				openingBalance, err := getLastLedgerBalance(sessionCtx, organizationID, purchaseID, warehouseID, countryOrigin, processType, product)
				if err != nil {
					return nil, err
				}
				stockDelta := calculateStockDelta(ledgerEntry.TransactionType, quantity)
				closingBalance := 0.0
				if jobworkType == "inWard-jobWork" {
					closingBalance = openingBalance + stockDelta
				} else if jobworkType == "outWard-jobWork" {
					closingBalance = openingBalance - stockDelta
				}

				ledgerEntry.OpeningBalance = openingBalance
				ledgerEntry.TransactionBalance = quantity
				ledgerEntry.ClosingBalance = closingBalance

				if err := ProcessStockLedgerEntry(organizationID, ledgerEntry, userID); err != nil {
					return nil, err
				}
				return nil, nil
			})
		}

	}

	return nil
}

func GetPurchaseLedgerRequest(organizationID string, purchaseType string, purchaseDocument map[string]interface{}, wareHouseDocument map[string]interface{}, userID string, stockMovementId string) (StockLedgerEntry, error) {
	var quantity float64
	stockType := "RCN"
	productID := ""
	factoryID := ""
	warehouseID := ""
	purchaseId := ""
	originId := ""
	refId := ""
	if purchaseType == "domestic" {
		// For domestic purchases, wareHouseDocument is invoice_details
		if cnoTo, ok := wareHouseDocument["cno_to"].(string); ok && cnoTo == "Warehouse" {
			if whID, ok := wareHouseDocument["warehouse_id"].(string); ok {
				warehouseID = whID
				isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
				if isfactoryWarehouse {
					factoryID = factoryId
				}
			}
		}

		if invoiceQuantity, ok := wareHouseDocument["quantity"].(float64); ok {
			quantity = invoiceQuantity
		}

		if quantity == 0 {
			return StockLedgerEntry{}, errors.New("domestic purchase: missing or zero quantity")
		}
		purchaseId = purchaseDocument["_id"].(string)
		originId = purchaseDocument["country_origin"].(string)
		productID = "RCN"
		refId = stockMovementId
	} else if purchaseType == "International" {
		refId = stockMovementId
		warehouseID = wareHouseDocument["warehouse"].(string)

		isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}

		if netWeight, ok := wareHouseDocument["weight"].(float64); ok {
			quantity = netWeight
		}

		if quantity == 0 {
			return StockLedgerEntry{}, errors.New("purchase: missing or zero net_weight in quality_reports")
		}
		purchaseId = purchaseDocument["_id"].(string)
		originId = purchaseDocument["country_origin"].(string)
		productID = "RCN"
	} else if purchaseType == "kernel" {
		refId = stockMovementId
		stockType = "kernel"
		productID = purchaseDocument["product_id"].(string)
		warehouseID = purchaseDocument["warehouse"].(string)
		purchaseDocument["country_origin"] = purchaseDocument["origin"].(string)
		isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}
		purchaseId = purchaseDocument["template_id"].(string)
		originId = purchaseDocument["origin"].(string)
		quantity = helper.ToFloat64(purchaseDocument["quantity"])
	}

	customerName := ""
	if companyName, ok := purchaseDocument["customer_id"].(string); ok {
		customerName = companyName
	}
	transactionDate := time.Now()
	if soldOnStr, ok := purchaseDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	insertID := uuid.New().String()
	ledgerEntry := StockLedgerEntry{
		ID:              insertID,
		PurchaseID:      purchaseId,
		Origin:          originId,
		StockType:       stockType,
		WarehouseId:     warehouseID,
		ProductId:       productID,
		FactoryId:       factoryID,
		TransactionType: "purchase",
		TransactionDate: transactionDate,
		CustomerName:    customerName,
		Remarks:         "Purchase arrival ledger",
		CreatedBy:       userID,
		CreatedOn:       time.Now(),
		Location:        warehouseID,
		RefId:           refId,
	}
	return ledgerEntry, nil
}

func GetSaleLedgerRequest(organizationID string, saleDocument map[string]interface{}, userID string) (StockLedgerEntry, error) {
	var quantity float64
	if quantityFloat, ok := saleDocument["quantity"].(float64); ok {
		quantity = quantityFloat
	} else if quantityInt, ok := saleDocument["quantity"].(int); ok {
		quantity = float64(quantityInt)
	}
	if quantity == 0 {
		return StockLedgerEntry{}, errors.New("sale: missing or zero quantity")
	}

	stockType := "RCN"
	productID := "RCN"
	factoryID := ""
	if productValue, ok := saleDocument["product_id"].(string); ok && productValue != "" {
		stockType = "kernel"
		productID = productValue
	}

	origin := ""
	if originValue, ok := saleDocument["origin"].(string); ok {
		origin = originValue
	}

	warehouseID := ""
	if warehouseValue, ok := saleDocument["warehouse"].(string); ok {
		warehouseID = warehouseValue
	}
	if warehouseID != "" {
		isfactoryWarehouse, factoryId := WareHouseCheck(organizationID, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}
	}

	customerName := ""
	if customerValue, ok := saleDocument["customer_id"].(string); ok {
		customerName = customerValue
	} else if customerValue, ok := saleDocument["customer_name"].(string); ok {
		customerName = customerValue
	}

	transactionDate := time.Now()
	if soldOnStr, ok := saleDocument["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	ledgerEntry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         saleDocument["purchase_id"].(string),
		Origin:             origin,
		StockType:          stockType,
		WarehouseId:        warehouseID,
		ProductId:          productID,
		FactoryId:          factoryID,
		TransactionType:    "sale",
		SaleId:             saleDocument["_id"].(string),
		TransactionDate:    transactionDate,
		CustomerName:       customerName,
		Remarks:            "Sale ledger",
		CreatedBy:          userID,
		CreatedOn:          time.Now(),
		TransactionBalance: quantity,
		Location:           warehouseID,
		RefId:              saleDocument["_id"].(string),
	}
	return ledgerEntry, nil
}

func ProcessStockTransafer(organizationID string, stock map[string]interface{}, userID string) error {
	db := database.GetConnection(organizationID)
	client := db.Client()

	transferID := helper.ToString(stock["_id"])
	warehouseFrom := helper.ToString(stock["warehouse_from"])
	warehouseTo := helper.ToString(stock["warehouse_to"])
	transferWeight := helper.ToFloat64(stock["transfer_weight"])
	stockID := helper.ToString(stock["stock_id"])
	productID := helper.ToString(stock["product_id"])
	purchaseID := helper.ToString(stock["purchase_id"])

	if transferID == "" || warehouseFrom == "" || warehouseTo == "" || transferWeight == 0 || stockID == "" || productID == "" {
		return errors.New("missing required fields for stock transfer")
	}

	// Get factory ID and origin for source warehouse
	isFactoryWarehouseFrom, factoryIDFrom := WareHouseCheck(organizationID, warehouseFrom)
	if !isFactoryWarehouseFrom {
		factoryIDFrom = "" // Not a factory warehouse
	}
	purchaseDocFrom, _ := GetDataById(organizationID, purchaseID, "purchase")
	originFrom := helper.ToString(purchaseDocFrom["country_origin"])
	productDoc, _ := GetSHStockTypeByID(organizationID, stockID, "stock_in_hand")
	productType := helper.ToString(productDoc["stock_type"])

	// Get factory ID and origin for destination warehouse
	isFactoryWarehouseTo, factoryIDTo := WareHouseCheck(organizationID, warehouseTo)
	if !isFactoryWarehouseTo {
		factoryIDTo = "" // Not a factory warehouse
	}
	purchaseDocTo, _ := GetDataById(organizationID, purchaseID, "purchase")
	originTo := helper.ToString(purchaseDocTo["country_origin"])

	transactionDate := time.Now()
	if createdOnStr, ok := stock["created_on"]; ok {
		transactionDate = helper.ParseDate(createdOnStr)
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {

		ledgerFromUpdateFilter := LedgerFilter{
			ProductID:   productID,
			PurchaseID:  purchaseID,
			Origin:      originFrom,
			RefId:       transferID,
			WarehouseID: warehouseFrom,
		}

		exists, _ := ledgerEntryExists(organizationID, ledgerFromUpdateFilter)
		if exists {
			go UpdateLedgerEntryByIDs(organizationID, ledgerFromUpdateFilter, transferWeight, userID, productID)
		} else {
			ledgerEntryOut := StockLedgerEntry{
				ID:                 uuid.New().String(),
				PurchaseID:         purchaseID,
				Origin:             originFrom,
				StockType:          productType,
				WarehouseId:        warehouseFrom,
				ProductId:          productID,
				FactoryId:          factoryIDFrom,
				TransactionType:    "TRANSFER-OUT",
				TransactionDate:    transactionDate,
				CustomerName:       "", // Not applicable for stock transfer
				Remarks:            "Stock transfer out",
				CreatedBy:          userID,
				CreatedOn:          time.Now(),
				Location:           warehouseFrom,
				RefId:              transferID,
				TransactionBalance: transferWeight, // Will be negated by calculateStockDelta
			}

			openingBalanceOut, err := getLastLedgerBalance(sessionCtx, organizationID, ledgerEntryOut.PurchaseID, ledgerEntryOut.WarehouseId, ledgerEntryOut.Origin, ledgerEntryOut.StockType, ledgerEntryOut.ProductId)
			if err != nil {
				return nil, err
			}
			stockDeltaOut := calculateStockDelta(ledgerEntryOut.TransactionType, ledgerEntryOut.TransactionBalance)
			closingBalanceOut := openingBalanceOut + stockDeltaOut

			if closingBalanceOut < 0 {
				return nil, errors.New("insufficient stock in source warehouse for transfer")
			}

			ledgerEntryOut.OpeningBalance = openingBalanceOut
			ledgerEntryOut.ClosingBalance = closingBalanceOut

			if err := ProcessStockLedgerEntry(organizationID, ledgerEntryOut, userID); err != nil {
				return nil, err
			}
		}

		ledgerToUpdateFilter := LedgerFilter{
			ProductID:   productID,
			PurchaseID:  purchaseID,
			Origin:      originTo,
			RefId:       transferID,
			WarehouseID: warehouseTo,
		}

		to_exists, _ := ledgerEntryExists(organizationID, ledgerToUpdateFilter)
		if to_exists {
			go UpdateLedgerEntryByIDs(organizationID, ledgerToUpdateFilter, transferWeight, userID, productID)
		} else {
			ledgerEntryIn := StockLedgerEntry{
				ID:                 uuid.New().String(),
				PurchaseID:         purchaseID,
				Origin:             originTo,
				StockType:          productType,
				WarehouseId:        warehouseTo,
				ProductId:          productID,
				FactoryId:          factoryIDTo,
				TransactionType:    "TRANSFER-IN",
				TransactionDate:    transactionDate,
				CustomerName:       "", // Not applicable for stock transfer
				Remarks:            "Stock transfer",
				CreatedBy:          userID,
				CreatedOn:          time.Now(),
				Location:           warehouseTo,
				RefId:              transferID,
				TransactionBalance: transferWeight,
			}

			openingBalanceIn, err := getLastLedgerBalance(sessionCtx, organizationID, ledgerEntryIn.PurchaseID, ledgerEntryIn.WarehouseId, ledgerEntryIn.Origin, ledgerEntryIn.StockType, ledgerEntryIn.ProductId)
			if err != nil {
				return nil, err
			}
			stockDeltaIn := calculateStockDelta(ledgerEntryIn.TransactionType, ledgerEntryIn.TransactionBalance)
			closingBalanceIn := openingBalanceIn + stockDeltaIn

			ledgerEntryIn.OpeningBalance = openingBalanceIn
			ledgerEntryIn.ClosingBalance = closingBalanceIn

			if err := ProcessStockLedgerEntry(organizationID, ledgerEntryIn, userID); err != nil {
				return nil, err
			}
		}

		return nil, nil
	})

	return err
}

func ProcessJobWorkStockMovement(inputData map[string]interface{}, jobwork map[string]interface{}, jobworkTemplate map[string]interface{}, purchase map[string]interface{}, organizationID string, userID string, actionType string, insertedID string, purchaseID string) error {
	processID := fmt.Sprintf("%v", inputData["input_from"])
	if processID == "" {
		processID = "rcn"
	}

	// ---------------------------------------------------
	// PREPARE finalData
	// ---------------------------------------------------
	var finalData map[string]interface{}

	if actionType == "outWard-jobWork" {
		// jobworkInwardProducts := helper.ToStringSlice(jobworkTemplate["product_id"])
		productID, _ := inputData["input_product_types"].(string)

		// for _, product := range jobworkInwardProducts {
		finalData = map[string]interface{}{
			"jobwork_type": jobwork["type"],

			"quantity":       inputData["weight"],
			"purchase_id":    purchaseID,
			"job_id":         insertedID,
			"factory_id":     jobwork["infactory"],
			"customer_id":    jobwork["service_provider"],
			"warehouse_id":   jobwork["warehouse_id"],
			"country_origin": purchase["country_origin"],
			"process_id":     processID,
			"created_on":     inputData["created_on"],
			"product_id":     productID,
		}

		ProcessJobwork(organizationID, finalData, userID)
		// }

	} else if actionType == "inWard-jobWork" {

		jobworkParent, err := GetDataById(organizationID, inputData["parent_jobwork_id"].(string), "job_work")
		if err != nil {
			log.Println("jobwork data not found:", err)
			jobworkParent = map[string]interface{}{}
		}

		process, err := GetDataById(
			organizationID,
			inputData["template_id"].(string),
			"jobwork_template",
		)

		var processType string

		if err != nil {
			log.Println("process data not found:", err)
			process = map[string]interface{}{}
			processType = "" // default value
		} else {
			if val, ok := process["return_process_id"]; ok {
				processType = fmt.Sprintf("%v", val)
			} else {
				processType = ""
			}
		}

		inputData["purchase_id"] = purchaseID
		inputData["factory_id"] = jobworkParent["infactory"]
		inputData["warehouse_id"] = jobworkParent["warehouse_id"]
		inputData["country_origin"] = purchase["country_origin"]
		inputData["process_id"] = processID
		inputData["jobwork_type"] = jobwork["type"]
		inputData["job_id"] = insertedID
		inputData["customer_id"] = jobwork["service_provider"]

		ProcessInwardJobwork(organizationID, jobworkTemplate, inputData, userID, processType)
	}
	return nil
}

func ProcessJobWorkStockMovementCrossOrg(inputData map[string]interface{}, jobwork map[string]interface{}, jobworkTemplate map[string]interface{}, purchase map[string]interface{}, sourceOrgID string, destOrgID string, userID string, actionType string, insertedID string, purchaseID string) error {
	processID := fmt.Sprintf("%v", inputData["input_from"])
	if processID == "" {
		processID = "rcn"
	}

	if actionType == "outWard-jobWork" {
		productID, _ := inputData["input_product_types"].(string)
		quantity := helper.ToFloat64(inputData["weight"])
		warehouseID := helper.ToString(jobwork["warehouse_id"])
		countryOrigin := helper.ToString(purchase["country_origin"])

		destData := map[string]interface{}{
			"jobwork_type":   "inWard-jobWork",
			"type":           "process",
			"quantity":       quantity,
			"purchase_id":    purchaseID,
			"job_id":         insertedID,
			"factory_id":     jobwork["infactory"],
			"customer_id":    jobwork["service_provider"],
			"warehouse_id":   warehouseID,
			"country_origin": countryOrigin,
			"process_id":     processID,
			"created_on":     inputData["created_on"],
			"product_id":     productID,
		}

		if err := ProcessJobwork(destOrgID, destData, userID); err != nil {
			log.Printf("Failed to add inward stock to destination org %s: %v", destOrgID, err)
			return fmt.Errorf("failed to add stock to destination org: %w", err)
		}

		log.Printf("Cross-org stock movement: Added %.2f units inward to %s (from %s)", quantity, destOrgID, sourceOrgID)
	}

	return nil
}
