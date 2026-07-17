package entities

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

type ProcessLedgerEntry struct {
	ID                 string    `bson:"_id" json:"_id"`
	ProcessID          string    `bson:"process_id" json:"process_id"`
	PurchaseID         string    `bson:"purchase_id" json:"purchase_id"`
	Origin             string    `bson:"origin" json:"origin"`
	FactoryID          string    `bson:"factory_id" json:"factory_id"`
	ProcessType        string    `bson:"process_type" json:"process_type"` // cooking, borma, shelling
	InputProductID     string    `bson:"input_product_id" json:"input_product_id"`
	OutputProductID    string    `bson:"output_product_id" json:"output_product_id"`
	TransactionType    string    `bson:"transaction_type" json:"transaction_type"` // input, output
	TransactionDate    time.Time `bson:"transaction_date" json:"transaction_date"`
	InputQuantity      float64   `bson:"input_quantity" json:"input_quantity"`
	OutputQuantity     float64   `bson:"output_quantity" json:"output_quantity"`
	OpeningBalance     float64   `bson:"opening_balance" json:"opening_balance"`
	TransactionBalance float64   `bson:"transaction_balance" json:"transaction_balance"`
	ClosingBalance     float64   `bson:"closing_balance" json:"closing_balance"`
	Remarks            string    `bson:"remarks" json:"remarks"`
	CreatedBy          string    `bson:"created_by" json:"created_by"`
	CreatedOn          time.Time `bson:"created_on" json:"created_on"`
}

// ProcessSummary represents current process stock levels
type ProcessSummary struct {
	ID            string    `bson:"_id" json:"_id"`
	ProcessID     string    `bson:"process_id" json:"process_id"`
	PurchaseID    string    `bson:"purchase_id" json:"purchase_id"`
	Origin        string    `bson:"origin" json:"origin"`
	FactoryID     string    `bson:"factory_id" json:"factory_id"`
	ProcessType   string    `bson:"process_type" json:"process_type"`
	ProductID     string    `bson:"product_id" json:"product_id"`
	AvailableQty  float64   `bson:"available_qty" json:"available_qty"`
	LastUpdatedBy string    `bson:"last_updated_by" json:"last_updated_by"`
	LastUpdatedOn time.Time `bson:"last_updated_on" json:"last_updated_on"`
	CreatedBy     string    `bson:"created_by" json:"created_by"`
	CreatedOn     time.Time `bson:"created_on" json:"created_on"`
}

// ProcessOperation handles process ledger and summary updates
func ProcessOperation(organizationID string, processData map[string]interface{}, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	processType := processData["process_type"].(string)
	factoryID := processData["factory_id"].(string)
	purchaseID := processData["purchase_id"].(string)

	// Get origin from purchase if not in processData
	var origin string
	if originVal, ok := processData["country_origin"]; ok && originVal != nil {
		origin = originVal.(string)
	} else {
		// Fetch origin from purchase collection
		var purchase map[string]interface{}
		err := database.Collection("purchase").FindOne(context.Background(), bson.M{"_id": purchaseID}).Decode(&purchase)
		if err == nil {
			if countryOrigin, ok := purchase["country_origin"].(string); ok {
				origin = countryOrigin
			}
		}
	}

	// Get input quantity from various possible fields
	var inputQty float64
	if val, ok := processData["input_weight"]; ok && val != nil {
		inputQty = val.(float64)
	} else if val, ok := processData["filled_tins"]; ok && val != nil {
		inputQty = val.(float64)
	}

	// Get output quantity
	var outputQty float64
	if val, ok := processData["output_weight"]; ok && val != nil {
		outputQty = val.(float64)
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		transactionDate := time.Now()
		if soldOnStr, ok := processData["created_on"]; ok {
			transactionDate = helper.ParseDate(soldOnStr)
		}
		// Input transaction (debit)
		inputLedger := ProcessLedgerEntry{
			ID:                 uuid.New().String(),
			ProcessID:          processData["_id"].(string),
			PurchaseID:         purchaseID,
			Origin:             origin,
			FactoryID:          factoryID,
			ProcessType:        processType,
			InputProductID:     getInputProduct(processType),
			OutputProductID:    getOutputProduct(processType),
			TransactionType:    "input",
			TransactionDate:    transactionDate,
			InputQuantity:      inputQty,
			TransactionBalance: -inputQty,
			Remarks:            processType + " input",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
		}
		// Output transaction (credit)
		outputLedger := ProcessLedgerEntry{
			ID:                 uuid.New().String(),
			ProcessID:          processData["_id"].(string),
			PurchaseID:         purchaseID,
			Origin:             origin,
			FactoryID:          factoryID,
			ProcessType:        processType,
			InputProductID:     getInputProduct(processType),
			OutputProductID:    getOutputProduct(processType),
			TransactionType:    "output",
			TransactionDate:    transactionDate,
			OutputQuantity:     outputQty,
			TransactionBalance: outputQty,
			Remarks:            processType + " output",
			CreatedBy:          userID,
			CreatedOn:          time.Now(),
		}

		// Insert ledger entries
		if _, err := database.Collection("process_ledger").InsertOne(sessionCtx, inputLedger); err != nil {
			return nil, err
		}
		if _, err := database.Collection("process_ledger").InsertOne(sessionCtx, outputLedger); err != nil {
			return nil, err
		}

		// Update process summary
		if err := updateProcessSummary(sessionCtx, organizationID, inputLedger, userID); err != nil {
			return nil, err
		}
		if err := updateProcessSummary(sessionCtx, organizationID, outputLedger, userID); err != nil {
			return nil, err
		}

		return nil, nil
	})

	return err
}

func getInputProduct(processType string) string {
	switch processType {
	case "COOK": // cooking
		return "rcn"
	case "BORM": // borma
		return "cooked_rcn"
	case "COOL": // cracking
		return "borma_rcn"
	case "PEEL": // machine peeling
		return "cracked_rcn"
	case "SHELL": // shelling
		return "peeled_rcn"
	case "GRAD": // grading
		return "kernel"
	case "PACK": // packing
		return "graded_kernel"
	default:
		return "rcn"
	}
}

func getPreviousProductType(processType string) string {
	switch processType {
	case "COOK": // cooking
		return "RCN"
	case "SHELL": // borma
		return "COOK"
	case "BORM": // cracking
		return "SHELL"
	case "COOL": // machine peeling
		return "BORM"
	case "PEEL": // shelling
		return "COOL"
	case "GRAD": // grading
		return "PEEL"
	case "PACK": // packing
		return "GRAD"
	case "RCN":
		return "RCN"
	default:
		return "KERNEL"
	}
}

func getOutputProduct(processType string) string {
	switch processType {
	case "COOK": // cooking
		return "WIP"
	case "BORM": // borma
		return "WIP"
	case "COOL": // cracking
		return "WIP"
	case "PEEL": // machine peeling
		return "WIP"
	case "SHELL": // shelling
		return "WIP"
	case "GRAD": // grading
		return "WIP"
	case "PACK": // packing
		return "WIP"
	case "rcn", "RCN":
		return "RCN"
	default:
		return "kernel"
	}
}

func updateProcessSummary(sessionCtx mongo.SessionContext, organizationID string, ledger ProcessLedgerEntry, userID string) error {
	db := database.GetConnection(organizationID)

	filter := bson.M{
		"process_id":   ledger.ProcessID,
		"purchase_id":  ledger.PurchaseID,
		"origin":       ledger.Origin,
		"factory_id":   ledger.FactoryID,
		"process_type": ledger.ProcessType,
		"product_id":   ledger.OutputProductID,
	}

	update := bson.M{
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

	upsert := true
	opts := options.Update().SetUpsert(upsert)
	_, err := db.Collection("process_summary").UpdateOne(context.Background(), filter, update, opts)
	return err
}
