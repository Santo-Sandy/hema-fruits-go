package onboarding

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared/database"
)

// GenerateCompleteProductionDoc creates a realistic production document with all fields
func GenerateCompleteProductionDoc(
	processType string,
	factoryID string,
	warehouseID string,
	purchaseID string,
	origin string,
	orgID string,
	templateID string,
	inputWeight float64,
	outputWeight float64,
	currentDate time.Time,
	machineName string,
) bson.M {
	id := uuid.New().String()
	
	// Get database connection
	ctx := context.Background()
	db := database.GetConnection(orgID)
	
	// Get random employee
	var employees []bson.M
	workerID := ""
	empCursor, _ := db.Collection("employee").Find(ctx, bson.M{"status": "Active"})
	if empCursor != nil {
		empCursor.All(ctx, &employees)
		if len(employees) > 0 {
			workerID = fmt.Sprintf("%v", employees[rand.Intn(len(employees))]["_id"])
		}
	}
	
	// Get equipment details from equipments collection based on process type
	equipmentName := machineName
	equipmentID := ""
	var equipment bson.M
	err := db.Collection("equipments").FindOne(ctx, bson.M{
		"process_id": processType,
		"status":     "Active",
	}).Decode(&equipment)
	if err == nil && equipment != nil {
		if name, ok := equipment["machine_name"].(string); ok && name != "" {
			equipmentName = name
		}
		if id, ok := equipment["_id"].(string); ok && id != "" {
			equipmentID = id
		}
	}
	
	// Base document common to all processes
	doc := bson.M{
		"_id":                     id,
		"factory_id":              factoryID,
		"warehouse_id":            warehouseID,
		"process_type":            processType,
		"input_weight":            inputWeight,
		"purchase_id":             purchaseID,
		"status":                  "Active",
		"origin":                  origin,
		"created_by":              "SYSTEM",
		"created_on":              currentDate,
		"update_by":               "SYSTEM",
		"update_on":               currentDate,
		"org_id":                  orgID,
		"template_id":             templateID,
		"process_start_date_time": currentDate,
		"price_per_kg":            12,
		"worker_id":               workerID,
		"equipment_name":          equipmentName,
		"equipment_id":            equipmentID,
	}

	// Add process-specific fields
	switch processType {
	case "COOK":
		doc["STEAMEDRCN"] = outputWeight
		doc["duration"] = 8
		doc["stones_weight"] = 0
		doc["product_name"] = "RAW CASHEW NUT"
		doc["temperature"] = 120 + rand.Intn(20) // 120-140°C
		doc["pressure"] = 15 + rand.Intn(5)      // 15-20 PSI
		
		// Add equipment details
		doc["machine_name"] = equipmentName
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 1
		
		// Add equipment object (nested)
		if equipmentID != "" {
			doc["equipments"] = bson.M{
				"_id":                      equipmentID,
				"equipment_process_type":   "Batch",
				"max_capacity":             8000,
				"status":                   "Active",
				"equipment_type":           "Machine",
				"unit":                     doc["unit_id"],
				"minimum_duration":         15,
				"machine_name":             equipmentName,
				"purchase_date":            time.Now().AddDate(-1, 0, 0), // 1 year ago
				"machine_supplier_name":    getRandomSupplier(),
				"warranty":                 15,
				"is_production":            true,
				"process_id":               "COOK",
				"factory":                  factoryID,
				"target_per_day":           3200,
				"created_by":               "SYSTEM",
				"created_on":               time.Now().AddDate(-1, 0, 0),
			}
		}

	case "SHELL", "MC-SH", "ML-SH":
		doc["SH_WHOLES"] = outputWeight
		doc["duration"] = 8
		doc["template_name"] = "SHELLING"
		doc["machine_name"] = equipmentName
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 2
		doc["machine_type"] = "MC-SH"
		if processType == "ML-SH" {
			doc["machine_type"] = "ML-SH"
		}
		if equipmentID != "" {
			doc["equipments"] = bson.M{
				"_id":                    equipmentID,
				"machine_name":           equipmentName,
				"equipment_process_type": "Batch",
				"max_capacity":           8000,
				"status":                 "Active",
				"equipment_type":         "Machine",
				"unit":                   doc["unit_id"],
				"minimum_duration":       15,
				"machine_supplier_name":  getRandomSupplier(),
				"warranty":               15,
				"is_production":          true,
				"process_id":             processType,
				"factory":                factoryID,
				"target_per_day":         3200,
				"created_by":             "SYSTEM",
				"created_on":             currentDate,
			}
		}

	case "BORM":
		doc["BR_WHOLES"] = outputWeight
		doc["template_name"] = "BORMA"
		doc["machine_name"] = equipmentName
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 3
		doc["duration"] = 6
		if equipmentID != "" {
			doc["equipments"] = bson.M{
				"_id":                    equipmentID,
				"machine_name":           equipmentName,
				"equipment_process_type": "Batch",
				"max_capacity":           10000,
				"status":                 "Active",
				"equipment_type":         "Machine",
				"unit":                   doc["unit_id"],
				"minimum_duration":       12,
				"machine_supplier_name":  getRandomSupplier(),
				"warranty":               12,
				"is_production":          true,
				"process_id":             processType,
				"factory":                factoryID,
				"target_per_day":         1000,
				"created_by":             "SYSTEM",
				"created_on":             currentDate,
			}
		}

	case "COOL":
		doc["COOLED"] = outputWeight
		doc["template_name"] = "COOLING"
		doc["machine_name"] = equipmentName
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 4
		doc["duration"] = 4
		doc["cooling_temperature"] = 25 + rand.Intn(10)
		if equipmentID != "" {
			doc["equipments"] = bson.M{
				"_id":                    equipmentID,
				"machine_name":           equipmentName,
				"equipment_process_type": "continuous",
				"max_capacity":           10000,
				"status":                 "Active",
				"equipment_type":         "CR",
				"unit":                   doc["unit_id"],
				"minimum_duration":       10,
				"machine_supplier_name":  getRandomSupplier(),
				"warranty":               5,
				"is_production":          true,
				"process_id":             processType,
				"factory":                factoryID,
				"target_per_day":         800,
				"created_by":             "SYSTEM",
				"created_on":             currentDate,
			}
		}

	case "PEEL":
		doc["PEELED"] = outputWeight
		doc["template_name"] = "PEELING"
		doc["machine_name"] = equipmentName
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 5
		doc["duration"] = 8
		if equipmentID != "" {
			doc["equipments"] = bson.M{
				"_id":                    equipmentID,
				"machine_name":           equipmentName,
				"equipment_process_type": "continuous",
				"max_capacity":           160,
				"status":                 "Active",
				"equipment_type":         "Machine",
				"unit":                   doc["unit_id"],
				"minimum_duration":       20,
				"machine_supplier_name":  getRandomSupplier(),
				"warranty":               10,
				"is_production":          true,
				"process_id":             processType,
				"factory":                factoryID,
				"target_per_day":         1000,
				"created_by":             "SYSTEM",
				"created_on":             currentDate,
			}
		}

	case "GRAD", "GRADING":
		doc["GRADING"] = outputWeight
		doc["template_name"] = "GRADING"
		doc["machine_name"] = equipmentName
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 6
		doc["duration"] = 6
		doc["P_ALL_WHOLES"] = outputWeight
		doc["WW"] = outputWeight * (rand.Float64() * 0.4)
		doc["SW"] = outputWeight * (rand.Float64() * 0.5)
		doc["PKW"] = outputWeight * (rand.Float64() * 0.15)
		doc["LWP"] = outputWeight * (rand.Float64() * 0.08)
		doc["DW"] = outputWeight * (rand.Float64() * 0.05)
		doc["UPP"] = outputWeight * (rand.Float64() * 0.03)
		doc["UPW"] = outputWeight * (rand.Float64() * 0.3)
		doc["OW"] = outputWeight * (rand.Float64() * 0.02)
		doc["BUDS"] = outputWeight * (rand.Float64() * 0.1)
		doc["S"] = outputWeight * (rand.Float64() * 0.05)
		doc["HUSK"] = outputWeight * (rand.Float64() * 0.02)
		doc["nlg"] = 1 + rand.Intn(3)
		doc["colour"] = getRandomColour()
		doc["moisture"] = 3 + rand.Intn(3)
		doc["nut_count"] = 140 + rand.Intn(40)
		doc["uniformity"] = 1 + rand.Intn(2)
		doc["testa"] = 1 + rand.Intn(2)
		doc["insect_infested"] = getRandomYesNo()
		if equipmentID != "" {
			doc["equipments"] = bson.M{
				"_id":                    equipmentID,
				"machine_name":           equipmentName,
				"equipment_process_type": "continuous",
				"max_capacity":           5000,
				"status":                 "Active",
				"equipment_type":         "Machine",
				"unit":                   doc["unit_id"],
				"minimum_duration":       10,
				"machine_supplier_name":  getRandomSupplier(),
				"warranty":               10,
				"is_production":          true,
				"process_id":             processType,
				"factory":                factoryID,
				"target_per_day":         2000,
				"created_by":             "SYSTEM",
				"created_on":             currentDate,
			}
		}

	case "PACK":
		// Get product from lookup or use default
		productID := getRandomPackedProduct()
		doc["product_id"] = productID
		doc["PEELED"] = outputWeight
		doc["weight"] = outputWeight
		doc["template_name"] = "PACKING"
		doc["duration"] = 6
		
		// Packing specific fields
		packingType := getRandomPackingType()
		doc["type_of_packing"] = packingType
		
		// Calculate tins based on packing type
		tinWeight := getPackingTypeWeight(packingType)
		filledTins := int(outputWeight / tinWeight)
		if filledTins == 0 {
			filledTins = 1
		}
		
		doc["filled_tins"] = filledTins
		doc["start_serial_no"] = rand.Intn(1000) + 1
		doc["end_serial_no"] = doc["start_serial_no"].(int) + filledTins
		doc["packed_by"] = fmt.Sprintf("Pack%03d", rand.Intn(100)+1)
		
		// Quality parameters for packed goods
		doc["nlg"] = 1 + rand.Intn(3)
		doc["colour"] = getRandomColour()
		doc["moisture"] = 3 + rand.Intn(3)
		doc["nut_count"] = 140 + rand.Intn(40)
		doc["uniformity"] = 1 + rand.Intn(2)
		doc["testa"] = 1 + rand.Intn(2)
		doc["insect_infested"] = getRandomYesNo()
		
		// Unit and process info
		doc["unit_id"] = getRandomUnitID()
		doc["process_id"] = 6
	}

	return doc
}

// Helper functions for random data generation

func getRandomColour() string {
	colours := []string{"very good", "good", "fair", "light", "white", "pale"}
	return colours[rand.Intn(len(colours))]
}

func getRandomYesNo() string {
	if rand.Intn(10) < 2 { // 20% chance of "yes"
		return "yes"
	}
	return "no"
}

func getRandomPackedProduct() string {
products := []string{
    "Borma Pieces",
    "HUSK POWDER",
    "K/LWP Mix",
    "LWP",
    "NW PIECES INPUT",
    "NW PIECES OUTPUT",
    "PIECES",
    "Pieces",
    "Pieces Mixed",
    "Pieces Unpeeled Mixed",
    "SPLITS",
    "SW210",
    "SW240",
    "SWP",
    "SWP Mix",
    "Separation Pieces",
    "Splits",
    "Splits Mix",
    "Unpeeled Pieces",
    "Unpeeled Pieces 2",
    "Unpeeled Pieces 3",
    "Unpeeled Separation Pieces",
    "W180",
    "W210",
    "W240",
    "W320",
    "W450",
    "swp2",
}
	return products[rand.Intn(len(products))]
}

func getRandomPackingType() string {
	packingTypes := []string{
		"001", // 10 kg
		"002", // 11.34 kg (25 lbs)
		"003", // 22.68 kg (50 lbs)
		"004", // 15 kg
		"005", // 20 kg
	}
	return packingTypes[rand.Intn(len(packingTypes))]
}

func getPackingTypeWeight(packingType string) float64 {
	weights := map[string]float64{
		"001": 10.0,
		"002": 11.34,
		"003": 22.68,
		"004": 15.0,
		"005": 20.0,
	}
	if weight, ok := weights[packingType]; ok {
		return weight
	}
	return 10.0 // default
}

func getRandomUnitID() string {
	units := []string{
		"6a6UNI--001",
		"6a6UNI--002",
		"6a6UNI--003",
		"6a6UNI--004",
		"6a6UNI--005",
	}
	return units[rand.Intn(len(units))]
}

func getRandomEquipmentID(processType string) string {
	return ""
}

func getEquipmentFromDB(db *mongo.Database, processType string, factoryID string) (string, string) {
	ctx := context.Background()
	var equipmentList []bson.M
	cursor, _ := db.Collection("equipments").Find(ctx, bson.M{
		"process_id": processType,
		"factory":    factoryID,
		"status":     "Active",
	})
	if cursor != nil {
		cursor.All(ctx, &equipmentList)
	}
	if len(equipmentList) > 0 {
		equipment := equipmentList[rand.Intn(len(equipmentList))]
		id := fmt.Sprintf("%v", equipment["_id"])
		name := fmt.Sprintf("%v", equipment["machine_name"])
		return id, name
	}
	return "", ""
}

func getRandomSupplier() string {
	suppliers := []string{
		"TRV Enterprises",
		"ABC Machinery",
		"XYZ Equipment Co",
		"Global Machines Ltd",
		"Tech Solutions Inc",
		"Industrial Equipment Corp",
	}
	return suppliers[rand.Intn(len(suppliers))]
}
