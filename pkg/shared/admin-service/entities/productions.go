package entities

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

func UpdateProductionSummary(orgId string, inputData map[string]interface{}) error {
	// Extract date from process_start_date_time
	var startDate string
	if dateTime, ok := inputData["process_start_date_time"]; ok {
		if dateStr, ok := dateTime.(string); ok {
			if parsedTime, err := time.Parse(time.RFC3339, dateStr); err == nil {
				startDate = parsedTime.Format("02-01-2006")
			}
		} else if timeVal, ok := dateTime.(time.Time); ok {
			startDate = timeVal.Format("02-01-2006")
		}
	}

	if startDate == "" {
		startDate = time.Now().Format("02-01-2006")
	}

	// Get factory info for org_id
	factoryId := inputData["factory_id"].(string)
	var factory map[string]interface{}
	err := database.GetConnection(orgId).Collection("factory").FindOne(context.Background(), bson.M{"_id": factoryId}).Decode(&factory)
	if err != nil {
		return err
	}

	factoryOrgId := factory["org_id"].(string)
	processId := helper.ToInt32(inputData["process_id"])

	// Check if it's a subprocess by looking up process_type in sub_process collection
	if processType, ok := inputData["process_type"].(string); ok {
		var subProcess map[string]interface{}
		err := database.GetConnection(orgId).Collection("sub_process").FindOne(context.Background(), bson.M{"_id": processType}).Decode(&subProcess)
		if err == nil {
			// For subprocess, use the process_type as the process identifier
			processId = helper.ToInt32(subProcess["process_id"])
		}
	}

	// Calculate weights based on process_id
	var updateFields bson.M
	inputWeight := helper.ToFloat64(inputData["input_weight"])
	outputWeight := helper.ToFloat64(inputData["output_weight"])
	filledTins := helper.ToFloat64(inputData["filled_tins"])

	// Get packing weight if process_id is 7
	packingWeight := 0.0
	if processId == 7 {
		if packingId, ok := inputData["type_of_packing"].(string); ok {
			var packingData map[string]interface{}
			err := database.GetConnection(orgId).Collection("lookup").FindOne(context.Background(), bson.M{"_id": packingId}).Decode(&packingData)
			if err == nil {
				packingValue := helper.ToFloat64(packingData["value"])
				packingWeight = filledTins * packingValue
			}
		}
	}

	switch processId {
	case 1: // Cooking
		updateFields = bson.M{"$inc": bson.M{"totalCookingWeight": inputWeight, "totalWeight": inputWeight}}
	case 2: // Shelling
		updateFields = bson.M{"$inc": bson.M{"totalShellingWeight": inputWeight, "totalWeight": inputWeight}}
	case 3: // Borma
		updateFields = bson.M{"$inc": bson.M{"totalBormaWeight": inputWeight, "totalWeight": inputWeight}}
	case 4: // Cooling
		updateFields = bson.M{"$inc": bson.M{"totalCoolingWeight": inputWeight, "totalWeight": inputWeight}}
	case 5: // Pelling
		updateFields = bson.M{"$inc": bson.M{"totalPellingWeight": outputWeight, "totalWeight": outputWeight}}
	case 6: // Grading
		updateFields = bson.M{"$inc": bson.M{"totalGradingWeight": inputWeight, "totalWeight": inputWeight}}
	case 7: // Packing
		updateFields = bson.M{"$inc": bson.M{"totalPackingWeight": packingWeight, "totalWeight": packingWeight}}
	default:
		return nil
	}

	documentId := startDate + "-" + factoryId

	// Add set fields for first time creation (excluding fields being incremented)
	setOnInsertFields := bson.M{
		"_id":                     documentId,
		"date":                    startDate,
		"org_id":                  factoryOrgId,
		"factory_id":              factoryId,
		"process_start_date_time": time.Now(),
	}

	// Initialize other weight fields to 0 (except current process and totalWeight)
	if processId != 1 {
		setOnInsertFields["totalCookingWeight"] = 0
	}
	if processId != 2 {
		setOnInsertFields["totalShellingWeight"] = 0
	}
	if processId != 3 {
		setOnInsertFields["totalBormaWeight"] = 0
	}
	if processId != 4 {
		setOnInsertFields["totalCoolingWeight"] = 0
	}
	if processId != 5 {
		setOnInsertFields["totalPellingWeight"] = 0
	}
	if processId != 6 {
		setOnInsertFields["totalGradingWeight"] = 0
	}
	if processId != 7 {
		setOnInsertFields["totalPackingWeight"] = 0
	}

	updateFields["$setOnInsert"] = setOnInsertFields

	filter := bson.M{"_id": documentId}
	opts := options.Update().SetUpsert(true)

	_, err = database.GetConnection(orgId).Collection("production_summary").UpdateOne(context.Background(), filter, updateFields, opts)
	if err != nil {
		return err
	}
	return nil
}

func ProductionProductLevelUpdates(orgId string, inputData map[string]interface{}, previousData map[string]interface{}) {
	var processTypetemplateId string
	var purchaseId string
	//var processId int
	var processType string
	var previourProcessType string
	var factoryId string
	var warehouseId string
	// var previourProcessId int

	purchaseId = getString(inputData["purchase_id"])
	processType = getString(inputData["process_type"])
	factoryId = getString(inputData["factory_id"])
	warehouseId = getString(inputData["warehouse_id"])
	// processId = helper.ToInt(inputData["process_id"])
	processTypetemplateId = getString(inputData["template_id"])

	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"template_id", processTypetemplateId}}}},
	}

	templateProducts, err := helper.GetAggregateQueryResult(orgId, "process_product", pipeline)
	if err != nil {
		// return err
	}

	if len(templateProducts) == 0 {
		// return fmt.Errorf("No data found")
	}

	processPipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", processType}}}},
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
	if err != nil {
		// return err
	}

	if len(processData) != 0 {
		previourProcessType = processData[0]["previous_process_type"].(string)
	}

	for _, obj := range templateProducts {
		productId := getString(obj["product_id"])
		productType := getString(obj["type"])
		if productType == "input" {
			qty := helper.ToFloat64(inputData[productId])
			//oldqty := helper.ToFloat64(previousData[productId])
			diff := qty * -1

			filter := bson.M{
				"product_id":   productId,
				"process_type": previourProcessType,
				"purchase_id":  purchaseId,
				"factory_id":   factoryId,
				"warehouse_id": warehouseId,
			}

			update := bson.M{
				"$inc": bson.M{
					"available_qty": diff,
				},
				"$set": bson.M{
					"product_id":   productId,
					"process_type": previourProcessType,
					"purchase_id":  purchaseId,
					"type":         productType,
					"factory_id":   factoryId,
					"warehouse_id": warehouseId,
					"stock_type":   getStockTypeForProcess(previourProcessType),
					"location":     warehouseId,
				},
				"$setOnInsert": bson.M{
					"_id": uuid.New().String(),
				},
			}

			opts := options.Update().SetUpsert(true)

			_, err := database.GetConnection(orgId).
				Collection("stock_in_hand").
				UpdateOne(context.Background(), filter, update, opts)

			if err != nil {
				fmt.Println("Stock update failed:", err)
			}

		} else if productType == "output" {
			qty := helper.ToFloat64(inputData[productId])
			oldqty := helper.ToFloat64(previousData[productId])
			diff := qty - oldqty

			filter := bson.M{
				"product_id":   productId,
				"process_type": processType,
				"purchase_id":  purchaseId,
				"factory_id":   factoryId,
				"warehouse_id": warehouseId,
			}

			update := bson.M{
				"$inc": bson.M{
					"available_qty": diff,
				},
				"$set": bson.M{
					"product_id":   productId,
					"process_type": processType,
					"purchase_id":  purchaseId,
					"type":         productType,
					"warehouse_id": warehouseId,
					"factory_id":   factoryId,
					"stock_type":   "WIP",
					"location":     warehouseId,
				},
				"$setOnInsert": bson.M{
					"_id": uuid.New().String(),
				},
			}

			opts := options.Update().SetUpsert(true)

			_, err = database.GetConnection(orgId).
				Collection("stock_in_hand").
				UpdateOne(context.Background(), filter, update, opts)
		}
	}

}

// Helper function to get stock type based on process type
func getStockTypeForProcess(processType string) string {
	if processType == "" {
		return "RCN"
	}
	return "WIP"
}
