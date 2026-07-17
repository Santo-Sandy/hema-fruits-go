package entities

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"

	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared"
	cloudflareMethod "kriyatec.com/pms-api/pkg/shared/cloudflare"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/sampleData"

	"kriyatec.com/pms-api/pkg/shared/onboarding"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

var updateOpts = options.Update().SetUpsert(true)
var factoryconfig = make(map[string]interface{})
var fileUploadPath = ""
var ctx = context.Background()

// func createDynamicStruct(typeInfo string) interface{} {
// 	// Parse the reflection information string to get the reflect type
// 	structType := parseTypeInfo(typeInfo)

// 	// Create a slice to hold struct fields
// 	var fields []reflect.StructField

// 	// Iterate over the fields of the reflect struct
// 	for i := 0; i < structType.NumField(); i++ {
// 		field := structType.Field(i)

// 		// Create struct field with type and tags
// 		structField := reflect.StructField{
// 			Name: field.Name,
// 			Type: field.Type,
// 			Tag:  reflect.StructTag(field.Tag.Get("json")), // Only taking JSON tags for simplicity
// 		}

// 		// Append the field to the slice
// 		fields = append(fields, structField)
// 	}

// 	// Create the struct type
// 	newStructType := reflect.StructOf(fields)

// 	// Create a new instance of the struct
// 	newStructValue := reflect.New(newStructType).Elem()

// 	// Return the struct value
// 	return newStructValue.Interface()
// }

// Helper function to parse the reflection information string and get the reflect type
// func parseTypeInfo(typeInfo string) reflect.Type {
// 	typeInfo = strings.TrimSpace(typeInfo)
// 	structName := strings.Split(typeInfo, " ")[1] // Get the name of the struct
// 	structType, _ := reflect.StructOf([]reflect.StructField{}).FieldByNameFunc(func(f string) bool {
// 		return f == structName
// 	})
// 	return structType.Type
// }

// PostDocHandler --METHOD Data insert to mongo Db with Proper Field Validation
func IsDup(err error) (bool, string, string) {
	if wes, ok := err.(mongo.WriteException); ok {
		for i := range wes.WriteErrors {
			if wes.WriteErrors[i].Code == 11000 || wes.WriteErrors[i].Code == 11001 || wes.WriteErrors[i].Code == 12582 || wes.WriteErrors[i].Code == 16460 {
				// Extract the values associated with "_id" and "name" using regular expressions
				errorMessage := wes.WriteErrors[i].Error()

				// Extract the "_id" value
				reID := regexp.MustCompile(`_id: "([^"]+)"`)
				matchesID := reID.FindStringSubmatch(errorMessage)
				id := ""
				if len(matchesID) == 2 {
					id = matchesID[1]
				}

				// Extract the "name" value
				reName := regexp.MustCompile(`name: "([^"]+)"`)
				matchesName := reName.FindStringSubmatch(errorMessage)
				name := ""
				if len(matchesName) == 2 {
					name = matchesName[1]
				}

				return true, id, name
			}
		}
	}
	return false, "", ""
}
// findtheNextProcesstemplate
//
// PURPOSE:
// This function determines the next process template based on the current
// production template ID. It is used to move workflow from Borma stage
// to Cooling stage in the production pipeline.
//
// FLOW:
// 1. Read current template_id from input data
// 2. Match it against known Borma templates
// 3. Return corresponding Cooling template
// 4. If no match found, return empty string
//
// TEMPLATE MAPPING:
// - Borma-NW-pieces-fields → Cooling-NW-pieces-fields
// - Borma-NW-wholes-fields → Cooling-NW-wholes-fields
// - Borma-NW-fields        → Cooling-NW-fields
//
// RETURN:
// - string → next process template ID
// - "" → if no valid mapping exists
//
// NOTE:
// This function acts as a STATIC WORKFLOW MAPPER between production stages.
func findtheNextProcesstemplate(inputData map[string]interface{}) string {
	currenttemplate := inputData["template_id"]

	switch currenttemplate {
	case "Borma-NW-pieces-fields":
		return "Cooling-NW-pieces-fields"
	case "Borma-NW-wholes-fields":
		return "Cooling-NW-wholes-fields"
	case "Borma-NW-fields":
		return "Cooling-NW-fields"
	default:
		return ""
	}
}

// autoEntryForCooling
//
// PURPOSE:
// This function automatically creates a new "Cooling" production entry
// based on input data from the previous process stage (Borma/Upstream).
//
// FLOW:
// 1. Identify current and next process templates
// 2. Fetch process-product mapping from "process_product" collection
// 3. Map input product values into cooling production structure
// 4. Build new cooling production payload with metadata
// 5. Assign process sequencing and increment process_id
// 6. Generate lot number if required (based on config)
// 7. Validate stock before insertion
// 8. Insert new production record into "productions"
// 9. Update production summary asynchronously
// 10. Update production stock after creation
//
// DATA TRANSFORMATION:
// - output_weight → input_weight
// - borma_product → cooling_product
// - process_end_date_time → process_start_date_time
// - Adds factory, unit, warehouse context
//
// LOT LOGIC:
// - Uses LotCreatingConfig() to check process stage
// - Generates lot number using LotCreating()
//
// IMAGE HANDLING:
// - Only first image entry is taken
// - Marks image status as "Start"
//
// STOCK FLOW:
// - ValidateProductionStockUpdate() ensures stock correctness before insert
// - PostProductionStock() updates inventory after insert
//
// ASYNC OPERATIONS:
// - UpdateProductionSummary() runs in background (non-blocking)
//
// COLLECTION USED:
// - productions (main production entry)
//
// NOTE:
// This function is responsible for CREATING a new cooling stage record,
// unlike autoUpdateForCooling which updates existing records.
func autoEntryForCooling(orgId string, userId string, inputData map[string]interface{}, prevousbatchId string) error {
	coolingData := make(map[string]interface{})

	// Get the current and next template
	currentTemplate := inputData["template_id"].(string)
	nextTemplate := findtheNextProcesstemplate(inputData)

	// Build the pipeline to get previous and current product IDs
	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"template_id", currentTemplate},
					{"type", "output"},
				},
			},
		},
		bson.D{
			{"$project",
				bson.D{
					{"_id", 0},
					{"previousProduct", "$product_id"},
					{"currentProduct", "$product_id"},
				},
			},
		},
	}

	// Execute the pipeline
	result, err := helper.GetAggregateQueryResult(orgId, "process_product", pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// Map the previous product output to current product input AND output
	if len(result) > 0 {
		// First, get the output product for the current template
		// outputPipeline := bson.A{
		// 	bson.D{{"$match", bson.D{
		// 		{"template_id", nextTemplate},
		// 		{"type", "output"},
		// 	}}},
		// }
		// outputProducts, _ := helper.GetAggregateQueryResult(orgId, "process_product", outputPipeline)

		for _, result := range result {
			if previousProductID, ok := result["previousProduct"].(string); ok {
				if currentProductID, ok := result["currentProduct"].(string); ok {
					if previousValue, exists := inputData[previousProductID]; exists {
						// Set input product
						coolingData[currentProductID] = previousValue

						// Set output product with same value as initi`al estimate
						// This will be updated when production is completed with actual output
						// if len(outputProducts) > 0 {
						// 	for _, outProd := range outputProducts {
						// 		if outputProductID, ok := outProd["product_id"].(string); ok {
						// 			coolingData[outputProductID] = previousValue
						// 		}
						// 	}
						// }
					}
				}
			}
		}
	}

	coolingData["created_on"] = time.Now()
	coolingData["created_by"] = userId
	coolingData["process_type"] = "COOL"
	coolingData["input_weight"] = inputData["output_weight"]
	coolingData["warehouse_id"] = inputData["warehouse_id"]
	coolingData["cooling_product"] = inputData["borma_product"]
	coolingData["process_start_date_time"] = inputData["process_end_date_time"]
	coolingData["purchase_id"] = inputData["purchase_id"]
	coolingData["prevous_batch_id"] = prevousbatchId
	coolingData["factory_id"] = inputData["factory_id"]
	coolingData["unit_id"] = inputData["unit_id"]
	if imageUpload, ok := inputData["image_upload"].([]interface{}); ok && len(imageUpload) > 1 {
		if imgMap, ok := imageUpload[1].(map[string]interface{}); ok {
			imgMap["process_status"] = "Start"
			coolingData["image_upload"] = []interface{}{imgMap}
		}
	}
	coolingData["equipment_id"] = inputData["cooling_equipment_id"]
	coolingData["trolley_weight"] = inputData["trolley_weight"]
	coolingData["remarks"] = inputData["remarks"]
	if processID, ok := inputData["process_id"].(float64); ok {
		coolingData["process_id"] = int(processID) + 1
	}
	coolingData["template_id"] = nextTemplate
	if _, ok := coolingData["status"]; !ok || coolingData["status"] == "" {
		coolingData["status"] = "Start"
	}

	helper.UpdateDateObject(coolingData)
	helper.HandleIDGeneration(coolingData, orgId, "productions")

	processId, err := LotCreatingConfig(orgId, coolingData["factory_id"].(string))
	if err != nil {
		return err
	}
	reqProcessId := helper.ToInt32(coolingData["process_id"])

	if processId == reqProcessId {
		value, err := LotCreating(orgId, userId, coolingData)
		if err != nil {
			return err
		}
		coolingData["lot_number"] = value
	}

	// Validate stock before inserting production (use empty productionId for CREATE)
	if err := ValidateProductionStockUpdate(orgId, coolingData, ""); err != nil {
		return shared.BadRequest(fmt.Sprintf("Stock validation failed: %v", err))
	}

	res, err := Insert(orgId, "productions", coolingData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	go func() {
		err := UpdateProductionSummary(orgId, coolingData)
		if err != nil {
			log.Printf("Failed to update production summary: %v", err)
		}
	}()

	insertedId := helper.ToString(res.InsertedID)
	err = PostProductionStock(orgId, insertedId, userId, coolingData)
	if err != nil {
		log.Printf("Failed to post production stock: %v", err)
	}

	return err
}

// autoUpdateForCooling
//
// PURPOSE:
// This function automatically prepares and updates the "cooling" production process
// based on the output of a previous "borma" (or upstream) process.
//
// FLOW:
// 1. Identify current and next process templates
// 2. Fetch process-product mapping from DB (process_product collection)
// 3. Map previous process output products → current cooling input fields
// 4. Build cooling process data using previous stage output values
// 5. Set process metadata (equipment, weight, remarks, timestamps)
// 6. Validate stock before updating
// 7. Update production stock after validation
// 8. Upsert/update cooling production record in "productions" collection
//
// DATA TRANSFORMATION:
// - output_weight → input_weight
// - borma_product → cooling_product
// - process_end_date_time → process_start_date_time
// - previous batch tracking added
//
// SPECIAL HANDLING:
// - Image upload status is normalized to "Start"
// - Template is switched to next process template
// - Existing created_on / created_by removed to avoid overwrite conflict
//
// STOCK CONTROL:
// - ValidateProductionStockUpdate() ensures stock consistency
// - PutProductionStock() updates inventory after validation
//
// COLLECTION USED:
// - process_product (mapping logic)
// - productions (final update)
//
// NOTE:
// This function acts as an AUTOMATION BRIDGE between two production stages
// and ensures seamless process flow + inventory consistency.
func autoUpdateForCooling(orgId string, userId string, bormaData map[string]interface{}, prevoiusCoolingData map[string]interface{}, prevousbatchId string, Id string) error {
	coolingData := prevoiusCoolingData

	// Get the current and next template
	currentTemplate := bormaData["template_id"].(string)
	nextTemplate := findtheNextProcesstemplate(bormaData)

	// Build the pipeline to get previous and current product IDs
	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"template_id", currentTemplate},
					{"type", "output"},
				},
			},
		},
		bson.D{
			{"$project",
				bson.D{
					{"_id", 0},
					{"previousProduct", "$product_id"},
					{"currentProduct", "$product_id"},
				},
			},
		},
	}

	// Execute the pipeline
	result, err := helper.GetAggregateQueryResult(orgId, "process_product", pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// Map the previous product output to current product input
	if len(result) > 0 {
		for _, result := range result {
			if previousProductID, ok := result["previousProduct"].(string); ok {
				if currentProductID, ok := result["currentProduct"].(string); ok {
					if previousValue, exists := bormaData[previousProductID]; exists {
						coolingData[currentProductID] = previousValue
					}
					// if ParentId, ok := result["_id"].(string); ok {
					// 	if ParentValue, exists := bormaData[ParentId]; exists {
					// 		coolingData[ParentId] = ParentValue
					// 	}
					// }
				}
			}
		}
	}

	coolingData["input_weight"] = bormaData["output_weight"]
	coolingData["cooling_product"] = bormaData["borma_product"]
	coolingData["process_start_date_time"] = bormaData["process_end_date_time"]
	coolingData["prevous_batch_id"] = prevousbatchId
	if imageUpload, ok := bormaData["image_upload"].([]interface{}); ok && len(imageUpload) > 1 {
		if imgMap, ok := imageUpload[1].(map[string]interface{}); ok {
			imgMap["process_status"] = "Start"
			coolingData["image_upload"] = []interface{}{imgMap}
		}
	}
	coolingData["equipment_id"] = bormaData["cooling_equipment_id"]
	coolingData["trolley_weight"] = bormaData["trolley_weight"]
	coolingData["remarks"] = bormaData["remarks"]
	coolingData["template_id"] = nextTemplate
	if _, ok := coolingData["status"]; !ok || coolingData["status"] == "" {
		coolingData["status"] = "Start"
	}
	helper.UpdateDateObject(coolingData)
	delete(coolingData, "created_on")
	delete(coolingData, "created_by")

	created_on := time.Now()
	update := bson.M{
		"$set": coolingData,
		"$setOnInsert": bson.M{
			"created_on": created_on,
			"created_by": userId,
		},
	}
	coolingData["update_on"] = time.Now()
	coolingData["update_by"] = userId
	if err := ValidateProductionStockUpdate(orgId, coolingData, Id); err != nil {
		return shared.BadRequest(fmt.Sprintf("Stock validation failed: %v", err))
	}
	// Update stock after validation passes
	if err := PutProductionStock(orgId, Id, userId, coolingData); err != nil {
		return shared.BadRequest(fmt.Sprintf("Stock update failed: %v", err))
	}
	_, err = database.GetConnection(orgId).Collection("productions").UpdateOne(ctx, helper.DocIdFilter(Id), update, updateOpts)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	return err
}
// PostDocHandler
//
// PURPOSE:
// This is a generic REST API handler used to insert data into dynamic collections
// based on the URL parameter ":model_name".
//
// FLOW:
// 1. Validate organization & user token
// 2. Parse request body
// 3. Apply collection-specific business rules
// 4. Auto-generate IDs, timestamps, and status
// 5. Handle special workflows (production, sales, purchase, banking, etc.)
// 6. Insert data into MongoDB
// 7. Trigger post-insert side effects (ledger update, stock update, etc.)
//
// USAGE:
// POST /entities/:model_name
// Example: POST /entities/customer
//
// NOTE:
// This function acts as a centralized dynamic CRUD engine for multiple modules.
func PostDocHandler(c *fiber.Ctx) error {
	// Get organization
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("model_name")
	var inputData map[string]interface{}

	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	if collectionName == "lots" {
		inputData["lot_start_date_time"] = time.Now()
	}

	// Update Date Object
	helper.UpdateDateObject(inputData)
	helper.HandleIDGeneration(inputData, org.Id, collectionName)

	if _, ok := inputData["status"]; !ok || inputData["status"] == "" {
		inputData["status"] = "Active"
	}

	inputData["created_on"] = time.Now()
	inputData["created_by"] = userToken.UserId

	if collectionName == "organization" || collectionName == "master_menu" || collectionName == "role_acl" {
		org.Id = "shared"
	}

	if collectionName == "organization" {
		//	name := inputData["name"].(string) // Make sure this is a string
		emailId := inputData["email_id"].(string)
		var existUserData map[string]interface{}
		database.GetConnection("shared").Collection("temporary_user").FindOne(context.Background(), bson.M{"email_id": emailId}).Decode(&existUserData)
		if existUserData != nil {
			return shared.InternalServerError("Email Already Exists")
		}
		nxtSeq, _ := helper.GetNextSeqNumber("ORG", org.Id)
		year := time.Now().Year()

		orgId := "ORG-" + helper.ToString(year) + "-" + helper.ToString(nxtSeq)
		inputData["_id"] = orgId
		inputData["firstLogin"] = true
		// var userData map[string]interface{}
		// err := database.GetConnection("shared").Collection("temporary_user").FindOne(context.Background(), bson.M{"_id": emailId}).Decode(&userData)
		// if err != nil {
		// 	return shared.InternalServerError("No user Found")
		// }

		// Create db name: first 3 chars of name + "CERP"
		//

		// dbName := strings.ToUpper(dbPrefix) + "_cerp"
		// id, err := database.CreateNewMongoDatabase(dbName)
		// if err != nil {
		// 	return shared.InternalServerError(err.Error())
		// }

		// inputData["db_name"] = id

	}

	if collectionName == "productions" {
		processId, err := LotCreatingConfig(userToken.OrgId, inputData["factory_id"].(string))
		if err != nil {
			return shared.InternalServerError(err.Error())
		}
		reqProcessId := helper.ToInt32(inputData["process_id"])

		if processId == reqProcessId {
			value, err := LotCreating(userToken.OrgId, userToken.UserId, inputData)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}
			inputData["lot_number"] = value
		}
	}

	// if collectionName == "sale" {
	// 	if val, exists := inputData["einvoice_number"]; !exists || val == "" {
	// 		var factoryId string

	// 		if val, ok := inputData["factory"]; ok {
	// 			if str, ok := val.(string); ok {
	// 				factoryId = str
	// 			}
	// 		} else {
	// 			factoryId = userToken.FactoryId
	// 		}

	// 		if factoryId == "" {
	// 			return fmt.Errorf("factoryId is empty")
	// 		}

	// 		// Load from DB only if not present in cache
	// 		if _, ok := factoryconfig[factoryId]; !ok {
	// 			factoryDoc, err := GetDataById(org.Id, factoryId, "config")
	// 			if err == nil {
	// 				if einvoice, ok := factoryDoc["sale_einvoice"]; ok {
	// 					factoryconfig[factoryId] = einvoice
	// 				}
	// 			}
	// 		}

	// 		year := time.Now().Format("06")  // FY year (e.g., 25,26)
	// 		month := time.Now().Format("01") // 01–12

	// 		unique := "SEQ|" + factoryId + "-FY" + year + "-" + month + "-"

	// 		sno, _ := helper.HandleSequenceOrder(unique, org.Id, collectionName)
	// 		inputData["einvoice_number"] = sno
	// 	}
	// }

	// if collectionName == "customer" {
	// 	inputData["status"] = "INVITED"
	// }

	// Validate stock before inserting production
	if collectionName == "productions" && inputData["other_worker_salary"] == nil {
		if err := ValidateProductionStockUpdate(org.Id, inputData, ""); err != nil {
			return shared.BadRequest(fmt.Sprintf("Stock validation failed: %v", err))
		}
	}

	// Validate consignment_status before creating "In progress" record
	if collectionName == "consignment_status" {
		if status, ok := inputData["status"].(string); ok && status == "In progress" {
			warehouseID, _ := inputData["warehouse_id"].(string)
			if warehouseID != "" {
				var existingConsignment map[string]interface{}
				filter := bson.M{
					"warehouse_id": warehouseID,
					"status":       "In progress",
				}
				err := database.GetConnection(org.Id).Collection("consignment_status").FindOne(context.Background(), filter).Decode(&existingConsignment)
				if err == nil && existingConsignment != nil {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"success": false,
						"message": "Already one consignment is in progress. Please close it before starting another.",
					})
				}
			}
		}
	}

	// Insert data into the database
	res, err := Insert(org.Id, collectionName, inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	if collectionName == "reports" && org.Id == "TEAMALPHA" {
		inputData["_id"] = res.InsertedID
		go sendAdminReportFcmNotification(org.Id, inputData)
	}

	if collectionName == "bank_details" {
		bankName, ok := inputData["bank_name"].(string)
		if !ok {
			return shared.BadRequest("invalid bank_name")
		}

		// trim left & right spaces
		bankName = strings.TrimSpace(bankName)

		var closingBalance float64 = 0

		if val, exists := inputData["closing_balance"]; exists {
			switch v := val.(type) {
			case float64:
				closingBalance = v
			case int:
				closingBalance = float64(v)
			case string:
				if v != "" {
					parsed, err := strconv.ParseFloat(v, 64)
					if err == nil {
						closingBalance = parsed
					}
				}
			}
		}

		// replace middle spaces with '-'
		bankID := strings.ReplaceAll(bankName, " ", "-")

		// optional: normalize case
		// bankID = strings.ToLower(bankID)

		bnkUpdate := map[string]interface{}{
			"parent_id":   "BANKS",
			"name":        inputData["bank_name"],
			"_id":         bankID,
			"created_by":  userToken.UserId,
			"created_on":  time.Now(),
			"description": "",
			"status":      "Active",
		}
		_, err := Insert(org.Id, "account_head", bnkUpdate)
		if err != nil {
			return shared.BadRequest(err.Error())
		}

		//  Insert into cash_in_hand collection (parallel entry)

		cashEntry := map[string]interface{}{
			"_id":             bankID,
			"factory_id":      inputData["factory_id"],
			"closing_balance": closingBalance,
			"date_of_entry":   inputData["date_of_entry"],
			"created_by":      userToken.UserId,
			"created_on":      time.Now(),
			"updated_by":      userToken.UserId,
			"updated_on":      time.Now(),
			"account_head_id": "BANKS",
		}

		_, err = Insert(org.Id, "cash_in_hand", cashEntry)
		if err != nil {
			return shared.BadRequest("cash_in_hand insert failed: " + err.Error())
		}
	}
	if collectionName == "customer" {
		bankName, ok := inputData["_id"].(string)
		if !ok {
			return shared.BadRequest("invalid bank_name")
		}

		// trim left & right spaces
		bankName = strings.TrimSpace(bankName)

		// replace middle spaces with '-'
		bankID := strings.ReplaceAll(bankName, " ", "-")

		// optional: normalize case
		// bankID = strings.ToLower(bankID)

		bnkUpdate := map[string]interface{}{
			"parent_id":   "CUSTOMERS",
			"name":        inputData["customer_name"],
			"_id":         bankID,
			"created_by":  userToken.UserId,
			"created_on":  time.Now(),
			"description": "",
			"status":      "Active",
		}
		_, err := Insert(org.Id, "account_head", bnkUpdate)
		if err != nil {
			return shared.BadRequest(err.Error())
		}
	}

	// if collectionName == "sold_products_info" {
	// 	updatePacking(inputData["batch_no"].([]interface{}), org.Id, userToken.UserId, res.InsertedID.(string))
	// }

	// if collectionName == "sale" {
	// 	inputData["ref_id"] = res.InsertedID.(string)
	// 	ProcessSale(org.Id, inputData, userToken.UserId)
	// }

	if collectionName == "organization" {
		InsertUser(inputData)
	}

	// if collectionName == "customer" {
	// 	var loginAllowed bool

	// 	loginAllowed = inputData["is_login_available"].(bool)
	// 	if loginAllowed {
	// 		CreateServiceProvider(inputData, userToken.OrgId, helper.ToString(res.InsertedID))
	// 	}
	// }

	// Load the struct if new data is available on data_model collection
	if res.InsertedID != nil { //collectionName == "data_model" &&
		helper.ServerInitstruct(org.Id)
	}

	if collectionName == "maintance_details" {
		id := res.InsertedID.(string)
		helper.GenerateMultipleMaintenanceData(inputData, org.Id, id, nil, true)
	}

	if collectionName == "jobwork_details" {
		log.Println("job work triggered", inputData)

		// Extract IDs properly
		jobworkID := fmt.Sprintf("%v", inputData["jobwork_id"])
		purchaseID := fmt.Sprintf("%v", inputData["purchase_id"])
		templateID := helper.ToString(inputData["template_id"])

		if purchaseID == "" || purchaseID == "<nil>" {
			//ftech purchasde id from outward
			outward_id := inputData["outward_jobwork_id"].(string)
			outwardJobwork, err := GetDataById(org.Id, outward_id, "jobwork_details")
			if err != nil {
				log.Println("outward jobwork data not found:", err)
				outwardJobwork = map[string]interface{}{}
			}
			purchaseID = outwardJobwork["purchase_id"].(string)

		}
		// ---------------------------------------------------
		// FETCH JOBWORK DETAILS
		// ---------------------------------------------------
		jobwork, err := GetDataById(org.Id, jobworkID, "job_work")
		if err != nil {
			log.Println("jobwork data not found:", err)
			jobwork = map[string]interface{}{}
		}

		jobworkTemplate, err := GetDataById(org.Id, templateID, "jobwork_template")
		if err != nil {
			log.Println("jobwork data not found:", err)
			jobworkTemplate = map[string]interface{}{}
		}

		actionType := fmt.Sprintf("%v", jobwork["type"]) // Safe type fetch

		// ---------------------------------------------------
		// FETCH PURCHASE DETAILS
		// ---------------------------------------------------
		purchase, err := GetDataById(org.Id, purchaseID, "purchase")
		if err != nil {
			log.Println("purchase data not found:", err)
			purchase = map[string]interface{}{}
		}

		if actionType == "outWard-jobWork" {
			fmt.Println("TRUE — matched")
		} else {
			fmt.Println("FALSE — did not match")
		}
		ProcessJobWorkStockMovement(inputData, jobwork, jobworkTemplate, purchase, org.Id, userToken.UserId, actionType, res.InsertedID.(string), purchaseID)
		CheckServiceProviderLogin(inputData, org.Id, userToken.UserId, res.InsertedID.(string))
		// Stock movement continues...
	}

	if collectionName == "holiday_configuration" {
		id := res.InsertedID.(string)
		helper.CalculateLeavesAndUpsert(inputData, id, org.Id)
	}

	if collectionName == "sale" {
		//domestic sale
		if inputData["type_of_sale"] != "High Sea Sale" && inputData["type_of_sale"] != "kernel" {
			if inputData["origin"] == nil {
				purchaseData, err := GetDataById(org.Id, inputData["purchase_id"].(string), "purchase")
				if err == nil {
					inputData["origin"] = purchaseData["country_origin"]
					inputData["sale_id"] = res.InsertedID
				}
			}
			SaleLedgerUpdate(inputData, org.Id, userToken.UserId)
		}

	}
	if collectionName == "purchase_products_info" {
		//kernel puerchase
		KernalAndOtherPurchaseUpdate(inputData, org.Id, userToken.UserId)
	}
	if collectionName == "sold_products_info" {
		//kernel sale
		err := UpdateKernelInventorySerailNumber(inputData, org.Id, userToken.UserId)
		if err != nil {
			return shared.BadRequest(fmt.Sprintf("Failed to update kernel inventory: %v", err))
		}
		err = KernalAndOtherSaleUpdate(inputData, org.Id, userToken.UserId)
		if err != nil {
			return shared.BadRequest(fmt.Sprintf("Failed to process sale: %v", err))
		}
	}

	if collectionName == "stock_transfer" {
		inputData["_id"] = res.InsertedID
		ProcessStockTransafer(org.Id, inputData, userToken.UserId)
	}

	if collectionName == "productions" && inputData["other_worker_salary"] == nil {
		if inputData["process_type"] == "PACK" {
			packingId, _ := inputData["type_of_packing"].(string)

			if packingId == "005" {
				fmt.Println("New process for 005")

				facId := inputData["factory_id"].(string)
				facPrefix := strings.ToUpper(facId[:3])
				seqData := "kernel-pack-" + facId
				seq, _ := helper.GetNextSeqNumber(seqData, org.Id)
				pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)
				serialData := map[string]interface{}{
					"_id":             pac,
					"s_no":            inputData["serial_no"],
					"status":          "packed",
					"production_id":   res.InsertedID,
					"purchase_id":     inputData["purchase_id"],
					"stock_from":      "production",
					"created_on":      time.Now(),
					"created_by":      userToken.UserId,
					"quantity":        inputData["weight"],
					"product_id":      inputData["product_id"],
					"type_of_packing": inputData["type_of_packing"],
				}
				Insert(org.Id, "kernel_inventory", serialData)

				ProductionKernelSTockInUpdate(org.Id, inputData, userToken.UserId, false)
			} else {
				startSerialNo := helper.InterfaceToInt64(inputData["start_serial_no"])
				endSerialNo := helper.InterfaceToInt64(inputData["end_serial_no"])
				packingTypeData, _ := GetDataById(org.Id, inputData["type_of_packing"].(string), "lookup")
				packingValue := helper.ToFloat64(packingTypeData["value"])
				for i := startSerialNo; i <= endSerialNo; i++ {
					facId := inputData["factory_id"].(string)
					facPrefix := strings.ToUpper(facId[:3])
					seqData := "kernel-pack-" + facId
					seq, _ := helper.GetNextSeqNumber(seqData, org.Id)
					pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)
					serialData := map[string]interface{}{
						"_id":             pac,
						"s_no":            i,
						"status":          "packed",
						"production_id":   res.InsertedID,
						"purchase_id":     inputData["purchase_id"],
						"stock_from":      "production",
						"created_on":      time.Now(),
						"created_by":      userToken.UserId,
						"quantity":        packingValue,
						"product_id":      inputData["product_id"],
						"type_of_packing": inputData["type_of_packing"],
					}
					Insert(org.Id, "kernel_inventory", serialData)
				}

				ProductionKernelSTockInUpdate(org.Id, inputData, userToken.UserId, false)
			}
		} else if inputData["process_type"] == "COOK" {
			// origin := ""
			// if purchaseId, ok := inputData["purchase_id"].(string); ok {
			// 	if purchase, err := GetDataById(org.Id, purchaseId, "purchase"); err == nil {
			// 		if countryOrigin, ok := purchase["country_origin"].(string); ok {
			// 			origin = countryOrigin
			// 		}
			// 	}
			// }
			// cokData := map[string]interface{}{
			// 	"filled_tins":    inputData["input_weight"],
			// 	"purchase_id":    inputData["purchase_id"],
			// 	"country_origin": origin,
			// 	"factory_id":     inputData["factory_id"],
			// 	"_id":            res.InsertedID,
			// 	"product_id":     "rcn",
			// 	"process_type":   "COOK",
			// }
			// ProcessRCNCooking(org.Id, cokData, userToken.UserId)
		}

		// ProductionProductLevelUpdates(org.Id, inputData, nil)

		//process operation ledger update

		// err := ProcessOperation(org.Id, inputData, userToken.UserId)
		// if err != nil {
		// 	// Log error but don't fail the main operation
		// 	log.Printf("ProcessOperation failed: %v", err)
		// }

		// Update production summary in background
		go func() {
			err := UpdateProductionSummary(org.Id, inputData)
			if err != nil {
				log.Printf("Failed to update production summary: %v", err)
			}
		}()

		// Update production stock in background
		if inputData["process_type"] != "PACK" {
			insertedId := helper.ToString(res.InsertedID)
			err := PostProductionStock(org.Id, insertedId, userToken.UserId, inputData)
			if err != nil {
				log.Printf("Failed to post production stock: %v", err)
			}
		}

	}

	if collectionName == "employee" {
		if inputData["email"] != nil {

			userExists := CheckUser(inputData["email"].(string), org.Id)
			if userExists {
				InsertEmployeeAsUser(inputData, org.Id)
			} else {
				UpdateUser(inputData, org.Id)
			}
		}
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message":   "Insert Successfully",
		"insert ID": res.InsertedID,
	})

}
// KernalInventory
//
// PURPOSE:
// This API is used to create or update Kernel Purchase Inventory data
// in a dynamic (upsert) manner inside the "kernal_purchase_data" collection.
//
// FLOW:
// 1. Validate organization and user token
// 2. Parse request body
// 3. Set default fields (status, created_on, created_by)
// 4. Check if ID exists:
//      - If yes → Update existing record
//      - If no  → Create new record with generated unique ID
// 5. Perform MongoDB Upsert operation (Insert or Update)
// 6. Fetch updated kernel data and related purchase data
// 7. Safely construct merged data object
// 8. Trigger async business logic:
//      - Kernel purchase update
//      - Inventory creation
// 9. Return final inserted/updated ID
//
// NOTE:
// - Uses Upsert (UpdateOne with SetUpsert=true)
// - Heavy business logic runs asynchronously (goroutines)
// - Safe nil handling used for dependent collections
//
// ENDPOINT:
// POST /kernel-inventory/:id (optional id for update)
func KernalInventory(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	var inputData map[string]interface{}

	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	helper.UpdateDateObject(inputData)

	if _, ok := inputData["status"]; !ok || inputData["status"] == "" {
		inputData["status"] = "Active"
	}

	inputData["created_on"] = time.Now()
	inputData["created_by"] = userToken.UserId

	id := c.Params("id")

	// If ID exists → update. Else → create new
	if id != "" {
		inputData["_id"] = id
	} else {
		inputData["_id"] = helper.Generateuniquekey()
	}

	updateFilter := bson.M{"_id": inputData["_id"]}

	upsert := true
	opts := options.Update().SetUpsert(upsert)

	db := database.GetConnection(org.Id)

	// Replace document on upsert
	update := bson.M{"$set": inputData}

	res, err := db.Collection("kernal_purchase_data").UpdateOne(context.Background(), updateFilter, update, opts)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// Determine final ID
	var finalID string
	if res.UpsertedID != nil {
		finalID = res.UpsertedID.(string)
	} else {
		finalID = inputData["_id"].(string)
	}
	// Get Old Kernel Data
	// -------------------------------
	var kernelOldData map[string]interface{}
	var purchaseData map[string]interface{}

	kernelOldData, err = GetDataById(org.Id, finalID, "kernal_purchase_data")
	if err != nil {
		kernelOldData = nil
	}

	// Extract purchase_template_id safely
	purchaseTemplateId := ""
	if kernelOldData != nil {
		if v, ok := kernelOldData["purchase_template_id"].(string); ok {
			purchaseTemplateId = v
		}
	}

	// -------------------------------
	// Get Purchase Data
	// -------------------------------
	purchaseData, err = GetDataById(org.Id, purchaseTemplateId, "purchase")
	if err != nil {
		purchaseData = nil
	}

	// Helper function to safely get values
	safeGet := func(data map[string]interface{}, key string) interface{} {
		if data == nil {
			return nil
		}
		if v, ok := data[key]; ok {
			return v
		}
		return nil
	}

	// -------------------------------
	// Constructed Data (SAFE ACCESS)
	// -------------------------------

	constructedData := map[string]interface{}{
		"product_id":  safeGet(kernelOldData, "product_id"),
		"warehouse":   safeGet(kernelOldData, "warehouse_id"),
		"origin":      safeGet(kernelOldData, "origin_id"),
		"template_id": safeGet(purchaseData, "_id"),
		"customer_id": safeGet(purchaseData, "customer_id"),
		"quantity":    safeGet(kernelOldData, "quantity"),
		"_id":         finalID,
	}

	go KernalAndOtherPurchaseUpdate(constructedData, org.Id, userToken.UserId)

	go helper.InventoryCreation(
		inputData,
		finalID,
		userToken,
		db.Collection("kernel_inventory"),
	)
	// }

	return shared.SuccessResponse(c, fiber.Map{
		"message":     "Inserted Successfully",
		"purchase_id": finalID,
	})
}
// ProcessPettyCash
//
// This function handles all petty cash and bank-related transactions.
// It supports receivable, payable, expenses, deposits, and bank transfers.
//
// It performs the following operations:
// - Validates required input fields (factory_id, type, amount)
// - Determines transaction type and builds auto description
// - Fetches last cash/bank ledger entry
// - Calculates opening and closing balance
// - Prevents negative balance (insufficient funds check)
// - Handles bank-specific operations (withdraw, deposit, transfer, DD)
// - Creates voucher_details entry for audit tracking
// - Inserts transaction into main collection
// - Updates cash_in_hand ledger using atomic increment (upsert)
//
// Endpoint:
// POST /petty-cash/:modelName
//
// Note: This is a critical financial module that controls real-time cash flow.
func ProcessPettyCash(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("modelName")

	var inputData map[string]interface{}
	if err := c.BodyParser(&inputData); err != nil {
		return shared.BadRequest(err.Error())
	}

	helper.UpdateDateObject(inputData)

	// ---------------- VALIDATIONS ----------------
	factoryID, ok := inputData["factory_id"].(string)
	if !ok || factoryID == "" {
		return shared.BadRequest("factory_id is required")
	}

	transactionType, ok := inputData["type"].(string)
	if !ok || transactionType == "" {
		return shared.BadRequest("transaction type is required")
	}

	amount := helper.ToFloat64(inputData["amount"])
	if amount <= 0 {
		return shared.BadRequest("invalid amount")
	}

	if _, ok := inputData["status"]; !ok {
		inputData["status"] = "Active"
	}

	// ---------------- SET META ----------------
	inputData["_id"] = uuid.New().String()
	inputData["created_on"] = time.Now()
	inputData["created_by"] = userToken.UserId

	// ---------------- GET LAST LEDGER ----------------
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var lastLedgerEntry map[string]interface{}
	opts := options.FindOne().SetSort(bson.M{"created_on": -1})
	var filter bson.M
	var autoDescription string

	var CashInHandID string
	var cashInHandMultiplyer float64
	if transactionType == "receivable" {
		var to string
		if inputData["to"] != nil {
			to = inputData["to"].(string)
		}
		customerID := inputData["from"].(string)
		custData, err := GetDataById(org.Id, customerID, "customer")
		if err != nil {
			return shared.InternalServerError("Account Head Not Found")
		}
		customerName := custData["customer_name"].(string)
		cashInHandMultiplyer = 1
		if to == "cash" {
			cashId := inputData["cash_name"].(string)
			filter = bson.M{"factory_id": factoryID, "_id": cashId}
			CashInHandID = cashId
			cashData, err := GetDataById(org.Id, cashId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var cashName string
			if cashData["name"] != nil {
				cashName = cashData["name"].(string)
			}
			autoDescription = fmt.Sprintf(
				"Amount received from %s and credited to %s",
				customerName,
				cashName,
			)

		} else {
			bankId := inputData["bank_name"].(string)
			bankData, err := GetDataById(org.Id, bankId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var bankName string
			if bankData["name"] != nil {
				bankName = bankData["name"].(string)
			}
			CashInHandID = bankId
			autoDescription = fmt.Sprintf(
				"Amount received from %s and credited to %s",
				customerName,
				bankName,
			)

			filter = bson.M{"factory_id": factoryID, "_id": bankId}
		}
	} else if transactionType == "payable" {
		var from string
		var to string
		var customerName string
		if inputData["from"] != nil {
			from = inputData["from"].(string)
		}
		if inputData["to"] != nil {
			to = inputData["to"].(string)
		}
		cashInHandMultiplyer = -1
		custData, err := GetDataById(org.Id, to, "customer")
		if err != nil {
			return shared.InternalServerError("customer Not Found")
		}
		customerName = custData["customer_name"].(string)

		if from == "cash" {
			cashId := inputData["cash_name"].(string)
			CashInHandID = cashId
			cashData, err := GetDataById(org.Id, cashId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var cashName string
			if cashData["name"] != nil {
				cashName = cashData["name"].(string)
			}
			autoDescription = "Amount paid to " + customerName + " from " + cashName

			filter = bson.M{"factory_id": factoryID, "_id": cashId}
		} else {
			bankId := inputData["bank_name"].(string)
			CashInHandID = bankId
			bankData, err := GetDataById(org.Id, bankId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var bankName string
			if bankData["name"] != nil {
				bankName = bankData["name"].(string)
			}
			autoDescription = "Amount paid to " + customerName + " from " + bankName

			filter = bson.M{"factory_id": factoryID, "_id": bankId}
		}
	} else if transactionType == "exp" {
		var from string
		var accHead string
		var accHeadID string
		if inputData["from"] != nil {
			from = inputData["from"].(string)
		}
		if inputData["account_head"] != nil {
			accHeadID = inputData["account_head"].(string)
		}
		cashInHandMultiplyer = -1
		custData, err := GetDataById(org.Id, accHeadID, "account_head")
		if err != nil {
			return shared.InternalServerError("Account Head Not Found")
		}
		accHead = custData["name"].(string)

		if from == "cash" {
			cashId := inputData["cash_name"].(string)
			filter = bson.M{"factory_id": factoryID, "_id": cashId}
			CashInHandID = cashId
			cashData, err := GetDataById(org.Id, cashId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var cashName string
			if cashData["name"] != nil {
				cashName = cashData["name"].(string)
			}
			autoDescription = "Amount paid towards " + accHead + " from " + cashName

		} else {
			bankId := inputData["bank_name"].(string)
			filter = bson.M{"factory_id": factoryID, "_id": bankId}
			CashInHandID = bankId
			bankData, err := GetDataById(org.Id, bankId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var bankName string
			if bankData["name"] != nil {
				bankName = bankData["name"].(string)
			}
			autoDescription = "Amount paid towards " + accHead + " from " + bankName
		}
	} else if transactionType == "bank" {
		bankId := inputData["bank_name"].(string)
		typeOfTransaction := inputData["type_of_transaction"].(string)
		cashId := ""
		bankData, err := GetDataById(org.Id, bankId, "account_head")
		if err != nil {
			return shared.InternalServerError("Account Head Not Found")
		}
		var bankName string
		if bankData["name"] != nil {
			bankName = bankData["name"].(string)
		}

		if inputData["cash_name"] != nil {
			cashId = inputData["cash_name"].(string)
		}
		var cashName string
		if cashId != "" {
			cashData, err := GetDataById(org.Id, cashId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}

			if cashData["name"] != nil {
				cashName = cashData["name"].(string)
			}
		}

		switch typeOfTransaction {
		case "cash_withdraw":

			filter = bson.M{"factory_id": factoryID, "_id": bankId}
			cashInHandMultiplyer = -1
			CashInHandID = bankId
			autoDescription = "Amount withdrawn from " + bankName + " and credited to " + cashName
		case "cash_deposit":

			cashInHandMultiplyer = -1
			CashInHandID = cashId
			filter = bson.M{"factory_id": factoryID, "_id": cashId}
			autoDescription = "Amount withdrawn from " + bankName + " and credited to " + cashName
		case "dd_taken_from_other_payment":
			filter = bson.M{"factory_id": factoryID, "_id": bankId}
			cashInHandMultiplyer = -1
			CashInHandID = bankId
			autoDescription = "Amount withdrawn from " + bankName
		case "amount_transfer_to_other_bank":

			cashInHandMultiplyer = -1
			CashInHandID = bankId

			filter = bson.M{"factory_id": factoryID, "_id": bankId}
			autoDescription = "Amount withdrawn from " + bankName + " and credited to " + cashName
		}

	} else if transactionType == "deposit_entry" {
		var to string
		if inputData["to"] != nil {
			to = inputData["to"].(string)
		}
		// customerID := inputData["from"].(string)
		// custData, err := GetDataById(org.Id, customerID, "customer")
		// if err != nil {
		// 	return shared.InternalServerError("Account Head Not Found")
		// }
		// customerName := custData["customer_name"].(string)
		cashInHandMultiplyer = 1
		if to == "cash" {
			cashId := inputData["cash_name"].(string)
			filter = bson.M{"factory_id": factoryID, "_id": cashId}
			CashInHandID = cashId
			cashData, err := GetDataById(org.Id, cashId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var cashName string
			if cashData["name"] != nil {
				cashName = cashData["name"].(string)
			}
			autoDescription = fmt.Sprintf(
				"Amount received from %s and credited to %s",
				// customerName,
				cashName,
			)

		} else {
			bankId := inputData["bank_name"].(string)
			bankData, err := GetDataById(org.Id, bankId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var bankName string
			if bankData["name"] != nil {
				bankName = bankData["name"].(string)
			}
			CashInHandID = bankId
			autoDescription = fmt.Sprintf(
				"Amount received from %s and credited to %s",
				// customerName,
				bankName,
			)

			filter = bson.M{"factory_id": factoryID, "_id": bankId}
		}
	}

	err := database.
		GetConnection(org.Id).
		Collection("cash_in_hand").
		FindOne(ctx, filter, opts).
		Decode(&lastLedgerEntry)

	openingBalance := 0.0
	if err == nil {
		openingBalance = helper.ToFloat64(lastLedgerEntry["closing_balance"])
	} else if err != mongo.ErrNoDocuments {
		openingBalance = 0
	}

	// ---------------- CALCULATE BALANCE ----------------
	var closingBalance float64
	switch transactionType {
	case "receivable":
		closingBalance = openingBalance + amount
	case "deposit_entry":
		closingBalance = openingBalance + amount
	case "payable", "exp":
		closingBalance = openingBalance - amount
	case "bank":
		typeOfTransactions := inputData["type_of_transaction"]
		switch typeOfTransactions {
		case "cash_withdraw", "amount_transfer_to_other_bank", "dd_taken_from_other_payment", "cash_deposit":
			closingBalance = openingBalance - amount

			// case "cash_deposit":
			// 	closingBalance = openingBalance + amount
		}
	default:
		return shared.BadRequest("invalid transaction type")
	}
	if closingBalance < 0 {
		return shared.BadRequest("Insufficient balance")
	}
	if transactionType == "bank" {
		bankId := inputData["bank_name"].(string)
		typeOfTransaction := inputData["type_of_transaction"].(string)
		cashId := ""
		if inputData["cash_name"] != nil {
			cashId = inputData["cash_name"].(string)
		}

		switch typeOfTransaction {
		case "cash_withdraw":

			err := PettyCashParallelEntry(org.Id, "CR", factoryID, false, bankId, cashId, amount, inputData)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}
			filter = bson.M{"factory_id": factoryID, "_id": bankId}
			cashInHandMultiplyer = -1

		case "cash_deposit":

			err := PettyCashParallelEntry(org.Id, "CR", factoryID, true, bankId, cashId, amount, inputData)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}
			cashInHandMultiplyer = -1
			CashInHandID = cashId

		case "dd_taken_from_other_payment":
			filter = bson.M{"factory_id": factoryID, "_id": bankId}
			cashInHandMultiplyer = -1
			err := PettyCashParallelEntry(org.Id, "CR", factoryID, false, bankId, cashId, amount, inputData)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}

		case "amount_transfer_to_other_bank":
			recivedBank := inputData["recived_bank"].(string)
			cashInHandMultiplyer = -1
			CashInHandID = bankId
			err := PettyCashParallelEntry(org.Id, "CR", factoryID, true, recivedBank, cashId, amount, inputData)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}

		}
	}

	// ---------------- PREPARE VOUCHER (COPY MAP) ----------------
	inputVoucherDetails := make(map[string]interface{})
	for k, v := range inputData {
		inputVoucherDetails[k] = v
	}

	inputVoucherDetails["_id"] = uuid.New().String()
	inputVoucherDetails["voucher_id"] = inputData["_id"]
	inputVoucherDetails["opening_balance"] = openingBalance
	inputVoucherDetails["closing_balance"] = closingBalance
	inputVoucherDetails["available_balance"] = closingBalance
	inputVoucherDetails["auto_description"] = autoDescription
	// ---------------- INSERTS ----------------
	res, err := Insert(org.Id, "voucher_details", inputVoucherDetails)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	if _, err = Insert(org.Id, collectionName, inputData); err != nil {
		return shared.BadRequest(err.Error())
	}
	cashInHandfilter := bson.M{
		"factory_id": factoryID,
		"_id":        CashInHandID,
	}

	update := bson.M{
		"$inc": bson.M{
			"closing_balance": amount * cashInHandMultiplyer, // use -amount for debit
		},
		"$set": bson.M{
			"updated_on": time.Now(),
			"updated_by": userToken.UserId,
		},
		"$setOnInsert": bson.M{
			"_id":        CashInHandID,
			"created_on": time.Now(),
			"created_by": userToken.UserId,
		},
	}

	cashInHandopts := options.Update().SetUpsert(true)

	database.
		GetConnection(org.Id).
		Collection("cash_in_hand").
		UpdateOne(ctx, cashInHandfilter, update, cashInHandopts)

	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message":  "Inserted successfully",
		"insertId": res.InsertedID,
	})
}
// PettyCashParallelEntry
//
// PURPOSE:
// This function handles parallel ledger entry updates for petty cash transactions,
// including both BANK and CASH accounts. It records voucher details and updates
// the cash_in_hand balance accordingly.
//
// FLOW:
// 1. Identify whether transaction is BANK or CASH based
// 2. Fetch latest balance from cash_in_hand collection
// 3. Calculate opening balance
// 4. Apply transaction type logic (CR / DR)
// 5. Compute closing balance
// 6. Create voucher entry in voucher_details collection
// 7. Update cash_in_hand balance using $inc
// 8. Ensure upsert for new accounts
//
// TRANSACTION TYPES:
// - CR (Credit) → Adds amount to balance
// - DR (Debit)  → Subtracts amount from balance
//
// BUSINESS LOGIC:
// - BANK or CASH account is selected dynamically
// - Maintains ledger consistency between voucher_details and cash_in_hand
// - Ensures real-time balance tracking per factory
//
// COLLECTIONS USED:
// - cash_in_hand → current balance tracking
// - voucher_details → transaction history log
//
// UPDATES:
// - closing_balance is incrementally updated
// - new accounts are auto-created (upsert)
//
// NOTE:
// This function is critical for maintaining financial consistency across
// petty cash and bank transactions in the system.
func PettyCashParallelEntry(orgID string, txnType string, factoryID string, bank bool, bankID string, cashID string, amount float64, inputData map[string]interface{}) error {

	var filter bson.M
	var cashInHandMultiplyer float64
	var CashInHandID string
	var lastLedgerEntry map[string]interface{}
	inputVoucherDetails := make(map[string]interface{})
	opts := options.FindOne().SetSort(bson.M{"created_on": -1})
	if bank {
		filter = bson.M{"factory_id": factoryID, "_id": bankID}
		inputVoucherDetails["to"] = "bank"
		inputVoucherDetails["bank_name"] = bankID
		CashInHandID = bankID

	} else {
		filter = bson.M{"factory_id": factoryID, "_id": cashID}
		inputVoucherDetails["to"] = "cash"
		inputVoucherDetails["cash_name"] = cashID
		CashInHandID = cashID
	}

	err := database.
		GetConnection(orgID).
		Collection("cash_in_hand").
		FindOne(ctx, filter, opts).
		Decode(&lastLedgerEntry)

	openingBalance := 0.0
	if err == nil {
		openingBalance = helper.ToFloat64(lastLedgerEntry["closing_balance"])
	} else if err != mongo.ErrNoDocuments {
		openingBalance = 0
	}
	var closingBalance float64
	if txnType == "DR" {
		closingBalance = openingBalance - amount
		if openingBalance == 0 {
			return fmt.Errorf("Insufficient balance")
		}
		cashInHandMultiplyer = -1
	} else if txnType == "CR" {
		closingBalance = openingBalance + amount
		cashInHandMultiplyer = 1
		// if openingBalance == 0 {
		// 	return fmt.Errorf("Insufficient balance")
		// }
	}

	inputVoucherDetails["_id"] = uuid.New().String()
	inputVoucherDetails["voucher_id"] = inputData["_id"]
	inputVoucherDetails["opening_balance"] = openingBalance
	inputVoucherDetails["closing_balance"] = closingBalance
	inputVoucherDetails["type"] = inputData["type"]
	inputVoucherDetails["type_of_transaction"] = inputData["type_of_transaction"]
	inputVoucherDetails["available_balance"] = closingBalance
	inputVoucherDetails["transactionDate"] = inputData["transactionDate"]

	inputVoucherDetails["amount"] = inputData["amount"]
	inputVoucherDetails["factory_id"] = inputData["factory_id"]

	_, err = Insert(orgID, "voucher_details", inputVoucherDetails)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	cashInHandfilter := bson.M{
		"factory_id": factoryID,
		"_id":        CashInHandID,
	}

	update := bson.M{
		"$inc": bson.M{
			"closing_balance": amount * cashInHandMultiplyer, // use -amount for debit
		},
		"$set": bson.M{
			"updated_on": time.Now(),
		},
		"$setOnInsert": bson.M{
			"_id":        CashInHandID,
			"created_on": time.Now(),
		},
	}

	cashInHandopts := options.Update().SetUpsert(true)

	database.
		GetConnection(orgID).
		Collection("cash_in_hand").
		UpdateOne(ctx, cashInHandfilter, update, cashInHandopts)

	if err != nil {
		return shared.BadRequest(err.Error())
	}
	return nil
}
// ProcessPettyCashRentry
//
// PURPOSE:
// This function is used for re-processing or rebuilding petty cash / voucher ledger
// from existing "cash_ledger" data into "voucher_details" collection.
//
// It is mainly used for:
// - Data migration / correction (re-entry process)
// - Rebuilding voucher ledger history
// - Normalizing transaction structure from legacy cash ledger data
//
// FLOW:
// 1. Fetch organization context
// 2. Load all cash ledger records (sorted by created date)
// 3. Loop through each ledger entry
// 4. Normalize transaction type (Credit/Debit → receivable/payable/exp/bank)
// 5. Build transaction metadata (customer, bank, cash, account head)
// 6. Calculate opening balance from last voucher entry
// 7. Compute closing balance
// 8. Validate insufficient balance
// 9. Construct voucher record
// 10. Insert into voucher_details + original collection
//
// IMPORTANT:
// - This is a batch processing / data re-entry utility
// - Used for historical ledger reconstruction
// - Should NOT be used for live transactions
//
// ENDPOINT:
// POST /reentry/:modelName
func ProcessPettyCashRentry(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	org.Id = "604162a4ce67408c8b22870191199ad3"

	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("modelName")
	collectionName = "voucher"

	getDataPipeline := bson.A{
		bson.D{{"$sort", bson.D{{"created_on", 1}}}},
	}

	// var inputData map[string]interface{}
	// if err := c.BodyParser(&inputData); err != nil {
	// 	return shared.BadRequest(err.Error())
	// }

	ledgerData, err := helper.GetAggregateQueryResult(org.Id, "cash_ledger", getDataPipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	for _, obj := range ledgerData {

		// helper.UpdateDateObject(inputData)

		// ---------------- VALIDATIONS ----------------
		factoryID, ok := obj["factory_id"].(string)
		if !ok || factoryID == "" {
			return shared.BadRequest("factory_id is required")
		}

		obj["account_head"] = obj["purchase_head"]

		transactionTYpe := obj["transactionType"].(string)
		var transactionType string
		if transactionTYpe == "Credit / Income" {
			transactionType = "bank"
		} else if transactionTYpe == "Debit / Expense" {
			transactionType = "exp"
			obj["from"] = "cash"
			obj["type"] = "exp"
			obj["cash_name"] = "DCASH"
			obj["payment_method"] = "cash"
		}

		// transactionType, ok = obj["type"].(string)
		// if !ok || transactionType == "" {
		// 	return shared.BadRequest("transaction type is required")
		// }

		amount := helper.ToFloat64(obj["amount"])
		if amount <= 0 {
			return shared.BadRequest("invalid amount")
		}

		if _, ok := obj["status"]; !ok {
			obj["status"] = "Active"
		}

		// ---------------- SET META ----------------
		obj["_id"] = uuid.New().String()
		obj["created_on"] = time.Now()
		obj["created_by"] = userToken.UserId

		// ---------------- GET LAST LEDGER ----------------
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var lastLedgerEntry map[string]interface{}
		opts := options.FindOne().SetSort(bson.M{"created_on": -1})
		var filter bson.M
		var autoDescription string
		if transactionType == "receivable" {
			var to string
			if obj["to"] != nil {
				to = obj["to"].(string)
			}
			customerID := obj["from"].(string)
			custData, err := GetDataById(org.Id, customerID, "customer")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			customerName := custData["customer_name"].(string)
			if to == "cash" {
				cashId := obj["cash_name"].(string)
				filter = bson.M{"factory_id": factoryID, "cash_name": cashId}
				cashData, err := GetDataById(org.Id, cashId, "account_head")
				if err != nil {
					return shared.InternalServerError("Account Head Not Found")
				}
				var cashName string
				if cashData["name"] != nil {
					cashName = cashData["name"].(string)
				}
				autoDescription = fmt.Sprintf(
					"Amount received from %s and credited to %s",
					customerName,
					cashName,
				)

			} else {
				bankId := obj["bank_name"].(string)
				bankData, err := GetDataById(org.Id, bankId, "account_head")
				if err != nil {
					return shared.InternalServerError("Account Head Not Found")
				}
				var bankName string
				if bankData["name"] != nil {
					bankName = bankData["name"].(string)
				}
				autoDescription = fmt.Sprintf(
					"Amount received from %s and credited to %s",
					customerName,
					bankName,
				)

				filter = bson.M{"factory_id": factoryID, "bank_name": bankId}
			}
		} else if transactionType == "payable" {
			var from string
			var to string
			var customerName string
			if obj["from"] != nil {
				from = obj["from"].(string)
			}
			if obj["to"] != nil {
				to = obj["to"].(string)
			}

			custData, err := GetDataById(org.Id, to, "customer")
			if err != nil {
				return shared.InternalServerError("customer Not Found")
			}
			customerName = custData["customer_name"].(string)

			if from == "cash" {
				cashId := obj["cash_name"].(string)
				cashData, err := GetDataById(org.Id, cashId, "account_head")
				if err != nil {
					return shared.InternalServerError("Account Head Not Found")
				}
				var cashName string
				if cashData["name"] != nil {
					cashName = cashData["name"].(string)
				}
				autoDescription = "Amount paid to " + customerName + " from " + cashName

				filter = bson.M{"factory_id": factoryID, "cash_name": cashId}
			} else {
				bankId := obj["bank_name"].(string)
				bankData, err := GetDataById(org.Id, bankId, "account_head")
				if err != nil {
					return shared.InternalServerError("Account Head Not Found")
				}
				var bankName string
				if bankData["name"] != nil {
					bankName = bankData["name"].(string)
				}
				autoDescription = "Amount paid to " + customerName + " from " + bankName

				filter = bson.M{"factory_id": factoryID, "bank_name": bankId}
			}
		} else if transactionType == "exp" {
			var from string
			var accHead string
			var accHeadID string
			if obj["from"] != nil {
				from = obj["from"].(string)
			}
			if obj["account_head"] != nil {
				accHeadID = obj["account_head"].(string)
			}

			custData, err := GetDataById(org.Id, accHeadID, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			accHead = custData["name"].(string)

			if from == "cash" {
				cashId := obj["cash_name"].(string)
				filter = bson.M{"factory_id": factoryID, "cash_name": cashId}
				cashData, err := GetDataById(org.Id, cashId, "account_head")
				if err != nil {
					return shared.InternalServerError("Account Head Not Found")
				}
				var cashName string
				if cashData["name"] != nil {
					cashName = cashData["name"].(string)
				}
				autoDescription = "Amount paid towards " + accHead + " from " + cashName

			} else {
				bankId := obj["bank_name"].(string)
				filter = bson.M{"factory_id": factoryID, "bank_name": bankId}

				bankData, err := GetDataById(org.Id, bankId, "account_head")
				if err != nil {
					return shared.InternalServerError("Account Head Not Found")
				}
				var bankName string
				if bankData["name"] != nil {
					bankName = bankData["name"].(string)
				}
				autoDescription = "Amount paid towards " + accHead + " from " + bankName
			}
		} else if transactionType == "bank" {
			bankId := "ICICI-BANK"
			bankData, err := GetDataById(org.Id, bankId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var bankName string
			if bankData["name"] != nil {
				bankName = bankData["name"].(string)
			}
			cashId := "DCASH"
			cashData, err := GetDataById(org.Id, cashId, "account_head")
			if err != nil {
				return shared.InternalServerError("Account Head Not Found")
			}
			var cashName string
			if cashData["name"] != nil {
				cashName = cashData["name"].(string)
			}
			autoDescription = "Amount withdrawn from " + bankName + " and credited to " + cashName

			filter = bson.M{"factory_id": factoryID, "cash_name": cashId}
			obj["type"] = "bank"
			obj["bank_name"] = "ICICI-BANK"
			obj["cash_name"] = "DCASH"
			obj["to"] = "cash"
		}

		err := database.
			GetConnection(org.Id).
			Collection("voucher_details").
			FindOne(ctx, filter, opts).
			Decode(&lastLedgerEntry)

		openingBalance := 0.0
		if err == nil {
			openingBalance = helper.ToFloat64(lastLedgerEntry["closing_balance"])
		} else if err != mongo.ErrNoDocuments {
			openingBalance = 0
		}

		// ---------------- CALCULATE BALANCE ----------------
		var closingBalance float64
		switch transactionType {
		case "receivable", "bank":
			closingBalance = openingBalance + amount
		case "payable", "exp":
			closingBalance = openingBalance - amount
		default:
			return shared.BadRequest("invalid transaction type")
		}

		if closingBalance < 0 {
			return shared.BadRequest("Insufficient balance")
		}

		// ---------------- PREPARE VOUCHER (COPY MAP) ----------------
		inputVoucherDetails := make(map[string]interface{})
		for k, v := range obj {
			inputVoucherDetails[k] = v
		}

		inputVoucherDetails["_id"] = uuid.New().String()
		inputVoucherDetails["voucher_id"] = obj["_id"]
		inputVoucherDetails["opening_balance"] = openingBalance
		inputVoucherDetails["closing_balance"] = closingBalance
		inputVoucherDetails["available_balance"] = closingBalance
		inputVoucherDetails["auto_description"] = autoDescription
		// ---------------- INSERTS ----------------
		_, err = Insert(org.Id, "voucher_details", inputVoucherDetails)
		if err != nil {
			return shared.BadRequest(err.Error())
		}

		if _, err = Insert(org.Id, collectionName, obj); err != nil {
			return shared.BadRequest(err.Error())
		}
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message":  "Inserted successfully",
		"insertId": "",
	})
}

// func ProcessPettyCash(c *fiber.Ctx) error {
// 	org, exists := helper.GetOrg(c)
// 	if !exists {
// 		return shared.BadRequest("Organization Id missing")
// 	}

// 	userToken := utils.GetUserTokenValue(c)
// 	collectionName := c.Params("modelName")
// 	var inputData map[string]interface{}
// 	err := c.BodyParser(&inputData)
// 	if err != nil {
// 		return shared.BadRequest(err.Error())
// 	}
// 	helper.UpdateDateObject(inputData)

// 	if _, ok := inputData["status"]; !ok || inputData["status"] == "" {
// 		inputData["status"] = "Active"
// 	}

// 	inputData["created_on"] = time.Now()
// 	inputData["created_by"] = userToken.UserId
// 	inputData["_id"] = uuid.New().String()

// 	var lastLedgerEntry map[string]interface{}
// 	opts := options.FindOne().SetSort(bson.M{"created_on": -1})
// 	filter := bson.M{"factory_id": inputData["factory_id"].(string)}

// 	err2 := database.GetConnection(org.Id).Collection(collectionName).FindOne(ctx, filter, opts).Decode(&lastLedgerEntry)

// 	var openingBalance float64
// 	if err2 != nil {
// 		if err2 == mongo.ErrNoDocuments {
// 			openingBalance = 0
// 		} else {
// 			return err2
// 		}
// 	} else {
// 		openingBalance = helper.ToFloat64(lastLedgerEntry["closing_balance"])
// 	}

// 	amount := helper.ToFloat64(inputData["amount"])
// 	transactionType := inputData["transactionType"].(string)

// 	var closingBalance float64
// 	if transactionType == "Credit / Income" {
// 		closingBalance = openingBalance + amount
// 	} else if transactionType == "Debit / Expense" {
// 		closingBalance = openingBalance - amount
// 	}

// 	inputData["opening_balance"] = openingBalance
// 	inputData["closing_balance"] = closingBalance
// 	inputData["available_balance"] = closingBalance

// 	if closingBalance < 0 {
// 		return shared.BadRequest("Insufficient balance")
// 	}

// 	res, err := Insert(org.Id, collectionName, inputData)
// 	if err != nil {
// 		return shared.BadRequest(err.Error())
// 	}
// 	return shared.SuccessResponse(c, fiber.Map{
// 		"message":   "Insert Successfully",
// 		"insert ID": res.InsertedID,
// 	})
// }

func InsertUser(inputData map[string]interface{}) error {

	// firstName := inputData["first_name"].(string)
	// lastName := inputData["last_name"].(string)
	// mobileNo := inputData["mobile_number"].(string)
	// emailId := inputData["email_id"].(string)
	// Id := inputData["_id"].(string)
	// rand := helper.GenerateRandomString(8)
	// pwd, err := helper.GeneratePasswordHash(rand)

	// if err != nil {
	// 	return err
	// }

	// createUserData := map[string]interface{}{
	// 	"_id":           uuid.New().String(),
	// 	"name":          firstName + " " + lastName,
	// 	"mobile_number": mobileNo,
	// 	"pwd":           pwd,
	// 	"email":         emailId,
	// 	"role":          "A",
	// 	"status":        "Active",
	// 	"created_on":    time.Now(),
	// 	"org_id":        Id,
	// }

	Insert("shared", "temporary_user", inputData)
	// database.GetConnection("shared").Collection("temporary_user").DeleteOne(context.Background(), bson.M{"_id": emailId})
	// helper.SendOnBoardingMail("https://demo.kajupro.com/", rand, emailId)

	// Email removed from POST - will be sent only when organization is approved via PUT
	// helper.SendRegistrationMail(inputData["first_name"].(string), inputData["name"].(string), inputData["email_id"].(string))
	return nil
}
// InsertEmployeeAsUser
//
// PURPOSE:
// This function automatically creates a system user account when an employee is created,
// and links that employee to authentication/login system.
//
// FLOW:
// 1. Extract employee details (name, email, id, status, etc.)
// 2. Generate random temporary password
// 3. Hash the password securely
// 4. Create user record in "user" collection
// 5. Remove temporary registration entry from "temporary_user"
// 6. Fetch organization domain information
// 7. Construct login URL for onboarding
// 8. Send onboarding email with login credentials
//
// SIDE EFFECTS:
// - Creates user login account
// - Deletes temporary user record
// - Sends onboarding email to employee
//
// SECURITY:
// - Password is randomly generated and hashed
// - Sent only via secure onboarding email
//
// NOTE:
// This is part of employee onboarding automation flow.
func InsertEmployeeAsUser(inputData map[string]interface{}, orgId string) error {

	name := inputData["employee_name"].(string)
	emailId := inputData["email"].(string)
	Id := inputData["_id"].(string)
	rand := helper.GenerateRandomString(8)
	pwd, err := helper.GeneratePasswordHash(rand)

	if err != nil {
		return err
	}

	createUserData := map[string]interface{}{
		"_id":           uuid.New().String(),
		"name":          name,
		"pwd":           pwd,
		"email":         emailId,
		"role":          "OA",
		"status":        inputData["status"].(string),
		"factory_id":    inputData["factory"],
		"mobile_number": inputData["contact_mobile_number"],
		"unit_id":       inputData["unit"],
		"created_on":    time.Now(),
		"org_id":        Id,
		"profileImage":  inputData["profileImage"],
	}

	Insert(orgId, "user", createUserData)
	database.GetConnection(orgId).Collection("temporary_user").DeleteOne(context.Background(), bson.M{"_id": emailId})

	// Get organization data to retrieve domain name
	var orgData map[string]interface{}
	err = database.GetConnection("shared").Collection("organization").FindOne(context.Background(), bson.M{"_id": Id}).Decode(&orgData)
	domainName := "demo"
	if err == nil && orgData["domain_name"] != nil {
		if dn, ok := orgData["domain_name"].(string); ok && dn != "" {
			domainName = dn
		}
	}
	loginURL := "https://" + domainName + ".kajupro.com/"

	helper.SendOnBoardingMail(loginURL, rand, emailId)
	return nil
}
// UpdateUser
//
// PURPOSE:
// This function updates an existing user record in the "user" collection
// when employee details are modified.
//
// FLOW:
// 1. Extract user organization ID from input data
// 2. Build filter using org_id to locate existing user
// 3. Prepare updated user fields from employee data
// 4. Perform MongoDB update operation using $set
//
// UPDATED FIELDS:
// - name
// - role (fixed as OA)
// - status
// - factory_id
// - mobile_number
// - unit_id
// - profileImage
//
// NOTE:
// This function ensures employee updates are reflected in the system user login table.
//
// WARNING:
// If user not found or update fails, no retry mechanism is handled.
func UpdateUser(inputData map[string]interface{}, orgId string) error {
	userOrgId := inputData["_id"].(string)
	filter := bson.M{"org_id": userOrgId}
	updateUserData := map[string]interface{}{
		"name":          inputData["employee_name"].(string),
		"role":          "OA",
		"status":        inputData["status"].(string),
		"factory_id":    inputData["factory"],
		"mobile_number": inputData["contact_mobile_number"],
		"unit_id":       inputData["unit"],
		"profileImage":  inputData["profileImage"],
	}
	update := bson.M{"$set": updateUserData}
	_, err := database.GetConnection(orgId).Collection("user").UpdateOne(ctx, filter, update, updateOpts)
	if err != nil {
		return nil
	}
	return nil
}
// CheckUser
//
// PURPOSE:
// This function checks whether a user already exists in the system
// based on the given email ID.
//
// FLOW:
// 1. Query "user" collection using email filter
// 2. Use aggregation pipeline to search matching records
// 3. If no records found → user does NOT exist
// 4. If record exists → user already exists
//
// RETURN:
// - true  → User does NOT exist (safe to create new user)
// - false → User already exists in system
//
// NOTE:
// This function is typically used during employee/user creation
// to avoid duplicate user accounts.
func CheckUser(emailid string, orgid string) bool {
	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"email", emailid},
				},
			},
		},
	}

	inwarddomesticresponse, _ := helper.GetAggregateQueryResult(orgid, "user", pipeline)

	if len(inwarddomesticresponse) == 0 {
		return true
	}
	return false
}

// updateOrganization
//
// PURPOSE:
// This function creates a unique MongoDB database for a new organization
// and assigns the generated database name to the organization record.
//
// FLOW:
// 1. Extract organization name from input data
// 2. Generate database prefix using first 2 characters of name
// 3. Add randomness + timestamp for uniqueness
// 4. Construct final database name in format:
//      <PREFIX><RANDOM><TIMESTAMP>_cerp
// 5. Create new MongoDB database using generated name
// 6. Store generated database ID in organization record
//
// EXAMPLE DB NAME:
// ORXK8f3a1_cerp
//
// NOTE:
// - Ensures each organization gets a unique isolated database
// - Prevents name collision using random + timestamp combination
//
// SIDE EFFECT:
// - Creates a new MongoDB database dynamically
func updateOrganization(inputData map[string]interface{}) error {
	name := inputData["name"].(string)

	// Take first 2 characters of name (sanitized)
	dbPrefix := "ORG"
	if len(name) >= 2 {
		dbPrefix = strings.ToUpper(name[:2])
	}

	// Ensure uniqueness: random + timestamp combo
	randomString := helper.GenerateRandomString(2)               // e.g., XK
	timestamp := time.Now().UnixNano() / int64(time.Millisecond) // ms-level timestamp
	shortTime := fmt.Sprintf("%x", timestamp)[10:]               // take last few hex chars for brevity

	dbName := fmt.Sprintf("%s%s%s_cerp", dbPrefix, randomString, shortTime)

	id, err := database.CreateNewMongoDatabase(dbName, inputData["_id"].(string))
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	inputData["db_name"] = id

	return nil
}

func PostChecking(c *fiber.Ctx) error {
	date := time.Now().Format("2006-01-02")
	return shared.SuccessResponse(c, date)
}
// LotCreatingConfig
//
// PURPOSE:
// This function fetches configuration settings for lot creation
// based on organization and factory.
//
// It specifically retrieves the "lot_starting_process" value
// from the configuration collection.
//
// FLOW:
// 1. Build MongoDB aggregation pipeline to filter config by:
//      - org_id
//      - factory_id
// 2. Execute aggregation query on "config" collection
// 3. Check if configuration exists
// 4. Extract "lot_starting_process" field
// 5. Convert value to int32 and return it
//
// RETURN:
// - int32 → lot starting process configuration value
// - error → if query fails or no config found
//
// NOTE:
// - Used in production/lot creation workflow
// - Controls which process step lot should start from
func LotCreatingConfig(orgId string, factoryId string) (int32, error) {

	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"factory_id", factoryId},
					{"org_id", orgId},
				},
			},
		},
	}

	fmt.Println(factoryId, "factory Id", orgId)

	inwarddomesticresponse, err := helper.GetAggregateQueryResult(orgId, "config", pipeline)
	if err != nil {
		return 0, err
	}
	fmt.Println(inwarddomesticresponse)
	if len(inwarddomesticresponse) == 0 {
		return 0, err
	}

	lotDetails := inwarddomesticresponse[0]

	lotStartingProcess := helper.ToInt32(lotDetails["lot_starting_process"])

	return lotStartingProcess, err
}
// LotCreating
//
// PURPOSE:
// This function generates or updates a lot number for a production process.
// It manages lot creation, increment logic, and history tracking based on
// factory, purchase, and date conditions.
//
// FLOW:
// 1. Get factory and purchase details
// 2. Fetch purchase origin (country_origin)
// 3. Check if lot master exists for factory
// 4. If not exists → create new lot master + first lot entry
// 5. If exists → fetch current lot number
// 6. Check lot history for purchase & factory
// 7. Apply date-based logic:
//      - New date → increment lot and create new entry
//      - Same date → update existing lot weight only
// 8. Update lot_history or create new record accordingly
//
// LOGIC RULES:
// - Each factory maintains independent lot sequence
// - Lot increments only when date changes
// - Same-day entries only increase weight
// - Purchase-based tracking is used for lot history grouping
//
// COLLECTIONS USED:
// - lot → master lot counter per factory
// - lot_history → detailed tracking of lot movements
// - purchase → origin data for lot metadata
//
// RETURN:
// - int32 → current lot number
// - error → failure in creation or update process
//
// NOTE:
// This function ensures strict control over production lot numbering
// and prevents duplicate or inconsistent lot generation.
func LotCreating(orgId string, userId string, inputData map[string]interface{}) (int32, error) {

	// prefix := "LOT"
	factoryId := inputData["factory_id"].(string)
	// UpperFactoryId := strings.ToUpper(factoryId)
	uniqueId := factoryId

	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", uniqueId}}}},
	}

	purchaseId := inputData["purchase_id"].(string)
	purchasePipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", purchaseId}}}},
	}

	purchaseResponse, err := helper.GetAggregateQueryResult(orgId, "purchase", purchasePipeline)
	if err != nil {
		return 0, err
	}

	purchaseData := purchaseResponse[0]

	originId := purchaseData["country_origin"].(string)
	parentId := inputData["_id"].(string)

	inwarddomesticresponse, _ := helper.GetAggregateQueryResult(orgId, "lot", pipeline)
	errs, inputTime := helper.StringToDateConverter(inputData["process_start_date_time"])
	if errs {
		return 0, fmt.Errorf("Cannot convert input date time")
	}

	inputWeight := helper.ToFloat64(inputData["input_weight"])
	var lotNo int32
	if len(inwarddomesticresponse) == 0 {
		lotNo, err = createLotMaster(factoryId, orgId)
		if err != nil {
			return 0, fmt.Errorf("Error In Creating Lot")
		}
		err := NewLotCreate(factoryId, orgId, lotNo, uniqueId, userId, purchaseId, originId, parentId, inputTime, inputWeight)
		if err != nil {
			return 0, err
		}
		return lotNo, nil
	} else {
		purchaseData := inwarddomesticresponse[0]
		lotNo = helper.ToInt32(purchaseData["lot"])
	}

	lotDetailsPipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"factory_id", factoryId},
					{"purchase_id", purchaseId},
				},
			},
		},
	}

	lotDeatilResponse, err := helper.GetAggregateQueryResult(orgId, "lot_history", lotDetailsPipeline)
	if err != nil {
		return 0, err
	}

	if len(lotDeatilResponse) == 0 {
		err := NewLotCreate(factoryId, orgId, lotNo+1, uniqueId, userId, purchaseId, originId, parentId, inputTime, inputWeight)
		if err != nil {
			return 0, err
		}

		return lotNo + 1, nil
	}

	data := lotDeatilResponse[0]
	getValue := lotDeatilResponse[0]["value"]
	value := helper.ToInt32(getValue)
	latestDate := data["latest_date"].(string)

	currentDate := inputTime.Format("2006-01-02")
	if latestDate != currentDate {
		err := updateLot(orgId, factoryId, uniqueId, value+1, userId, purchaseId, originId, parentId, inputTime, inputWeight)
		if err != nil {
			return 0, err
		}
		return value + 1, nil
	} else if latestDate == currentDate {
		// Define the update operation using $push
		update := map[string]interface{}{
			"$inc": bson.M{
				"weight": inputWeight,
			},
		}

		// Define the filter to match the document by _id and factory_id
		filter := map[string]interface{}{
			"factory_id":  factoryId,
			"purchase_id": purchaseId,
		}

		_, err := database.GetConnection(orgId).Collection("lot_history").UpdateOne(ctx, filter, update, updateOpts)
		if err != nil {
			return value, nil
		}
	}
	return value, nil
}
// NewLotCreate
//
// PURPOSE:
// This function creates a new lot entry and stores lot history,
// while also updating the latest lot number in the master "lot" collection.
//
// FLOW:
// 1. Build lot history document with metadata (factory, user, weight, purchase info)
// 2. Add initial lot tracking entry inside "lot_dates" array
// 3. Insert the record into "lot_history" collection
// 4. Update the master "lot" collection with latest lot number
//
// DATA STORED:
// - Lot value and starting lot number
// - Purchase, origin, and parent references
// - Weight and creation metadata
// - Historical tracking of lot changes
//
// SIDE EFFECTS:
// - Inserts record into lot_history
// - Updates lot master collection with latest lot number
//
// NOTE:
// This function maintains traceability of lot creation and lifecycle history.
func NewLotCreate(factoryId string, orgId string, lotNo int32, uniqueId string, userId string, purchaseId string, originId string, parentId string, inputTime time.Time, weight float64) error {

	inputData := map[string]interface{}{
		"_id":          uuid.New().String(),
		"value":        lotNo,
		"factory_id":   factoryId,
		"created_on":   time.Now(),
		"created_by":   userId,
		"latest_date":  inputTime.Format("2006-01-02"),
		"weight":       weight,
		"purchase_id":  purchaseId,
		"starting_lot": lotNo,
		"lot_dates": []map[string]interface{}{
			{
				"purchase_id": purchaseId,
				"origin_id":   originId,
				"parent_id":   parentId,
				"value":       lotNo,
				"weight":      weight,
				"date":        inputTime.Format("2006-01-02"),
				"created_on":  time.Now(),
				"created_by":  userId,
			},
		},
	}

	_, err := Insert(orgId, "lot_history", inputData)
	if err != nil {
		return err
	}
	lotMasterFilter := map[string]interface{}{
		"_id": factoryId,
	}
	lotMasterUpdate := map[string]interface{}{

		"$set": bson.M{
			"lot": lotNo,
		},
	}

	_, err = database.GetConnection(orgId).Collection("lot").UpdateOne(ctx, lotMasterFilter, lotMasterUpdate, updateOpts)
	if err != nil {
		return err
	}

	return nil

}

// createLotMaster
//
// PURPOSE:
// This function initializes a new Lot Master record for a factory
// when no existing lot entry is found.
//
// FLOW:
// 1. Create initial lot master document
// 2. Set starting lot value as 1
// 3. Store factory-wise lot tracking record in "lot" collection
// 4. Insert the record into database
//
// INITIAL DATA:
// - _id → factoryId (acts as primary key)
// - lot → starting value (1)
// - created_on → timestamp
//
// RETURN:
// - int32 → initial lot number (1)
// - error → if insert fails
//
// NOTE:
// This function is used only for first-time lot initialization per factory.
func createLotMaster(factoryId string, orgId string) (int32, error) {
	lotMaster := map[string]interface{}{
		"_id":        factoryId,
		"lot":        1,
		"created_on": time.Now(),
	}

	_, err := Insert(orgId, "lot", lotMaster)
	if err != nil {
		return 0, err
	}

	return 1, nil
}

// updateLot
//
// PURPOSE:
// This function updates an existing lot record by appending new lot activity
// and updating current lot values in both "lot_history" and "lot" collections.
//
// FLOW:
// 1. Create a new lot entry object (lot_dates)
// 2. Append the new entry into existing lot history using $push
// 3. Update current lot value and latest date using $set
// 4. Increment total weight using $inc
// 5. Update corresponding lot master record with latest lot value
//
// DB UPDATES:
// - lot_history:
//      • Append new lot event (lot_dates)
//      • Update value + latest_date
//      • Increment weight
//
// - lot:
//      • Update current lot number (master record)
//
// FILTER:
// - factory_id + purchase_id identifies the lot record
//
// NOTE:
// This function maintains both historical tracking and real-time lot state.
func updateLot(orgId string, factoryId string, uniqueId string, value int32, userId string, purchaseId string, originId string, parentId string, inputTime time.Time, weight float64) error {

	// The data you want to push into `lot_dates`
	lotDateEntry := map[string]interface{}{
		"purchase_id": purchaseId,
		"origin_id":   originId,
		"parent_id":   parentId,
		"value":       value,
		"weight":      weight,
		"date":        inputTime.Format("2006-01-02"),
		"created_on":  time.Now(),
		"created_by":  userId,
	}

	// Define the update operation using $push
	update := map[string]interface{}{
		"$push": map[string]interface{}{
			"lot_dates": lotDateEntry,
		},
		"$set": bson.M{
			"value":       value,
			"latest_date": inputTime.Format("2006-01-02"),
		},
		"$inc": bson.M{
			"weight": weight,
		},
	}

	// Define the filter to match the document by _id and factory_id
	filter := map[string]interface{}{
		"factory_id":  factoryId,
		"purchase_id": purchaseId,
	}

	_, err := database.GetConnection(orgId).Collection("lot_history").UpdateOne(ctx, filter, update, updateOpts)
	if err != nil {
		return err
	}

	lotMasterFilter := map[string]interface{}{
		"_id": factoryId,
	}
	lotMasterUpdate := map[string]interface{}{

		"$set": bson.M{
			"lot": value,
		},
	}

	_, err = database.GetConnection(orgId).Collection("lot").UpdateOne(ctx, lotMasterFilter, lotMasterUpdate, updateOpts)
	if err != nil {
		return err
	}

	return nil
}
// putDocByIDHandlers handles generic update operations for all collections.
//
// This is a central update controller that processes incoming request data,
// applies business rules based on collection type, and performs MongoDB updates.
//
// Major responsibilities:
// 1. Parse and sanitize incoming request body
// 2. Apply collection-specific transformations and validations
// 3. Handle business workflows (production, purchase, jobwork, employee, etc.)
// 4. Trigger side effects like:
//    - Stock updates (production, packing, sales)
//    - Ledger updates (purchase, invoice, jobwork)
//    - User creation/updation (employee sync)
//    - Production automation (cooling, lot creation)
// 5. Maintain audit/history logs for screens and templates
// 6. Handle special flows like:
//    - PACK process serial generation
//    - Production template change cleanup
//    - Jobwork inward/outward processing
//    - Organization onboarding initialization
//
// Finally:
// - Updates the main document in MongoDB
// - Triggers post-update hooks (stock, ledger, async processes)
// - Returns success response
//
// NOTE: This function acts as a "business rules engine" for multiple modules
// and should ideally be split into domain-specific services for maintainability.
func putDocByIDHandlers(c *fiber.Ctx) error {

	//Get the orgId from Header

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// to  Get the User Details from Token
	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("model_name")

	var inputData map[string]interface{}

	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err)
	}
	if collectionName == "process_product" {
		if inputData["product_id"] != nil && inputData["expression"] != nil {
			productId := fmt.Sprintf("%v", inputData["product_id"])
			if productId != "" {
				inputData[productId] = inputData["expression"]
			}
		}
	}
	// UpdateData := helper.UpdateFieldsWithParentKey(inputData, "", updatedDatas)
	helper.UpdateDateObject(inputData)
	delete(inputData, "created_on")
	delete(inputData, "created_by")

	if collectionName == "purchase" {
		purchaseData, _ := GetDataById(org.Id, c.Params("id"), "purchase")
		if purchaseData != nil {
			purchaseType, _ := purchaseData["purchasetype"].(string)
			if purchaseType == "International" {

				newStockMovements, ok := inputData["stock_movement"].([]interface{})

				if ok && len(newStockMovements) > 0 {

					for i, mv := range newStockMovements {
						movement, isMap := mv.(map[string]interface{})
						if !isMap {
							return errors.New("purchase: invalid stock_movement element format")
						}

						_, exists := movement["created_on"]

						// If missing or <= 0 → set to current time
						if !exists {
							movement["created_on"] = time.Now()
							// update back to array
							newStockMovements[i] = movement
						}

					}

					// Write updated movements back to inputData
					inputData["stock_movement"] = newStockMovements
				}
			}

		}
	}
	created_on := time.Now()
	update := bson.M{
		"$set": inputData,
		"$setOnInsert": bson.M{
			"created_on": created_on,
			"created_by": userToken.UserId,
		},
	}
	inputData["update_on"] = time.Now()
	inputData["update_by"] = userToken.UserId

	if collectionName == "lot_history" {
		inputData["did"] = "web"
		if _, ok := inputData["activity_end_date_time"]; !ok {
			inputData["activity_end_date_time"] = time.Now()
			inputData["did"] = "app"
		}

		if _, k := inputData["activity_done_by"]; !k {
			inputData["activity_done_by"] = userToken.UserId
		}
	}

	if collectionName == "productions" {

		if inputData["status"] != nil {

			if inputData["process_type"] == "BOR" {
				if inputData["status"] == "Pause" {
					inputData["cycle_one_process_end_date_time"] = time.Now()
				} else if inputData["status"] == "Resume" {
					inputData["cycle_two_process_start_date_time"] = time.Now()
				}
			}

		}

		productionId := c.Params("id")

		// Background validation for stock availability
		skipValidate := false
		if check, ok := inputData["other_worker_salary"].(bool); ok {
			skipValidate = check
		}
		// For COOL process completion, skip validation but still do the update
		skipCoolValidation := false
		if inputData["process_type"] == "COOL" && inputData["status"] == "Completed" {
			skipCoolValidation = true
		}
		if inputData["process_type"] == "PACK" {
			if inputData["status"] == "Completed" {
				skipValidate = true
			}
		}

		if inputData["process_type"] != "PACK" && !skipValidate {
			// Template change is now handled inside PutProductionStock
			// Skip validation for COOL completion (values may exceed estimates)
			if !skipCoolValidation {
				if err := ValidateProductionStockUpdate(org.Id, inputData, productionId); err != nil {
					return shared.BadRequest(fmt.Sprintf("Stock validation failed: %v", err))
				}
			}
			// Update stock - handles template change detection internally
			if err := PutProductionStock(org.Id, productionId, userToken.UserId, inputData); err != nil {
				return shared.BadRequest(fmt.Sprintf("Stock update failed: %v", err))
			}
		} else if inputData["process_type"] == "PACK" && !skipValidate {
			// Handle PACK process updates
			productionId := c.Params("id")
			startSerialNo := helper.InterfaceToInt64(inputData["start_serial_no"])
			endSerialNo := helper.InterfaceToInt64(inputData["end_serial_no"])
			packingTypeData, _ := GetDataById(org.Id, inputData["type_of_packing"].(string), "lookup")
			packingValue := helper.ToFloat64(packingTypeData["value"])

			// Update kernel inventory for the serial number range
			for i := startSerialNo; i <= endSerialNo; i++ {
				facId := inputData["factory_id"].(string)
				facPrefix := strings.ToUpper(facId[:3])
				seqData := "kernel-pack-" + facId
				seq, _ := helper.GetNextSeqNumber(seqData, org.Id)
				pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)
				serialData := map[string]interface{}{
					"_id":             pac,
					"s_no":            i,
					"status":          "packed",
					"production_id":   productionId,
					"purchase_id":     inputData["purchase_id"],
					"stock_from":      "production",
					"created_on":      time.Now(),
					"created_by":      userToken.UserId,
					"quantity":        packingValue,
					"product_id":      inputData["product_id"],
					"type_of_packing": inputData["type_of_packing"],
				}
				Insert(org.Id, "kernel_inventory", serialData)
			}

			// Validate PACK stock (checks GRAD availability)
			if err := ValidatePackProductionStockUpdate(org.Id, inputData, productionId); err != nil {
				return shared.BadRequest(fmt.Sprintf("Stock validation failed: %v", err))
			}
			// Update stock after validation passes (updates GRAD and KERNEL)
			if err := UpdatePackProductionStockByRefId(org.Id, inputData, productionId, userToken.UserId); err != nil {
				return shared.BadRequest(fmt.Sprintf("Stock update failed: %v", err))
			}
		}
	} else if collectionName == "organization" || collectionName == "user_type" || collectionName == "master_menu" || collectionName == "role_acl" {
		org.Id = "shared"
	}
	if collectionName == "jobwork_details" {
		jobworkID := fmt.Sprintf("%v", inputData["jobwork_id"])
		purchaseID := fmt.Sprintf("%v", inputData["purchase_id"])
		templateID := helper.ToString(inputData["template_id"])
		jobwork, err := GetDataById(org.Id, jobworkID, "job_work")
		processID := fmt.Sprintf("%v", inputData["input_from"])

		if purchaseID == "" || purchaseID == "<nil>" {
			//ftech purchasde id from outward
			outward_id := inputData["outward_jobwork_id"].(string)
			outwardJobwork, err := GetDataById(org.Id, outward_id, "jobwork_details")
			if err != nil {
				log.Println("outward jobwork data not found:", err)
				outwardJobwork = map[string]interface{}{}
			}
			purchaseID = outwardJobwork["purchase_id"].(string)

		}
		if err != nil {
			log.Println("jobwork data not found:", err)
			jobwork = map[string]interface{}{}
		}
		purchase, err := GetDataById(org.Id, purchaseID, "purchase")
		if err != nil {
			log.Println("purchase data not found:", err)
			purchase = map[string]interface{}{}
		}
		log.Println("job work triggered", inputData)

		productID := ""

		if pid, ok := inputData["input_product_types"].(string); ok {
			productID = pid
		}
		jobworkTemplate, err := GetDataById(org.Id, templateID, "jobwork_template")

		if productID == "" {

			// filter := helper.DocIdFilter(c.Params("id"))
			// oldResults, _ := helper.GetQueryResult(org.Id, "jobwork_details", filter, 0, 1, nil)

			// new_created_on := created_on
			// if len(oldResults) > 0 && oldResults[0]["created_on"] != nil {
			// 	new_created_on = helper.ParseDate(oldResults[0]["created_on"])
			// }
			id := c.Params("id")

			jobworkParent, err := GetDataById(org.Id, inputData["parent_jobwork_id"].(string), "job_work")
			if err != nil {
				log.Println("jobwork data not found:", err)
				jobworkParent = map[string]interface{}{}
			}

			process, err := GetDataById(
				org.Id,
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
			// inputData["created_on"] = new_created_on
			inputData["job_id"] = id
			inputData["customer_id"] = jobwork["service_provider"]
			ProcessInwardJobwork(org.Id, jobworkTemplate, inputData, userToken.UserId, processType)
		} else {

			//if id already exists
			// filter := helper.DocIdFilter(c.Params("id"))
			// oldResults, _ := helper.GetQueryResult(org.Id, "jobwork_details", filter, 0, 1, nil)

			// new_created_on := created_on
			// if len(oldResults) > 0 && oldResults[0]["created_on"] != nil {
			// 	new_created_on = helper.ParseDate(oldResults[0]["created_on"])
			// }
			ledgerUpdateFilter := LedgerFilter{
				ProductID:   productID,
				PurchaseID:  purchaseID,
				Origin:      purchase["country_origin"].(string),
				RefId:       purchase["_id"].(string),
				WarehouseID: jobwork["warehouse_id"].(string),
			}

			exists, err := ledgerEntryExists(org.Id, ledgerUpdateFilter)
			if exists {
				go UpdateLedgerEntryByIDs(org.Id, ledgerUpdateFilter, inputData["weight"].(float64), userToken.UserId, productID)
			} else {

				if err != nil {
					log.Println("jobwork data not found:", err)
					jobworkTemplate = map[string]interface{}{}
				}

				actionType := fmt.Sprintf("%v", jobwork["type"]) // Safe type fetch

				if actionType == "outWard-jobWork" {
					fmt.Println("TRUE — matched")
				} else {
					fmt.Println("FALSE — did not match")
				}
				if processID == "" {
					processID = "rcn"
				}
				var finalData map[string]interface{}

				if actionType == "outWard-jobWork" {
					productID, _ := inputData["input_product_types"].(string)

					finalData = map[string]interface{}{
						"jobwork_type": jobwork["type"],

						"quantity":       inputData["weight"],
						"purchase_id":    purchaseID,
						"job_id":         c.Params("id"),
						"factory_id":     jobwork["infactory"],
						"customer_id":    jobwork["service_provider"],
						"warehouse_id":   jobwork["warehouse_id"],
						"country_origin": purchase["country_origin"],
						"process_id":     processID,
						// "created_on":     new_created_on,
						"product_id": productID,
					}

					ProcessJobwork(org.Id, finalData, userToken.UserId)
				}
			}

			if err != nil {
				return shared.InternalServerError(err.Error())
			}

		}
	}

	var oldData []bson.M
	var isPostData bool
	if collectionName == "maintance_details" {
		filter := helper.DocIdFilter(c.Params("id"))

		oldData, _ = helper.GetQueryResult(org.Id, collectionName, filter, 0, 1, nil)

		if len(oldData) == 0 {
			isPostData = true
		}
	}
	var oldScreenJSon string
	var newScreenJSon string
	var reportId bool

	if collectionName == "config" {
		id := fmt.Sprintf("%v", inputData["factory_id"])

		factoryconfig[id] = inputData["sale_einvoice"]

	}

	if collectionName == "screen" {
		if inputData["report_id"] != nil {
			reportId = true
		}
		if !reportId {

			filter := helper.DocIdFilter(c.Params("id"))

			results, err := helper.GetQueryResult(org.Id, collectionName, filter, 0, 1, nil)
			if err != nil {
				return err
			}

			if len(results) > 0 {
				if config, ok := results[0]["config"]; ok && config != nil {
					oldScreenJSon, _ = config.(string)
				}
			}

			newScreenJSon, _ = inputData["config"].(string)

		}

	}

	if collectionName == "templatetype" {
		filter := helper.DocIdFilter(c.Params("id"))

		screenData, _ := helper.GetQueryResult(org.Id, collectionName, filter, 0, 1, nil)

		if len(screenData) == 1 {
			if screenData[0]["mobile_template_config"] != nil {

				oldScreenJSon = screenData[0]["mobile_template_config"].(string)
			}
			newScreenJSon = inputData["mobile_template_config"].(string)
		}

	}
	var purchaseOldData map[string]interface{}
	var invoiceOldData map[string]interface{}
	var kernelOldData map[string]interface{}
	if collectionName == "purchase" {
		purchaseOldData, _ = GetDataById(org.Id, c.Params("id"), collectionName)

	}

	if collectionName == "invoice_details" {
		invoiceOldData, err = GetDataById(org.Id, c.Params("id"), collectionName)
		if err != nil {
			invoiceOldData = nil
		}
	}

	if collectionName == "purchase_products_info" {
		kernelOldData, err = GetDataById(org.Id, c.Params("id"), collectionName)
		if err != nil {
			kernelOldData = nil
		}
	}
	var soldProductOldData map[string]interface{}
	if collectionName == "sold_products_info" {
		soldProductOldData, err = GetDataById(org.Id, c.Params("id"), collectionName)
		if err != nil {
			soldProductOldData = nil
		}
	}

	// Handle template changes for productions - need to remove old product fields
	if collectionName == "productions" {
		productionID := c.Params("id")
		oldProductionData, err := GetDataById(org.Id, productionID, "productions")
		if err != nil {
			return shared.BadRequest(fmt.Sprintf("Failed to get existing production data: %v", err))
		}

		// Check if template changed
		oldTemplateId := getString(oldProductionData["template_id"])
		newTemplateId := getString(inputData["template_id"])

		if oldTemplateId != "" && newTemplateId != "" && oldTemplateId != newTemplateId {
			// Template changed - need to remove old product fields
			log.Printf("Template changed from %s to %s - removing old product fields", oldTemplateId, newTemplateId)

			// Get new template products to know which fields to keep
			newTemplatePipeline := bson.A{
				bson.D{{"$match", bson.D{{"template_id", newTemplateId}}}},
			}
			newTemplateProducts, _ := helper.GetAggregateQueryResult(org.Id, "process_product", newTemplatePipeline)
			newProductIds := make(map[string]bool)
			for _, obj := range newTemplateProducts {
				productId := getString(obj["product_id"])
				if productId != "" {
					newProductIds[productId] = true
				}
				parentId := getString(obj["parent_id"])
				if parentId != "" {
					newProductIds[parentId] = true
				}
			}

			// Get old template products
			oldTemplatePipeline := bson.A{
				bson.D{{"$match", bson.D{{"template_id", oldTemplateId}}}},
			}
			oldTemplateProducts, err := helper.GetAggregateQueryResult(org.Id, "process_product", oldTemplatePipeline)
			if err == nil && len(oldTemplateProducts) > 0 {
				// Build $unset for old product fields that are NOT in new template
				unsetFields := bson.M{}
				for _, obj := range oldTemplateProducts {
					productId := getString(obj["product_id"])
					if productId != "" && !newProductIds[productId] {
						unsetFields[productId] = ""
					}
					// Also check for parent_id fields
					parentId := getString(obj["parent_id"])
					if parentId != "" && parentId != productId && !newProductIds[parentId] {
						unsetFields[parentId] = ""
					}
				}

				// Add $unset to the update if we have fields to remove
				if len(unsetFields) > 0 {
					update["$unset"] = unsetFields
					log.Printf("Removing old product fields: %v", unsetFields)
				}
			}
		}
	}

	// Validate consignment_status BEFORE updating
	if collectionName == "consignment_status" {
		if status, ok := inputData["status"].(string); ok && status == "In progress" {
			warehouseID, _ := inputData["warehouse_id"].(string)
			if warehouseID != "" {
				docID := c.Params("id")
				
				// Get current document status
				var currentDoc map[string]interface{}
				err := database.GetConnection(org.Id).Collection("consignment_status").FindOne(context.Background(), bson.M{"_id": docID}).Decode(&currentDoc)
				if err != nil {
					return shared.BadRequest("Document not found")
				}
				
				// If current status is already "In progress", allow update
				currentStatus, _ := currentDoc["status"].(string)
				if currentStatus == "In progress" {
					// Already in progress, allow update
				} else {
					// Trying to change TO "In progress", check if another one exists
					var existingConsignment map[string]interface{}
					filter := bson.M{
						"warehouse_id": warehouseID,
						"status":       "In progress",
					}
					err := database.GetConnection(org.Id).Collection("consignment_status").FindOne(context.Background(), filter).Decode(&existingConsignment)
					if err == nil && existingConsignment != nil {
						// Another document is already "In progress"
						return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
							"success": false,
							"message": "Already one consignment is in progress. Please close it before starting another.",
						})
					}
				}
			}
		}
	}

	res, err := database.GetConnection(org.Id).Collection(collectionName).UpdateOne(ctx, helper.DocIdFilter(c.Params("id")), update, updateOpts)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// Hook: If organization is approved, trigger infrastructure setup and user synchronization
	if collectionName == "organization" && inputData["domain_status"] == "Approved" {
		orgID := c.Params("id")
		fmt.Printf("[Onboarding Hook] Organization %s approved. Processing infrastructure and users...\n", orgID)

		// 1. Fetch onboarding config from common_config
		commonDB := database.SharedDB.Client().Database("common_config")
		var storedConfig struct {
			Payload onboarding.OnboardingRequest `bson:"payload"`
		}
		// Flexible lookup: Try ID first, then Name
		err := commonDB.Collection("onboarding_configs").FindOne(ctx, bson.M{"_id": orgID}).Decode(&storedConfig)
		if err != nil {
			fmt.Printf("[Onboarding Hook] Config not found by ID %s. Trying lookup by org name...\n", orgID)
			// Fetch current name for lookup
			var currentOrg map[string]interface{}
			database.SharedDB.Collection("organization").FindOne(ctx, bson.M{"_id": orgID}).Decode(&currentOrg)
			if name, ok := currentOrg["name"].(string); ok {
				err = commonDB.Collection("onboarding_configs").FindOne(ctx, bson.M{"org_name": name}).Decode(&storedConfig)
			}
		}

		if err == nil {
			fmt.Printf("[Onboarding Hook] Found stored config for %s. Initializing infrastructure...\n", orgID)
			// InitializeInfrastructure internal DB creation is now synchronous.
			// This ensures GetConnection will work correctly for the next steps.
			_, err = onboarding.InitializeInfrastructure(orgID, storedConfig.Payload)
			if err != nil {
				fmt.Printf("[Onboarding Hook Error] Infrastructure initialization failed: %v\n", err)
				return shared.InternalServerError("Failed to initialize organization infrastructure: " + err.Error())
			}
		} else {
			fmt.Printf("[Onboarding Hook] Info: No stored onboarding config found for %s (Lookup failed). Skipping auto-activation.\n", orgID)
		}

		// 2. Call InsertOrgUser - MUST happen after InitializeInfrastructure to ensure the new DB is accessible
		fmt.Printf("[Onboarding Hook] Calling InsertOrgUser for org: %s\n", orgID)
		err = InsertOrgUser(inputData, orgID)
		if err != nil {
			fmt.Printf("[Onboarding Hook Error] Failed to sync user/email: %v\n", err)
			return shared.InternalServerError(err.Error())
		}
		fmt.Printf("[Onboarding Hook Success] User synced and email triggered for org: %s\n", orgID)
	}
	if collectionName == "productions" {
		if inputData["process_type"] == "BORM" && inputData["status"] == "Completed" {
			prevousbatchId := c.Params("id")
			pipeline := bson.A{
				bson.D{
					{"$match",
						bson.D{
							{"process_type", "COOL"},
							{"prevous_batch_id", prevousbatchId},
						},
					},
				},
			}
			result, err := helper.GetAggregateQueryResult(org.Id, "productions", pipeline)
			if err != nil {
				log.Println("Error fetching previous batch data:", err)
				return shared.BadRequest("No previous batch found for cooling")
			}
			if len(result) != 0 {
				previousData := result[0]
				id := previousData["_id"].(string)
				if id != "" && previousData["status"].(string) != "Completed" {
					if err := autoUpdateForCooling(org.Id, userToken.UserId, inputData, previousData, prevousbatchId, id); err != nil {
						log.Println("Error updateing cooling data:", err)
					}
				}

			} else {
				if err := autoEntryForCooling(org.Id, userToken.UserId, inputData, prevousbatchId); err != nil {
					log.Println("Error inserting cooling data:", err)
				}
			}

		}
	}
	// if collectionName == "sold_products_info" {
	// 	updatePacking(inputData["batch_no"].([]interface{}), org.Id, userToken.UserId, c.Params("id"))
	// }

	if collectionName == "maintance_details" {
		helper.GenerateMultipleMaintenanceData(inputData, org.Id, c.Params("id"), oldData, isPostData)
	}

	// if collectionName == "purchase" {
	// 	inputData["ref_id"] = c.Params("id")
	// 	ProcessPurchase(org.Id, inputData, userToken.UserId)
	// }

	if collectionName == "employee" {
		if inputData["email"] != nil {

			userExists := CheckUser(inputData["email"].(string), org.Id)
			if userExists {
				InsertEmployeeAsUser(inputData, org.Id)
			} else {
				UpdateUser(inputData, org.Id)
			}
		}
	}

	if collectionName == "screen" || collectionName == "templatetype" {
		ip := c.IP() // Get client IP from Fiber context
		currentTime := time.Now().UTC()
		screenID := c.Params("id")

		// Build the audit record
		screenUpdateJson := bson.M{
			"_id":                 uuid.New().String(),
			"screen_id":           screenID,
			"updated_by_ip":       ip,
			"updated_at":          currentTime,
			"updated_screen_name": screenID,
			"org_id":              org.Id,
			"user_id":             userToken.UserId,
			"old_json":            oldScreenJSon,
			"new_json":            newScreenJSon,
		}
		if !reportId {

			// Insert into history collection
			database.
				GetConnection(org.Id).
				Collection("Update_Screen_History").
				InsertOne(ctx, screenUpdateJson)
		}

	}
	if collectionName == "purchase" {
		//international purchase
		purchaseId := c.Params("id")
		if purchaseId != "" {
			purchaseData, err := GetDataById(org.Id, purchaseId, "purchase")
			if err == nil {
				if purchaseData["purchasetype"] != "high-seas" {
					PurchaseLedgerUpdate(purchaseOldData, "International", org, userToken.UserId, c.Params("id"), collectionName)
				}
			}
		}
	}
	if collectionName == "invoice_details" {
		//domestic purchase
		//fetch purchase type from purchase
		// if inputData["purchase_id"] != nil {
		// 	purchaseData, err := GetDataById(org.Id, inputData["purchase_id"].(string), "purchase")
		// 	if err == nil {
		// 		if purchaseData["purchasetype"] != "high-seas" {
		PurchaseLedgerUpdate(invoiceOldData, "domestic", org, userToken.UserId, c.Params("id"), collectionName)
		// 		}
		// 	}
		// }
	}

	// if collectionName == "purchase_products_info" {
	// 	//kernel puerchase
	// 	PurchaseLedgerUpdate(kernelOldData, "kernel", org, userToken.UserId, c.Params("id"), collectionName)
	// }
	if collectionName == "purchase_products_info" {
		//kernel puerchase
		PurchaseLedgerUpdate(kernelOldData, "kernel", org, userToken.UserId, c.Params("id"), collectionName)
	}

	if collectionName == "sold_products_info" {
		if inputData["type"] == "loose" {
			ProcessKernelLooseSale(org.Id, inputData, userToken.UserId)
			return nil
		}
		if inputData["tin_grid_data"] == nil {
			return nil
		}
		if len(inputData["tin_grid_data"].([]interface{})) == 0 {
			return shared.InternalServerError(fmt.Sprintf("Failed to update sold product info: %v", err))
		}
		docID := c.Params("id")

		if soldProductOldData != nil {
			if oldGridData, ok := soldProductOldData["tin_grid_data"].(primitive.A); ok {
				var allSerialNumbers []int
				for _, item := range oldGridData {
					if gridItem, ok := item.(map[string]interface{}); ok {
						if serialNoStr, ok := gridItem["serial_no"].(string); ok && serialNoStr != "" {
							serials, _ := helper.FormatSerialRange(serialNoStr)
							allSerialNumbers = append(allSerialNumbers, serials...)
						}
					}
				}

				if len(allSerialNumbers) > 0 {
					// Revert status to "packed"
					updateFilter := bson.M{
						"s_no":         bson.M{"$in": allSerialNumbers},
						"product_id":   soldProductOldData["product_id"],
						"warehouse_id": soldProductOldData["warehouse_id"],
						"origin_id":    soldProductOldData["origin_id"],
						"purchase_id":  soldProductOldData["purchase_id"],
					}
					update := bson.M{
						"$set": bson.M{
							"status":     "packed",
							"updated_on": time.Now(),
							"updated_by": userToken.UserId,
						},
					}
					database.GetConnection(org.Id).Collection("kernel_inventory").UpdateMany(context.Background(), updateFilter, update)
				}
			}

			// Delete old stock ledger entries
			if err := DeleteLedgerEntryByRefID(org.Id, docID, userToken.UserId); err != nil {
				log.Printf("ERROR deleting old ledger entries for sold_products_info %s: %v", docID, err)
			}
		}

		// 2. Apply New State
		successIndices, err := UpdateKernelInventorySerailNumberWithIndices(inputData, org.Id, userToken.UserId)
		if err != nil {
			log.Printf("ERROR updating serial numbers for sold_products_info %s: %v", docID, err)
			return shared.InternalServerError(fmt.Sprintf("Failed to update serial numbers: %v", err))
		}

		inputData["_id"] = docID

		if len(successIndices) > 0 {
			originalGrid := inputData["tin_grid_data"].([]interface{})
			var filteredGrid []interface{}
			for _, idx := range successIndices {
				if idx >= 0 && idx < len(originalGrid) {
					filteredGrid = append(filteredGrid, originalGrid[idx])
				}
			}
			filteredInputData := make(map[string]interface{})
			for k, v := range inputData {
				filteredInputData[k] = v
			}
			filteredInputData["tin_grid_data"] = filteredGrid

			if err := KernalAndOtherSaleUpdate(filteredInputData, org.Id, userToken.UserId); err != nil {
				log.Printf("ERROR creating ledger entry for sold_products_info %s: %v", docID, err)
				return shared.InternalServerError(fmt.Sprintf("Failed to create stock ledger: %v", err))
			}
		}
	}
	if c.Params("model_name") == "data_model" {
		if res.UpsertedID != nil {
			helper.ServerInitstruct(org.Id)
		}
	}

	if collectionName == "templatetype" {
		var templateJson string
		if inputData["mobile_template_config"] != nil {

			templateJson = inputData["mobile_template_config"].(string)
		}
		updateMobileJson(c.Params("id"), org.Id, templateJson, inputData)
	}

	if collectionName == "stock_transfer" {
		inputData["_id"] = c.Params("id")
		ProcessStockTransafer(org.Id, inputData, userToken.UserId)
	}

	return shared.SuccessResponse(c, "Updated Successfully")
}
// update ledger for purchase changes
// get latest data from DB
// safe map getter
// DOMESTIC logic start
// check quantity change
// get purchase data
// prepare ledger filter
// check ledger exists
// create or update ledger
// INTERNATIONAL logic start
// get stock movements
// validate data
// map old movements
// loop new movements
// compare old vs new
// create new entry if not exists
// update if changed
// KERNEL logic start
// check quantity change
// update ledger if needed
func PurchaseLedgerUpdate(oldData map[string]interface{}, purchaseType string, org helper.Organization, userId string, updateDocID string, collectionName string) error {
	data, err := GetDataById(org.Id, updateDocID, collectionName)
	if err != nil {
		return fmt.Errorf("error Getting Input Data for ledger: %v", err)
	}

	safeGet := func(data map[string]interface{}, key string) interface{} {
		if data == nil {
			return nil
		}
		if v, ok := data[key]; ok {
			return v
		}
		return nil
	}

	if purchaseType == "domestic" {
		oldQuantity := helper.ToFloat64(oldData["quantity"])
		newQuantity := helper.ToFloat64(data["quantity"])

		if oldQuantity == newQuantity {
			return nil
		}
		purchasedata, err := GetDataById(org.Id, data["purchase_id"].(string), "purchase")
		if err != nil {
			return fmt.Errorf("error Getting Purchase Data ledger: %v", err)
		}
		ledgerRequest, _ := GetPurchaseLedgerRequest(org.Id, purchaseType, purchasedata, data, userId, data["_id"].(string))
		ledgerUpdateFilter := LedgerFilter{
			ProductID:   ledgerRequest.ProductId,
			PurchaseID:  ledgerRequest.PurchaseID,
			Origin:      ledgerRequest.Origin,
			RefId:       ledgerRequest.RefId,
			WarehouseID: ledgerRequest.WarehouseId,
		}
		exists, _ := ledgerEntryExists(org.Id, ledgerUpdateFilter)
		if !exists {
			err = ProcessInternationAndDomesticRCNPurchase(org.Id, purchaseType, purchasedata, data, userId, data["_id"].(string))
			if err != nil {
				return fmt.Errorf("error processing domestic purchase for %s: %v", data["_id"].(string), err)
			}
		} else {
			go UpdateLedgerEntryByIDs(org.Id, ledgerUpdateFilter, newQuantity, userId, "")
		}

		return nil
	}

	if purchaseType == "International" {
		newStockMovements, ok := data["stock_movement"].(primitive.A)
		if !ok || len(newStockMovements) == 0 {
			return errors.New("purchase: missing or empty stock_movement array")
		}

		oldStockMovements, _ := oldData["stock_movement"].(primitive.A)

		// ✅ Convert old stock movements into a map by _id
		oldMap := make(map[string]map[string]interface{})
		for _, item := range oldStockMovements {
			if sm, ok := item.(map[string]interface{}); ok {
				if id, ok := sm["_id"].(string); ok {
					oldMap[id] = sm
				}
			}
		}

		for _, item := range newStockMovements {
			sm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			smID, _ := sm["_id"].(string)
			newWeight, _ := sm["weight"].(float64)

			if newWeight <= 0 {
				continue
			}

			oldWeight := 0.0
			if oldSm, exists := oldMap[smID]; exists {
				oldWeight, _ = oldSm["weight"].(float64)

			} else {
				// 🆕 This is a new stock movement — not in old data
				log.Printf("🆕 New stock movement detected: %s (weight=%.2f)", smID, newWeight)
				err := ProcessInternationAndDomesticRCNPurchase(org.Id, purchaseType, data, sm, userId, smID)
				if err != nil {
					return fmt.Errorf("error processing new stock movement for %s: %v", smID, err)
				}
				continue
			}

			// 🔍 Compare old vs new weight
			if oldWeight == newWeight {
				continue // ✅ Same quantity → do nothing
			}

			// ✅ Weight changed → trigger stock adjustment
			diff := newWeight - oldWeight
			log.Printf("Stock movement %s changed: old=%.2f new=%.2f diff=%.2f", smID, oldWeight, newWeight, diff)
			ledgerRequest, _ := GetPurchaseLedgerRequest(org.Id, purchaseType, data, sm, userId, smID)
			ledgerUpdateFilter := LedgerFilter{
				ProductID:   ledgerRequest.ProductId,
				PurchaseID:  ledgerRequest.PurchaseID,
				Origin:      ledgerRequest.Origin,
				RefId:       ledgerRequest.RefId,
				WarehouseID: ledgerRequest.WarehouseId,
			}
			exists, _ := ledgerEntryExists(org.Id, ledgerUpdateFilter)
			if !exists {
				err := ProcessInternationAndDomesticRCNPurchase(org.Id, purchaseType, data, sm, userId, smID)
				if err != nil {
					return fmt.Errorf("error processing stock adjustment for %s: %v", smID, err)
				}
			} else {

				go UpdateLedgerEntryByIDs(org.Id, ledgerUpdateFilter, newWeight, userId, "")
			}
		}
	}

	if purchaseType == "kernel" {
		oldQuantity := helper.ToFloat64(oldData["quantity"])
		newQuantity := helper.ToFloat64(data["quantity"])

		if oldQuantity == newQuantity {
			return nil
		}
		if err != nil {
			return fmt.Errorf("error updating ledger: %v", err)
		}
		if oldQuantity != 0 {
			ledgerUpdateFilter := LedgerFilter{
				ProductID:   safeGet(data, "product_id").(string),
				PurchaseID:  safeGet(data, "warehouse_id").(string),
				Origin:      safeGet(data, "origin_id").(string),
				RefId:       safeGet(data, "_id").(string),
				WarehouseID: safeGet(data, "warehouse_id").(string),
			}
			go UpdateLedgerEntryByIDs(org.Id, ledgerUpdateFilter, newQuantity, userId, "")

		}

		return nil
	}

	return nil
}
// kernel and other purchase ledger update wrapper
// call common purchase processing function
// return error if processing fails
// return success if completed
func KernalAndOtherPurchaseUpdate(data map[string]interface{}, orgId string, userId string) error {
	err := ProcessInternationAndDomesticRCNPurchase(orgId, "kernel", data, nil, userId, data["_id"].(string))
	if err != nil {
		return fmt.Errorf("error processing stock adjustment for %s: %v", err)
	}
	return nil
}
// KernalAndOtherSaleUpdate handles kernel/RCN sale processing with batch support.
//
// This function processes sale data from tin_grid_data and performs:
// 1. Loose sale handling (direct kernel loose sale processing)
// 2. Batch sale processing for multiple grid items
// 3. Purchase mapping for kernel stock when purchase_id is missing
// 4. Warehouse resolution from purchase invoice or stock movement
// 5. Sale processing via ProcessSale or ProcessKernelSaleBatch
//
// Flow:
// - If type = "loose" → directly process kernel loose sale
// - If tin_grid_data is interface{} array → process dynamic input format
// - If tin_grid_data is map slice → process strongly typed format
// - For kernel items without purchase_id → fetch from production & purchase
// - Build enriched sale documents
// - Finally process batch sale in single operation
//
// NOTE:
// This function ensures correct stock linkage between production → purchase → sale
// and maintains warehouse-level tracking.
func KernalAndOtherSaleUpdate(inputData map[string]interface{}, orgId string, userId string) error {

	if inputData["type"] == "loose" {
		ProcessKernelLooseSale(orgId, inputData, userId)
		return nil
	}

	if data, ok := inputData["tin_grid_data"].([]interface{}); ok && len(data) == 0 {
		if len(inputData["tin_grid_data"].([]interface{})) > 0 {
			// Prepare all sale documents for batch processing
			var saleDocuments []map[string]interface{}

			for i := 0; i < len(inputData["tin_grid_data"].([]interface{})); i++ {
				data := inputData["tin_grid_data"].([]interface{})[i].(map[string]interface{})

				purchaseID := ""
				if pid, ok := data["purchase_id"].(string); ok {
					purchaseID = pid
				}

				stockType := "RCN"
				if productValue, ok := data["product_id"].(string); ok && productValue != "" {
					stockType = "kernel"
				}

				if purchaseID == "" && stockType == "kernel" {
					if batchNo, ok := data["batch_no"].([]interface{}); ok && len(batchNo) > 0 {
						for _, batchItem := range batchNo {
							if batch, ok := batchItem.(map[string]interface{}); ok {
								if prodID, ok := batch["production_id"].(string); ok {
									var production map[string]interface{}
									err := database.GetConnection(orgId).Collection("productions").FindOne(context.Background(), bson.M{"_id": prodID}).Decode(&production)
									if err != nil {
										continue
									}
									if pid, ok := production["purchase_id"].(string); ok && pid != "" {
										saleData := make(map[string]interface{})
										for k, v := range data {
											saleData[k] = v
										}
										saleData["purchase_id"] = pid

										var purchase map[string]interface{}
										err := database.GetConnection(orgId).Collection("purchase").FindOne(context.Background(), bson.M{"_id": pid}).Decode(&purchase)
										if err == nil {
											if invoiceDetails, ok := purchase["invoice_details"].(primitive.A); ok {
												for _, item := range invoiceDetails {
													if invoice, ok := item.(map[string]interface{}); ok {
														if warehouseId, ok := invoice["warehouse_id"].(string); ok && warehouseId != "" {
															saleData["warehouse"] = warehouseId
															break
														}
													}
												}
											} else if stockMovement, ok := purchase["stock_movement"].(primitive.A); ok {
												for _, item := range stockMovement {
													if stock, ok := item.(map[string]interface{}); ok {
														if warehouse, ok := stock["warehouse"].(string); ok && warehouse != "" {
															saleData["warehouse"] = warehouse
															break
														}
													}
												}
											}
										}
										err = ProcessSale(orgId, saleData, userId)
										if err != nil {
											return fmt.Errorf("error processing sale: %v", err)
										}
									}
								}
							}
						}
						return nil
					}
				}

				//appending data from parent data
				if val, ok := inputData["template_id"].(string); ok && val != "" {
					data["template_id"] = val //customer name
				}
				if val, ok := inputData["sold_on"].(time.Time); ok && val != (time.Time{}) {
					data["sold_on"] = val
				}
				if val, ok := inputData["product_id"].(string); ok && val != "" {
					data["product_id"] = val
				}
				if val, ok := inputData["_id"].(string); ok && val != "" {
					data["sale_id"] = val
				}

				saleDocuments = append(saleDocuments, data)
			}

			// Process all items in a single transaction with cumulative balance tracking
			if len(saleDocuments) > 0 {
				err := ProcessKernelSaleBatch(orgId, saleDocuments, userId)
				if err != nil {
					return fmt.Errorf("error processing kernel sale batch: %v", err)
				}
			}
		}
	} else {
		tinGridData, ok := inputData["tin_grid_data"].([]map[string]interface{})
		if ok && len(tinGridData) > 0 {
			if len(tinGridData) > 0 {
				// Prepare all sale documents for batch processing
				var saleDocuments []map[string]interface{}

				for i := 0; i < len(tinGridData); i++ {
					data := tinGridData[i]

					purchaseID := ""
					if pid, ok := data["purchase_id"].(string); ok {
						purchaseID = pid
					}

					stockType := "RCN"
					if productValue, ok := data["product_id"].(string); ok && productValue != "" {
						stockType = "kernel"
					}

					if purchaseID == "" && stockType == "kernel" {
						if batchNo, ok := data["batch_no"].([]interface{}); ok && len(batchNo) > 0 {
							for _, batchItem := range batchNo {
								if batch, ok := batchItem.(map[string]interface{}); ok {
									if prodID, ok := batch["production_id"].(string); ok {
										var production map[string]interface{}
										err := database.GetConnection(orgId).Collection("productions").FindOne(context.Background(), bson.M{"_id": prodID}).Decode(&production)
										if err != nil {
											continue
										}
										if pid, ok := production["purchase_id"].(string); ok && pid != "" {
											saleData := make(map[string]interface{})
											for k, v := range data {
												saleData[k] = v
											}
											saleData["purchase_id"] = pid

											var purchase map[string]interface{}
											err := database.GetConnection(orgId).Collection("purchase").FindOne(context.Background(), bson.M{"_id": pid}).Decode(&purchase)
											if err == nil {
												if invoiceDetails, ok := purchase["invoice_details"].(primitive.A); ok {
													for _, item := range invoiceDetails {
														if invoice, ok := item.(map[string]interface{}); ok {
															if warehouseId, ok := invoice["warehouse_id"].(string); ok && warehouseId != "" {
																saleData["warehouse"] = warehouseId
																break
															}
														}
													}
												} else if stockMovement, ok := purchase["stock_movement"].(primitive.A); ok {
													for _, item := range stockMovement {
														if stock, ok := item.(map[string]interface{}); ok {
															if warehouse, ok := stock["warehouse"].(string); ok && warehouse != "" {
																saleData["warehouse"] = warehouse
																break
															}
														}
													}
												}
											}
											err = ProcessSale(orgId, saleData, userId)
											if err != nil {
												return fmt.Errorf("error processing sale: %v", err)
											}
										}
									}
								}
							}
							return nil
						}
					}

					//appending data from parent data
					if val, ok := inputData["template_id"].(string); ok && val != "" {
						data["template_id"] = val //customer name
					}
					if val, ok := inputData["sold_on"].(time.Time); ok && val != (time.Time{}) {
						data["sold_on"] = val
					}
					if val, ok := inputData["product_id"].(string); ok && val != "" {
						data["product_id"] = val
					}
					if val, ok := inputData["_id"].(string); ok && val != "" {
						data["sale_id"] = val
					}

					saleDocuments = append(saleDocuments, data)
				}

				// Process all items in a single transaction with cumulative balance tracking
				if len(saleDocuments) > 0 {
					err := ProcessKernelSaleBatch(orgId, saleDocuments, userId)
					if err != nil {
						return fmt.Errorf("error processing kernel sale batch: %v", err)
					}
				}
			}
		}

	}

	return nil
}

// ProcessKernelLooseSale handles loose kernel sales with transactional stock updates.
//
// This function performs:
// 1. Extract and validate sale quantity
// 2. Fetch selected stock from stock_in_hand
// 3. Derive warehouse, factory, origin, and purchase details
// 4. Build stock ledger entry for the sale
// 5. Start MongoDB transaction for safe stock update
// 6. Calculate opening, transaction, and closing balances
// 7. Prevent negative stock (insufficient stock check)
// 8. Insert stock ledger record
// 9. Update stock balance in stock_in_hand
//
// NOTE:
// Uses MongoDB session transaction to ensure stock consistency
// between ledger and stock tables during sale processing.
func ProcessKernelLooseSale(orgId string, inputData map[string]interface{}, userId string) error {
	database := database.GetConnection(orgId)
	client := database.Client()

	var quantity float64
	if quantityFloat, ok := inputData["total_quantity"].(float64); ok {
		quantity = quantityFloat
	} else if quantityInt, ok := inputData["total_quantity"].(int); ok {
		quantity = float64(quantityInt)
	}
	if quantity == 0 {
		return errors.New("sale: missing or zero quantity")
	}

	stockSelectionID := inputData["stock_selection"].(string)
	var stockInHand map[string]interface{}
	err := database.Collection("stock_in_hand").FindOne(context.Background(), bson.M{"_id": stockSelectionID}).Decode(&stockInHand)
	if err != nil {
		return fmt.Errorf("stock selection not found: %v", err)
	}

	stockType := "kernel"
	productID := inputData["product_id"].(string)

	origin := ""
	if originValue, ok := stockInHand["origin"].(string); ok {
		origin = originValue
	}

	warehouseID := ""
	if warehouseValue, ok := stockInHand["warehouse_id"].(string); ok {
		warehouseID = warehouseValue
	}

	factoryID := ""
	if warehouseID != "" {
		isfactoryWarehouse, factoryId := WareHouseCheck(orgId, warehouseID)
		if isfactoryWarehouse {
			factoryID = factoryId
		}
	}

	purchaseID := stockInHand["purchase_id"].(string)

	customerName := ""
	transactionDate := time.Now()
	if soldOnStr, ok := inputData["created_on"]; ok {
		transactionDate = helper.ParseDate(soldOnStr)
	}

	ledgerEntry := StockLedgerEntry{
		ID:                 uuid.New().String(),
		PurchaseID:         purchaseID,
		Origin:             origin,
		StockType:          stockType,
		WarehouseId:        warehouseID,
		ProductId:          productID,
		FactoryId:          factoryID,
		TransactionType:    "sale",
		SaleId:             inputData["_id"].(string),
		TransactionDate:    transactionDate,
		CustomerName:       customerName,
		Remarks:            "Kernel loose sale ledger",
		CreatedBy:          userId,
		CreatedOn:          time.Now(),
		TransactionBalance: quantity,
		Location:           warehouseID,
		RefId:              inputData["_id"].(string),
	}

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		openingBalance, err := getAvailableBalance(sessionCtx, orgId, purchaseID, warehouseID, origin, "WIP", productID)
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

		//TODO :change as ledger
		if _, err := database.Collection("stock_ledger").InsertOne(sessionCtx, ledgerEntry); err != nil {
			return nil, err
		}
		if err := updateStockBalance(sessionCtx, orgId, origin, "WIP", warehouseID, factoryID, purchaseID, productID, closingBalance-openingBalance, userId, "", ""); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func SaleLedgerUpdate(saleDoc map[string]interface{}, orgId string, userId string) error {
	typeOfSale := "kernel"
	if val, ok := saleDoc["type_of_sale"].(string); ok && val != "" {
		typeOfSale = val
	}

	if typeOfSale == "Domestic" {
		ledgerRequest, _ := GetSaleLedgerRequest(orgId, saleDoc, userId)
		ledgerUpdateFilter := LedgerFilter{
			ProductID:   ledgerRequest.ProductId,
			PurchaseID:  ledgerRequest.PurchaseID,
			Origin:      ledgerRequest.Origin,
			RefId:       ledgerRequest.RefId,
			WarehouseID: ledgerRequest.WarehouseId,
		}

		exists, _ := ledgerEntryExists(orgId, ledgerUpdateFilter)

		if exists {
			go UpdateLedgerEntryByIDs(orgId, ledgerUpdateFilter, saleDoc["quantity"].(float64), userId, "")
		} else {
			err := ProcessSale(orgId, saleDoc, userId)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}
		}
	}
	return nil
}

func GetSHStockTypeByID(orgID string, ID string, collectionName string) (map[string]interface{}, error) {
	var data map[string]interface{}
	err := database.GetConnection(orgID).Collection(collectionName).FindOne(context.Background(), bson.M{"_id": ID}).Decode(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetCustomerByOrgID(orgID string, ID string) (map[string]interface{}, error) {
	var data map[string]interface{}
	err := database.GetConnection("shared").Collection("organization").FindOne(context.Background(), bson.M{"_id": ID}).Decode(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}
func GetDataById(orgID string, ID string, collectionName string) (map[string]interface{}, error) {
	var data map[string]interface{}
	err := database.GetConnection(orgID).Collection(collectionName).FindOne(context.Background(), bson.M{"_id": ID}).Decode(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetActiveProcessesByFactoryId(orgId string, factoryId string) ([]bson.M, error) {
	fmt.Printf("DEBUG: Getting active processes for org: %s, factory: %s\n", orgId, factoryId)

	// First check if factory exists
	ctx := context.Background()
	var factoryExists bson.M
	err := database.GetConnection(orgId).Collection("config").FindOne(ctx, bson.M{"_id": factoryId}).Decode(&factoryExists)
	if err != nil {
		fmt.Printf("ERROR: Factory %s not found in config collection: %v\n", factoryId, err)
		return nil, fmt.Errorf("factory %s not found in config collection: %v", factoryId, err)
	}
	fmt.Printf("DEBUG: Factory exists in config collection\n")

	// Print the entire factory document to see its structure
	fmt.Printf("DEBUG: Complete factory document keys: ")
	for key := range factoryExists {
		fmt.Printf("%s ", key)
	}
	fmt.Println()

	// Check if factory_process_data exists
	if factoryProcessData, exists := factoryExists["factory_process_data"]; exists {
		fmt.Printf("DEBUG: factory_process_data exists, type: %T, value: %v\n", factoryProcessData, factoryProcessData)
	} else {
		fmt.Printf("ERROR: factory_process_data field not found\n")
		return nil, fmt.Errorf("factory_process_data field not found for factory: %s", factoryId)
	}

	// MongoDB aggregation pipeline to match factory ID and filter active processes
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"_id": factoryId,
			},
		},
		{
			"$addFields": bson.M{
				"active_processes": bson.M{
					"$filter": bson.M{
						"input": "$factory_process_data",
						"cond": bson.M{
							"$eq": []interface{}{"$$this.status", "Active"},
						},
					},
				},
			},
		},
	}

	cursor, err := database.GetConnection(orgId).Collection("config").Aggregate(ctx, pipeline)
	if err != nil {
		fmt.Printf("ERROR: Aggregation failed: %v\n", err)
		return nil, fmt.Errorf("aggregation failed: %v", err)
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err = cursor.All(ctx, &results); err != nil {
		fmt.Printf("ERROR: Failed to decode aggregation results: %v\n", err)
		return nil, fmt.Errorf("failed to decode results: %v", err)
	}

	if len(results) == 0 {
		fmt.Printf("ERROR: No results returned for factory %s\n", factoryId)
		return nil, fmt.Errorf("no results returned for factory %s", factoryId)
	}

	fmt.Printf("DEBUG: Aggregation successful, found %d results\n", len(results))
	return results, nil
}

func InsertOrgUser(inputData map[string]interface{}, orgId string) error {
	fmt.Printf("[InsertOrgUser] Starting for orgId: %s\n", orgId)
	var orgData map[string]interface{}
	orgData, err := GetDataById("shared", orgId, "organization")
	if err != nil {
		fmt.Printf("[InsertOrgUser Error] Failed to get org data: %v\n", err)
		return err
	}
	var mobileNo string

	// Get domain_name from inputData (from PUT request body)
	domainName := "app" // Default
	if val, exists := inputData["domain_name"]; exists && val != nil {
		if dn, ok := val.(string); ok && dn != "" {
			domainName = dn
		}
	}
	fmt.Printf("[InsertOrgUser] Using domain: %s\n", domainName)

	selectedRole, ok := orgData["selected_role_id"].(string)
	if !ok || selectedRole == "" {
		selectedRole = "OA"
	}

	// Check required fields
	if orgData["first_name"] == nil {
		fmt.Printf("[InsertOrgUser Error] Missing first_name in organization data\n")
		return fmt.Errorf("missing first_name in organization data")
	}
	if orgData["last_name"] == nil {
		fmt.Printf("[InsertOrgUser Error] Missing last_name in organization data\n")
		return fmt.Errorf("missing last_name in organization data")
	}
	if orgData["email_id"] == nil {
		fmt.Printf("[InsertOrgUser Error] Missing email_id in organization data\n")
		return fmt.Errorf("missing email_id in organization data")
	}

	firstName := orgData["first_name"].(string)
	lastName := orgData["last_name"].(string)

	factories, _ := orgData["factories"].(map[string]interface{})
	if factories == nil || factories["factory_contact"] == nil {
		mobileNo, _ = orgData["mobile_number"].(string)
	} else {
		mobileNo, _ = orgData["org_primary_contact_number"].(string)
	}

	emailId := orgData["email_id"].(string)
	password, ok := orgData["password"].(string)

	if !ok || password == "" {
		password = helper.GenerateRandomString(8)
		// Store the generated password in the organization record for consistency
		database.SharedDB.Collection("organization").UpdateOne(context.Background(), bson.M{"_id": orgId}, bson.M{"$set": bson.M{"password": password}})
	}
	Id := orgData["_id"].(string)
	err = CreateDomainAndSendOnboardingMail(emailId, domainName)
	if err != nil {
		return err
	}

	// Generate and update the password hash
	pwd, err := helper.GeneratePasswordHash(password)
	if err != nil {
		return err
	}
	pwdBinary := primitive.Binary{Subtype: 0x00, Data: pwd}

	// NEW: Check if user already exists to prevent duplicates and password overrides
	var existingUser map[string]interface{}
	err = database.SharedDB.Collection("user").FindOne(context.Background(), bson.M{"email": emailId}).Decode(&existingUser)
	if err == nil {
		fmt.Printf("[InsertOrgUser] User %s already exists. Syncing credentials and data to org DB.\n", emailId)

		// Update password hash and profile status
		existingUser["pwd"] = pwdBinary
		existingUser["is_profile_completed"] = true

		// Ensure both databases have the updated user using Upsert
		database.GetConnection(orgId).Collection("user").UpdateOne(
			context.Background(),
			bson.M{"email": emailId},
			bson.M{"$set": existingUser},
			options.Update().SetUpsert(true),
		)

		database.SharedDB.Collection("user").UpdateOne(
			context.Background(),
			bson.M{"email": emailId},
			bson.M{"$set": bson.M{"pwd": pwdBinary, "is_profile_completed": true}},
		)
	} else {
		// User doesn't exist, create new user
		fmt.Printf("[InsertOrgUser] Creating new user for: %s\n", emailId)

		createUserData := map[string]interface{}{
			"_id":                  uuid.New().String(),
			"name":                 firstName + " " + lastName,
			"mobile_number":        mobileNo,
			"pwd":                  pwdBinary,
			"email":                emailId,
			"role":                 selectedRole,
			"status":               "Active",
			"user_type":            "687788232ae447bb0d41c72b",
			"created_on":           time.Now(),
			"org_id":               Id,
			"is_profile_completed": true,
		}

		// Insert into both DBs
		database.GetConnection(orgId).Collection("user").InsertOne(context.Background(), createUserData)
		database.SharedDB.Collection("user").InsertOne(context.Background(), createUserData)
	}

	var tempUserData map[string]interface{}
	database.GetConnection("shared").Collection("temporary_user").FindOne(context.Background(), bson.M{"email_id": emailId}).Decode(&tempUserData)

	// if tempUserData != nil {
	// 	var customerType map[string]interface{}
	// 	var serviceProvider bool
	// 	var serviceProviderLoginAvailable bool
	// 	var parentOrg string
	// 	var userId string
	// 	customerType = tempUserData["customer_type"].(map[string]interface{})
	// 	if customerType["serviceprovider"].(bool) {
	// 		serviceProvider = true
	// 	}
	// 	if serviceProvider {
	// 		serviceProviderLoginAvailable = tempUserData["is_login_available"].(bool)
	// 	}

	// 	if serviceProviderLoginAvailable {
	// 		parentOrg = tempUserData["parent_org"].(string)
	// 		userId = tempUserData["_id"].(string)
	// 		// emailId = userId
	// 		database.GetConnection(parentOrg).Collection("customer").UpdateOne(context.Background(), bson.M{"_id": userId}, bson.M{"$set": bson.M{"domain_register": true, "org_id": orgId}})
	// 	}

	// }
	database.GetConnection("shared").Collection("temporary_user").DeleteOne(context.Background(), bson.M{"email_id": emailId})

	// Send onboarding email with login URL using domain_name from PUT request
	loginURL := "https://" + domainName + ".kajupro.com/login"
	fmt.Printf("[InsertOrgUser] Sending email to: %s with URL: %s\n", emailId, loginURL)
	SendOnBoardingCompletedMail(loginURL, emailId, emailId, password)
	fmt.Printf("[InsertOrgUser] Email sent successfully to: %s\n", emailId)
	return nil
}

// func GenerateMultipleMaintenanceData(data map[string]interface{}, orgId string, parentId string, oldData []bson.M, isPost bool) error {
// 	if data["frequency"] == nil || data["duration"] == nil {
// 		return nil
// 	}
// 	var startTime time.Time
// 	var startTimeIncluded bool
// 	var newStartTimeString string
// 	var oldStartTimeString string
// 	var err error
// 	frequency := data["frequency"].(string)
// 	duration := helper.ToInt(data["duration"])
// 	data["status"] = "Not Done"
// 	data["parent_id"] = parentId
// 	data["maintenance_type"] = "Scheduled"
// 	data["type"] = "Machine Maintenance"

// 	if data["start_time"] != nil {
// 		startTimeStr := data["start_time"].(string)
// 		newStartTimeString = data["start_time"].(string)
// 		startTime, err = time.Parse("03:04 PM", startTimeStr)
// 		if err != nil {
// 			fmt.Println(err.Error(), "git")
// 			return fmt.Errorf("invalid start_time format: %v", err)
// 		} else {
// 			fmt.Println("No error")
// 		}
// 		startTimeIncluded = true

// 	}

// 	// Start scheduling from the current date
// 	startDate := time.Now()

// 	if !isPost {
// 		res := oldData[0]
// 		oldDuration := helper.ToInt(res["duration"])
// 		oldFrequency := res["frequency"]
// 		if res["start_time"] != nil {
// 			oldStartTimeString = res["start_time"].(string)
// 		}
// 		if duration != oldDuration {
// 			deleteFilter := bson.M{"parent_id": parentId}
// 			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").DeleteMany(ctx, deleteFilter)
// 			if err != nil {
// 				return shared.InternalServerError("Error Deleting Data")
// 			}
// 		} else if oldFrequency != frequency {
// 			deleteFilter := bson.M{"parent_id": parentId}
// 			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").DeleteMany(ctx, deleteFilter)
// 			if err != nil {
// 				return shared.InternalServerError("Error Deleting Data")
// 			}
// 		} else if oldStartTimeString != newStartTimeString {
// 			deleteFilter := bson.M{"parent_id": parentId}
// 			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").DeleteMany(ctx, deleteFilter)
// 			if err != nil {
// 				return shared.InternalServerError("Error Deleting Data")
// 			}
// 		}

// 		if (oldDuration == duration) && (oldFrequency == frequency) && (oldStartTimeString == newStartTimeString) {
// 			return nil
// 		}
// 	}

// 	// lastDateOfCurrentMonth := helper.GetLastDateOfCurrentMonth()
// 	// lastDate := lastDateOfCurrentMonth.Day()
// 	// currentDate := time.Now().Day()
// 	// loopDate := lastDate - currentDate
// 	// fmt.Println(loopDate)

// 	var startHour int
// 	var startMinute int

// 	if startTimeIncluded {
// 		startHour = startTime.Hour()
// 		startMinute = startTime.Minute()
// 	} else {
// 		startHour = 9
// 		startMinute = 0
// 	}

// 	leaveCollection := database.GetConnection(orgId).Collection("leave")

// 	if frequency == "Hourly" {
// 		durationHours := int(duration) // Dynamic duration (e.g., 2 hours)

// 		for day := 1; day <= 7; day++ {
// 			// Calculate the start time for the current day
// 			dailyStartTime := time.Date(
// 				startDate.Year(), startDate.Month(), startDate.Day()+day,
// 				startHour, startMinute, 0, 0, startDate.Location(),
// 			)

// 			// Skip Sundays
// 			// if dailyStartTime.Weekday() == time.Sunday {
// 			// 	continue
// 			// }

// 			// Set the current time to the start of the day
// 			currentTime := dailyStartTime

// 			// Ensure the start time is valid (adjust to 9:00 AM if outside the valid range)
// 			if currentTime.Hour() < 9 || currentTime.Hour() >= 22 {
// 				currentTime = time.Date(
// 					currentTime.Year(), currentTime.Month(), currentTime.Day(),
// 					9, 0, 0, 0, currentTime.Location(),
// 				)
// 			}
// 			present := helper.IsSchedulePresent(leaveCollection, currentTime)
// 			if present {
// 				continue
// 			}
// 			// Generate occurrences for the day
// 			for currentTime.Hour() < 22 && currentTime.Hour() >= 9 {
// 				// Add the occurrence to the database
// 				delete(data, "_id")
// 				data["scheduled_date"] = currentTime
// 				update := bson.M{"$set": data}

// 				uniqueId := uuid.New().String()
// 				_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
// 					ctx, helper.DocIdFilter(uniqueId), update, updateOpts,
// 				)
// 				if err != nil {
// 					return shared.BadRequest(err.Error())
// 				}

// 				// Increment the time by durationHours
// 				currentTime = currentTime.Add(time.Duration(durationHours) * time.Hour)

// 				// Stop if the next occurrence goes beyond 10:00 PM
// 				if currentTime.Hour() > 22 {
// 					break
// 				}
// 			}
// 		}

// 		return nil
// 	} else if frequency == "Daily" {
// 		for day := 1; day <= 30; day += int(duration) {
// 			// Calculate the start time for the current day
// 			dailyStartTime := time.Date(
// 				startDate.Year(), startDate.Month(), startDate.Day()+day,
// 				startHour, startMinute, 0, 0, startDate.Location(),
// 			)

// 			// Skip Sundays
// 			// if dailyStartTime.Weekday() == time.Sunday {
// 			// 	continue
// 			// }
// 			present := helper.IsSchedulePresent(leaveCollection, dailyStartTime)
// 			if present {
// 				continue
// 			}

// 			// Prepare the occurrence
// 			delete(data, "_id")
// 			data["scheduled_date"] = dailyStartTime
// 			update := bson.M{"$set": data}

// 			uniqueId := uuid.New().String()
// 			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
// 				ctx, helper.DocIdFilter(uniqueId), update, updateOpts,
// 			)
// 			if err != nil {
// 				return shared.BadRequest(err.Error())
// 			}
// 		}
// 		return nil
// 	} else if frequency == "Monthly" {
// 		for month := 0; month <= 12; month += duration {
// 			// Add the number of months to startDate
// 			occurrenceDate := startDate.AddDate(0, month, 0)

// 			// Adjust the day to match startDate's day, handle months with fewer days
// 			day := startDate.Day()
// 			daysInMonth := daysIn(occurrenceDate.Year(), occurrenceDate.Month())

// 			// If the start day exceeds the days in the current month, set to the last day
// 			if day > daysInMonth {
// 				day = daysInMonth
// 			}

// 			// Create the occurrence date with the adjusted day
// 			occurrenceDate = time.Date(
// 				occurrenceDate.Year(),
// 				occurrenceDate.Month(),
// 				day,
// 				startHour,
// 				startMinute,
// 				0,
// 				0,
// 				occurrenceDate.Location(),
// 			)

// 			// Skip Sundays
// 			// if occurrenceDate.Weekday() == time.Sunday {
// 			// 	continue
// 			// }
// 			present := helper.IsSchedulePresent(leaveCollection, occurrenceDate)
// 			if present {
// 				continue
// 			}

// 			// Prepare the occurrence
// 			delete(data, "_id")
// 			data["scheduled_date"] = occurrenceDate
// 			update := bson.M{"$set": data}

// 			uniqueId := uuid.New().String()
// 			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
// 				ctx, helper.DocIdFilter(uniqueId), update, updateOpts,
// 			)
// 			if err != nil {
// 				return shared.BadRequest(err.Error())
// 			}
// 		}

// 	} else if frequency == "Weekly" {
// 		// Define the number of weeks to generate in the next month
// 		daysInMonth := daysIn(startDate.Year(), startDate.Month()) // Get the days in the current month
// 		weeksToGenerate := daysInMonth / 7                         // Calculate the number of full weeks in the month

// 		// Adjust for any partial week at the end of the month if needed
// 		for week := 1; week <= weeksToGenerate; week++ {
// 			// Calculate the occurrence date for the current week (7 days apart)
// 			occurrenceDate := startDate.AddDate(0, 0, week*7*int(duration))

// 			// Ensure the scheduled time is within the daily bounds (set start time)
// 			occurrenceDate = time.Date(
// 				occurrenceDate.Year(),
// 				occurrenceDate.Month(),
// 				occurrenceDate.Day(),
// 				startHour,
// 				startMinute,
// 				0,
// 				0,
// 				occurrenceDate.Location(),
// 			)

// 			// Skip Sundays
// 			// if occurrenceDate.Weekday() == time.Sunday {
// 			// 	continue
// 			// }

// 			present := helper.IsSchedulePresent(leaveCollection, occurrenceDate)
// 			if present {
// 				continue
// 			}
// 			// Prepare the occurrence
// 			delete(data, "_id")
// 			data["scheduled_date"] = occurrenceDate
// 			update := bson.M{"$set": data}

// 			uniqueId := uuid.New().String()
// 			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
// 				ctx, helper.DocIdFilter(uniqueId), update, updateOpts,
// 			)
// 			if err != nil {
// 				return shared.BadRequest(err.Error())
// 			}
// 		}
// 	}

// 	return nil
// }

// func daysIn(year int, month time.Month) int {
// 	t := time.Date(year, month, 0, 0, 0, 0, 0, time.UTC)
// 	return t.Day()
// }

func UpdateDocForPacking(c *fiber.Ctx) error {
	//Get the orgId from Header
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// to  Get the User Details from Token
	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("model_name")
	// collectionName, err := helper.CollectionNameGet(c.Params("model_name"), org.Id)
	// if err != nil {
	// 	return shared.BadRequest(err.Error())
	// }

	//sNo := inputData["s_no"]

	// Validate the input data based on the data model

	// inputData, errmsg := helper.UpdateValidateInDatamodel(collectionName, string(c.Body()), org.Id)
	// if errmsg != nil {
	// 	err := helper.GenerateErrorMessage(errmsg)
	// 	// Return the error message map as part of BadRequest response
	// 	return shared.BadRequest(err)
	// }
	var inputData map[string]interface{}
	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err)
	}
	// UpdateData := helper.UpdateFieldsWithParentKey(inputData, "", updatedDatas)
	helper.UpdateDateObject(inputData)
	var sold bool
	var update bson.M
	if inputData["sold"] != nil {
		sold = inputData["sold"].(bool)
	}
	if sold {
		update = bson.M{
			"$set": bson.M{
				"serials.$.status":      "sold",
				"serials.$.date":        time.Now(),
				"serials.$.template_id": inputData["template_id"].(string),
			},
		}

	} else {
		update = bson.M{
			"$set": bson.M{
				"serials.$.status":      "packed",
				"serials.$.date":        time.Now(),
				"serials.$.template_id": "",
			},
		}

	}

	filter := bson.M{
		"_id":          c.Params("id"),
		"serials.s_no": inputData["s_no"],
	}

	inputData["update_on"] = time.Now()
	inputData["update_by"] = userToken.UserId
	// Update data in the collection
	res, err := database.GetConnection(org.Id).Collection(collectionName).UpdateOne(ctx, filter, update)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	if c.Params("model_name") == "data_model" {
		if res.UpsertedID != nil {
			helper.ServerInitstruct(org.Id)

		}
	}

	return shared.SuccessResponse(c, "Updated Successfully")
}

func updatePacking(inputData []interface{}, orgid string, userid string, id string) error {
	helper.UpdateDateObject(inputData)

	for _, raw := range inputData {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue // skip if not a map
		}

		var sold bool
		var productionId string

		if val, exists := item["sold"]; exists {
			if b, ok := val.(bool); ok {
				sold = b
			}
		}

		if val, exists := item["production_id"]; exists {
			if s, ok := val.(string); ok {
				productionId = s
			}
		}

		var update bson.M
		if sold {
			update = bson.M{
				"$set": bson.M{
					"serials.$.status":      "sold",
					"serials.$.date":        time.Now(),
					"serials.$.template_id": id,
				},
			}
		} else {
			update = bson.M{
				"$set": bson.M{
					"serials.$.status":      "packed",
					"serials.$.date":        time.Now(),
					"serials.$.template_id": "",
				},
			}
		}
		filter := bson.M{
			"_id":          productionId,
			"serials.s_no": item["s_no"],
		}

		item["update_on"] = time.Now()
		item["update_by"] = userid

		_, err := database.GetConnection(orgid).Collection("productions").UpdateOne(ctx, filter, update)
		if err != nil {
			return shared.BadRequest(err.Error())
		}
	}

	return nil
}

func Insert(orgId string, collectionName string, inputData map[string]interface{}) (*mongo.InsertOneResult, error) {
	res, err := database.GetConnection(orgId).Collection(collectionName).InsertOne(ctx, inputData)

	if err != nil {
		var dupValue string
		errors, id, Name := IsDup(err)
		fmt.Println(errors)
		if errors {
			if id != "" {
				dupValue = id
			}
			if Name != "" {
				dupValue = Name
			}
			return res, fiber.NewError(200, "Duplicate Value : "+dupValue)
		}

		return res, err
	}
	return res, nil
}

// handleIDGeneration generates or handles the ID in the input data.
func GetDocByIdHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}
	id := c.Params("id")
	encodedID, _ := url.PathUnescape(id)
	filter := helper.DocIdFilter(encodedID)
	collectionName := c.Params("collectionName")
	if collectionName == "organization" || collectionName == "user_type" || collectionName == "master_menu" || collectionName == "role_acl" {
		org.Id = "shared"
	}
	fmt.Println(filter, org.Id)
	response, err := helper.GetQueryResult(org.Id, collectionName, filter, int64(0), int64(1), nil)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// if collectionName == "job_work" {
	// 	finalpipeline := bson.A{
	// 		bson.D{{"$match", bson.D{{"_id", c.Params("id")}}}},
	// 		bson.D{
	// 			{"$lookup",
	// 				bson.D{
	// 					{"from", "lots"},
	// 					{"localField", "lot_number"},
	// 					{"foreignField", "_id"},
	// 					{"as", "lot_result"},
	// 				},
	// 			},
	// 		},
	// 		bson.D{
	// 			{"$addFields",
	// 				bson.D{
	// 					{"available_lot_weight",
	// 						bson.D{
	// 							{"$sum",
	// 								bson.D{
	// 									{"$ifNull",
	// 										bson.A{
	// 											"$lot_result.weight",
	// 											0,
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 		bson.D{
	// 			{"$lookup",
	// 				bson.D{
	// 					{"from", "job_work"},
	// 					{"let",
	// 						bson.D{
	// 							{"id", "$_id"},
	// 							{"lot_no", "$lot_number"},
	// 						},
	// 					},
	// 					{"pipeline",
	// 						bson.A{
	// 							bson.D{
	// 								{"$match",
	// 									bson.D{
	// 										{"$expr",
	// 											bson.D{
	// 												{"$gt",
	// 													bson.A{
	// 														bson.D{
	// 															{"$size",
	// 																bson.D{
	// 																	{"$setIntersection",
	// 																		bson.A{
	// 																			"$$lot_no",
	// 																			"$lot_number",
	// 																		},
	// 																	},
	// 																},
	// 															},
	// 														},
	// 														0,
	// 													},
	// 												},
	// 											},
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 					},
	// 					{"as", "result"},
	// 				},
	// 			},
	// 		},
	// 		bson.D{
	// 			{"$addFields",
	// 				bson.D{
	// 					{"sold_weight",
	// 						bson.D{
	// 							{"$sum",
	// 								bson.D{
	// 									{"$ifNull",
	// 										bson.A{
	// 											"$result.weight",
	// 											0,
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 		bson.D{
	// 			{"$set",
	// 				bson.D{
	// 					{"available_lot_weight",
	// 						bson.D{
	// 							{"$subtract",
	// 								bson.A{
	// 									"$available_lot_weight",
	// 									"$sold_weight",
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	}
	// 	// fmt.Println(finalpipeline)
	// 	// To Get the Data from Db
	// 	Response, err := helper.GetAggregateQueryResult(org.Id, collectionName, finalpipeline)
	// 	if err != nil {
	// 		shared.InternalServerError(err.Error())
	// 	}
	// 	fmt.Println(Response)
	// 	res := Response[0]
	// 	lotWeight := res["available_lot_weight"]
	// 	response[0]["available_lot_weight"] = lotWeight
	// }
	// } else if collectionName == "sale" {
	// 	finalpipeline := bson.A{
	// 		bson.D{{"$match", bson.D{{"_id", c.Params("id")}}}},
	// 		bson.D{
	// 			{"$lookup",
	// 				bson.D{
	// 					{"from", "origin"},
	// 					{"localField", "origin"},
	// 					{"foreignField", "country_code"},
	// 					{"as", "origin_result"},
	// 				},
	// 			},
	// 		},
	// 		bson.D{
	// 			{"$unwind",
	// 				bson.D{
	// 					{"path", "$origin_result"},
	// 					{"preserveNullAndEmptyArrays", true},
	// 				},
	// 			},
	// 		},
	// 		bson.D{{"$set", bson.D{{"origin", "$origin_result.name"}}}},
	// 	}

	// 	// fmt.Println(finalpipeline)
	// 	// To Get the Data from Db
	// 	Response, err := helper.GetAggregateQueryResult(org.Id, collectionName, finalpipeline)
	// 	if err != nil {
	// 		shared.InternalServerError(err.Error())
	// 	}

	// 	response = Response
	// }

	return shared.SuccessResponse(c, response)

}

func DeleteById(c *fiber.Ctx) error {

	// Get the orgId from Header
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// userToken := utils.GetUserTokenValue(c)

	// Check if the collectionName is "user_files" for special handling
	if c.Params("collectionName") == "user_files" {
		return helper.DeleteFileIns3(c)
	}

	// Construct filter based on document ID
	filter := helper.DocIdFilter(c.Params("id"))

	// // Check if collectionName is not "data_model" AND not "model_config"

	// collectionName := c.Params("collectionName")
	// id := c.Params("id")

	// // Special handling for collections that require rule checking and stock ledger updates
	// var children []ChildRule
	// switch collectionName {
	// case "purchase":
	// 	children = []ChildRule{
	// 		{Collection: "invoice_details", ForeignKey: "purchase_id"},
	// 		{Collection: "productions", ForeignKey: "purchase_id"},
	// 		{Collection: "sale", ForeignKey: "purchase_id"},
	// 		{Collection: "job_work", ForeignKey: "purchase_id"},
	// 	}
	// case "sale":
	// 	children = []ChildRule{
	// 		{Collection: "invoice_details", ForeignKey: "sale_id"},
	// 	}
	// case "job_work":
	// 	children = []ChildRule{
	// 		{Collection: "invoice_details", ForeignKey: "job_id"},
	// 	}
	// case "productions":
	// 	children = []ChildRule{
	// 		{Collection: "kernel_inventory", ForeignKey: "production_id"},
	// 	}
	// case "kernel_inventory":
	// 	// Add dependencies if any, e.g., sales or dispatches
	// }

	// // Check if deletion is allowed
	// if len(children) > 0 {
	// 	msg, err := CheckDeleteRule(org.Id, collectionName, id, children)
	// 	if err != nil {
	// 		return shared.BadRequest(err.Error())
	// 	}
	// 	if msg != "can_delete" {
	// 		return shared.BadRequest(msg)
	// 	}
	// }

	// // Update Stock Ledger for specific collections
	// if collectionName == "purchase" || collectionName == "sale" || collectionName == "job_work" || collectionName == "productions" || collectionName == "kernel_inventory" || collectionName == "invoice_details" {
	// 	userToken := utils.GetUserTokenValue(c)
	// 	err := DeleteLedgerEntryByRefID(org.Id, id, userToken.UserId)
	// 	if err != nil {
	// 		return shared.InternalServerError("Failed to update stock ledger: " + err.Error())
	// 	}
	// }
	// if c.Params("collectionName") != "data_model" && c.Params("collectionName") != "model_config" {
	// 	inputData := bson.M{
	// 		"$set": bson.M{
	// 			"is_delete":  true,
	// 			"deleted_by": userToken.UserId,
	// 		},
	// 	}

	// 	// Update document with is_delete and deleted_by fields
	// 	_, err := database.GetConnection(org.Id).Collection(c.Params("collectionName")).UpdateOne(context.TODO(), filter, inputData)
	// 	if err != nil {
	// 		return err
	// 	}
	// } else {
	// 	// Delete document from "data_model" or "model_config" collections
	// 	_, err := database.GetConnection(org.Id).Collection(c.Params("collectionName")).DeleteOne(context.TODO(), filter)
	// 	if err != nil {
	// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Error deleting document"})
	// 	}
	// }

	if c.Params("collectionName") == "organization" || c.Params("collectionName") == "master_menu" || c.Params("collectionName") == "role_acl" {
		org.Id = "shared"
	}

	if c.Params("collectionName") == "equipments" {
		pipeline := bson.A{
			bson.D{{"$match", bson.D{{"_id", c.Params("id")}}}},
			bson.D{
				{"$lookup",
					bson.D{
						{"from", "maintance_details"},
						{"localField", "_id"},
						{"foreignField", "equipment_id"},
						{"as", "result"},
					},
				},
			},
			bson.D{{"$addFields", bson.D{{"ids", "$result._id"}}}},
		}

		equipemntData, err := helper.GetAggregateQueryResult(org.Id, "equipments", pipeline)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Error deleting maintance_details",
				"err":     err.Error(),
			})
		}

		equipData := equipemntData[0]
		getIds := equipData["ids"].(primitive.A)

		filter := bson.M{
			"_id": bson.M{
				"$in": getIds,
			},
		}
		parentFilter := bson.M{
			"parent_id": bson.M{
				"$in": getIds,
			},
		}

		_, err = database.GetConnection(org.Id).
			Collection("maintance_details").
			DeleteMany(context.TODO(), filter)

		_, err = database.GetConnection(org.Id).
			Collection("equipment_maintenance_data").
			DeleteMany(context.TODO(), parentFilter)
		if err != nil {
			// handle error
		}
	}

	// if c.Params("collectionName") == "equipments" {
	// 	docID := c.Params("id")
	// 	cursor, err := database.GetConnection(org.Id).Collection("maintance_details").Find(context.TODO(), bson.M{"equipment_id": docID})
	// 	if err != nil {
	// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Error fetching maintance_details"})
	// 	}
	// 	defer cursor.Close(context.TODO())

	// 	var maintenanceDocs []bson.M
	// 	if err := cursor.All(context.TODO(), &maintenanceDocs); err != nil {
	// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Error parsing maintance_details"})
	// 	}
	// 	for _, doc := range maintenanceDocs {
	// 		// Extract _id from maintenance_detail
	// 		if maintenanceID, ok := doc["_id"].(primitive.ObjectID); ok {
	// 			_, err := database.GetConnection(org.Id).Collection("equipment_maintenance_data").DeleteMany(context.TODO(), bson.M{
	// 				"parent_id": maintenanceID, // or maintenanceID if stored as ObjectID
	// 			})
	// 			if err != nil {
	// 				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 					"message": "Error deleting equipment_maintenance_data",
	// 				})
	// 			}
	// 		}
	// 	}

	// 	_, err = database.GetConnection(org.Id).Collection("maintance_details").DeleteMany(context.TODO(), bson.M{"equipment_id": docID})
	// 	if err != nil {
	// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 			"message": "Error deleting maintance_details",
	// 		})
	// 	}
	// }

	if c.Params("collectionName") == "maintance_details" {
		docID := c.Params("id")
		_, err := database.GetConnection(org.Id).Collection("equipment_maintenance_data").DeleteMany(context.TODO(), bson.M{
			"parent_id": docID, // or maintenanceID if stored as ObjectID
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": "Error deleting equipment_maintenance_data",
			})
		}
	}
	if c.Params("collectionName") == "sale" {
		docID := c.Params("id")
		userToken := utils.GetUserTokenValue(c)

		cursor, err := database.GetConnection(org.Id).Collection("sold_products_info").Find(
			context.Background(),
			bson.M{"template_id": docID},
		)
		if err == nil {
			defer cursor.Close(context.Background())

			var soldProducts []map[string]interface{}
			if err := cursor.All(context.Background(), &soldProducts); err == nil {
				for _, soldProduct := range soldProducts {
					soldProductID, _ := soldProduct["_id"].(string)

					UpdateSerials(soldProductID, org.Id, userToken.UserId, "delete")
					// var tinGridData []interface{}
					// if gridData, ok := soldProduct["tin_grid_data"].(primitive.A); ok {
					// 	tinGridData = []interface{}(gridData)
					// } else if gridData, ok := soldProduct["tin_grid_data"].([]interface{}); ok {
					// 	tinGridData = gridData
					// }

					// if len(tinGridData) > 0 {
					// 	for i := 0; i < len(tinGridData); i++ {
					// 		if ledgerRefId, ok := tinGridData[i].(map[string]interface{})["ledger_ref_id"].(string); ok && ledgerRefId != "" {
					// 			if err := DeleteLedgerEntry(org.Id, ledgerRefId, userToken.UserId); err != nil {
					// 				log.Printf("WARNING: Failed to delete ledger entry %s for sold_products_info %s: %v", ledgerRefId, docID, err)
					// 			}
					// 		} else {
					// 			log.Printf("WARNING: No ledger_ref_id found for sold_products_info %s, skipping ledger deletion", docID)
					// 		}
					// 	}
					// }

					updatePackingID(soldProductID, org.Id, userToken.UserId)
				}

				database.GetConnection(org.Id).Collection("sold_products_info").DeleteMany(
					context.Background(),
					bson.M{"template_id": docID},
				)
			}
		}
	}

	if c.Params("collectionName") == "sold_products_info" {
		docID := c.Params("id")
		userToken := utils.GetUserTokenValue(c)

		// soldProduct, err := GetDataById(org.Id, docID, "sold_products_info")
		// if err == nil {
		// 	UpdateSerials(docID, org.Id, userToken.UserId, "delete")
		// 	// var tinGridData []interface{}
		// 	// if gridData, ok := soldProduct["tin_grid_data"].(primitive.A); ok {
		// 	// 	tinGridData = []interface{}(gridData)
		// 	// } else if gridData, ok := soldProduct["tin_grid_data"].([]interface{}); ok {
		// 	// 	tinGridData = gridData
		// 	// }

		// 	// if len(tinGridData) > 0 {
		// 	// 	for i := 0; i < len(tinGridData); i++ {
		// 	// 		if ledgerRefId, ok := tinGridData[i].(map[string]interface{})["ledger_ref_id"].(string); ok && ledgerRefId != "" {
		// 	// 			if err := DeleteLedgerEntry(org.Id, ledgerRefId, userToken.UserId); err != nil {
		// 	// 				log.Printf("WARNING: Failed to delete ledger entry %s for sold_products_info %s: %v", ledgerRefId, docID, err)
		// 	// 			}
		// 	// 		} else {
		// 	// 			log.Printf("WARNING: No ledger_ref_id found for sold_products_info %s, skipping ledger deletion", docID)
		// 	// 		}
		// 	// 	}
		// 	// }
		// } else {
		// 	log.Printf("WARNING: Could not fetch sold_products_info %s before deletion: %v", docID, err)
		// }

		if err := updatePackingID(docID, org.Id, userToken.UserId); err != nil {
			log.Printf("WARNING: Failed to update packing ID for sold_products_info %s: %v", docID, err)
		}
	}
	if c.Params("collectionName") == "kernal_purchase_data" {
		db := database.GetConnection(org.Id)
		filter := bson.M{
			"kernal_purchase_id": c.Params("id"),
		}
		// correct call
		go helper.DeleteByCollAndFilter(db.Collection("kernel_inventory"), filter)
	}
	if c.Params("collectionName") == "purchase" && c.Params("type") == "kernel" {
		// && type= ""
		db := database.GetConnection(org.Id)
		filter := bson.M{
			"purchase_template_id": c.Params("id"),
		}
		// correct call
		go helper.DeleteByCollAndFilter(db.Collection("kernal_purchase_data"), filter)
		go helper.DeleteByCollAndFilter(db.Collection("kernel_inventory"), filter)
	}

	// Handle production stock deletion
	if c.Params("collectionName") == "productions" {
		productionID := c.Params("id")
		userToken := utils.GetUserTokenValue(c)

		// Get production to check process_type
		var production map[string]interface{}
		err := database.GetConnection(org.Id).Collection("productions").
			FindOne(context.Background(), bson.M{"_id": productionID}).Decode(&production)

		if err == nil && production["process_type"] != "PACK" {
			if err := DeleteProductionStock(org.Id, productionID, userToken.UserId); err != nil {
				log.Printf("ERROR: Failed to delete production stock: %v", err)
				// Don't fail the deletion, just log the error
			} else {
				log.Printf("SUCCESS: Production stock deleted for production: %s", productionID)
			}
		}
	}

	// Delete document from "data_model" or "model_config" collections
	_, err := database.GetConnection(org.Id).Collection(c.Params("collectionName")).DeleteOne(context.TODO(), filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Error deleting document"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Document successfully deleted"})
}

func updatePackingID(PId string, orgid string, userid string) error {
	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", PId}}}},
	}

	productionData, err := helper.GetAggregateQueryResult(orgid, "sold_products_info", pipeline)
	if err != nil {
		return shared.BadRequest(fmt.Sprintf("failed to fetch production data: %v", err))
	}

	if len(productionData) == 0 {
		return shared.BadRequest("no production data found for given PId")
	}

	productionProcess := productionData[0]

	// ✅ Safely check batch_no
	rawBatch, ok := productionProcess["batch_no"]
	if !ok {
		return shared.BadRequest("batch_no not found in production data")
	}

	inputData, ok := rawBatch.([]interface{})
	if !ok {
		return shared.BadRequest("batch_no is not in expected format (array)")
	}

	if len(inputData) == 0 {
		return shared.BadRequest("no items found in batch_no")
	}

	for _, raw := range inputData {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue // skip if not a map
		}

		var productionId string
		if val, exists := item["production_id"]; exists {
			if s, ok := val.(string); ok {
				productionId = s
			}
		}

		if productionId == "" {
			continue // skip if production_id missing
		}

		update := bson.M{
			"$set": bson.M{
				"serials.$.status":      "packed",
				"serials.$.date":        time.Now(),
				"serials.$.template_id": "",
			},
		}

		filter := bson.M{
			"_id":          productionId,
			"serials.s_no": item["s_no"],
		}

		item["update_on"] = time.Now()
		item["update_by"] = userid

		_, err := database.GetConnection(orgid).
			Collection("productions").
			UpdateOne(ctx, filter, update)

		if err != nil {
			return shared.BadRequest(err.Error())
		}
	}

	return nil
}

func DeleteByAll(c *fiber.Ctx) error {
	//Get the orgId from Header
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collectionName")

	filter := bson.M{}
	_, err := database.GetConnection(org.Id).Collection(collectionName).DeleteMany(ctx, filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Error deleting documents"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Documents successfully deleted"})
}

func getDocByIddHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collectionName")
	projectid := c.Params("projectid")
	// module Collection
	filter := bson.A{
		bson.D{{"$match", bson.D{{"project_id", projectid}}}},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "task"},
					{"localField", "moduleid"},
					{"foreignField", "moduleid"},
					{"as", "results"},
				},
			},
		},
		bson.D{
			{"$project",
				bson.D{
					{"_id", 1},
					{"moduleid", 1},
					{"parentmodulename", 1},
					{"modulename", 1},
					{"enddate", 1},
					{"project_id", 1},
					{"startdate", 1},
					{"task_name", "$results.task_name"},
				},
			},
		},
	}

	response, err := helper.GetAggregateQueryResult(org.Id, collectionName, filter)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, response)
}

func getDocByClientIdHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}

	var filter bson.M
	collectionName := c.Params("collectionName")
	clientname := c.Params("clientname")
	decodedProjectName, err := url.QueryUnescape(clientname)
	if err != nil {
		// fmt.Println("Error decoding:", err)
	}
	client := strings.Replace(decodedProjectName, "%20", " ", -1)
	// fmt.Println("Decoded Client Name:", client)
	if collectionName == "testcase" {
		filter = bson.M{"moduleid": client}
	} else {
		filter = bson.M{"clientname": client}

	}

	response, err := helper.GetQueryResult(org.Id, collectionName, filter, int64(0), int64(50000), nil)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	return shared.SuccessResponse(c, response)
}

// getDocsHandler --METHOD get the data from Db with pagination
func getDocsHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	//	userToken := utils.GetUserTokenValue(c)

	// collectionName := c.Params("collectionName")
	var requestBody helper.PaginationRequest

	if err := c.BodyParser(&requestBody); err != nil {
		return nil
	}

	var pipeline []primitive.M
	pipeline = helper.MasterAggregationPipeline(requestBody, c)
	// if userToken.UserRole != "SA" {
	// 	OrgPipeline := helper.GenerateOrgIdFilter(userToken.OrgId)
	// 	pipeline = append(pipeline, OrgPipeline)
	// 	//fmt.Println(OrgPipeline)
	// }
	if len(requestBody.MultiFieldSearchFilter) > 0 {
		var orConditions []bson.M

		for _, filter := range requestBody.MultiFieldSearchFilter {
			// if filter.Operator == "CONTAINS" {
			// 	orConditions = append(orConditions, bson.M{
			// 		filter.Column: bson.M{
			// 			"$regex":   filter.Value.(string),
			// 			"$options": "i",
			// 		},
			// 	})
			// }
			if filter.Operator == "CONTAINS" {
				prefix, ok := filter.Value.(string)
				if !ok {
					continue
				}
				orConditions = append(orConditions, bson.M{
					filter.Column: bson.M{
						"$regex":   "^" + regexp.QuoteMeta(prefix),
						"$options": "i", // case-insensitive
					},
				})
			}

		}

		if len(orConditions) > 0 {

			pipeline = append(pipeline, bson.M{
				"$match": bson.M{"$or": orConditions},
			})

		}
	}
	if len(requestBody.Sort) > 0 {
		sortConditions := helper.BuildSortConditions(requestBody.Sort)
		pipeline = append(pipeline, sortConditions)
	}

	PagiantionPipeline := helper.PagiantionPipeline(requestBody.Start, requestBody.End)
	pipeline = append(pipeline, PagiantionPipeline)

	originHeader := strings.ToLower(c.Get("Origin"))
	refererHeader := strings.ToLower(c.Get("Referer"))
	isOnboarding := strings.Contains(originHeader, "onboarding") || strings.Contains(refererHeader, "onboarding")

	if (isOnboarding && c.Params("collectionName") == "questions") || c.Params("collectionName") == "organization" || c.Params("collectionName") == "user_type" || c.Params("collectionName") == "db_config" || c.Params("collectionName") == "master_menu" || c.Params("collectionName") == "role_acl" {
		org.Id = "shared"
	}

	// if c.Params("collectionName") == "organization" || c.Params("collectionName") == "role_acl" || c.Params("collectionName") == "user" || c.Params("collectionName") == "db_config" {
	// 	org.Id = "shared"
	// }

	OrgId := org.Id
	if requestBody.FromAnotherOrg {
		OrgId = requestBody.OrgId
	}
	Response, err := helper.GetAggregateQueryResult(OrgId, c.Params("collectionName"), pipeline)
	if err != nil {
		if cmdErr, ok := err.(mongo.CommandError); ok {
			return shared.BadRequest(cmdErr.Message)
		}
	}
	return shared.SuccessResponse(c, Response)

}

// OnboardingProcessing  -- METHOD Onboarding processing for user and send the email
func OnboardingProcessing(orgId, email, emailtype, category string) error {
	// Generate the 'decoding' value (replace this with your actual logic)
	decoding := helper.Generateuniquekey()

	filter := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"title", category},
					{"emailtype", emailtype},
				},
			},
		},
	}

	Response, err := helper.GetAggregateQueryResult(orgId, "email_template", filter)
	if err != nil {
		fmt.Println("Err",
			err.Error(),
		)

	}

	if err := helper.SimpleEmailHandler(email, os.Getenv("CLIENT_EMAIL"), "Welcome to pms Onboarding", replacestring(Response[0]["template"].(string), fmt.Sprintf("%s%s%s", Response[0]["link"].(string), `=`, decoding))); err == nil {
		// If email sending was successful
		if err := UsertemporaryStoringData(orgId, email, decoding); err != nil {
			log.Println("Failed to insert user junked files:", err)
		}
	} else {
		return shared.BadRequest("Email sending failed:")
	}

	return nil
}

func replacestring(template, Replacement string) string {

	return strings.ReplaceAll(template, `{{link}}`, Replacement)
}

// USER ON BOARDING TEMPLATE  //todo
func createOnBoardtemplate(link string) string {

	body := `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Welcome to Our Onboarding Process</title>
	</head>
	<body>
		<table cellpadding="0" cellspacing="0" width="100%" bgcolor="#f0f0f0">
			<tr>
				<td align="center">
					<table cellpadding="0" cellspacing="0" width="600" style="border-collapse: collapse;">
						<tr>
							<td align="center" bgcolor="#ffffff" style="padding: 40px 0 30px 0; border-top: 3px solid #007BFF;">
								<h1>Welcome to Our Onboarding Process</h1>
								<p>Thank you for choosing our services. We are excited to have you on board!</p>
								<p>Please follow the steps below to get started:</p>
								<ol>
									<div>Step 1: Complete your profile</div>
									<div>Step 2: Explore our platform</div>
									<div>Step 3: Contact our support team if you have any questions</div>
								</ol>
								<p>Enjoy your journey with us!</p>
								<p>
								<a href="{{link}}" style="background-color: #007BFF; color: #fff; padding: 10px 20px; text-decoration: none; display: inline-block; border-radius: 5px;">Activation Now</a>
								</p>
							</td>
						</tr>
					</table>
				</td>
			</tr>
		</table>
	</body>
	</html>`

	return body
}

// + link +
func UsertemporaryStoringData(orgid, requestMail, appToken string) error {

	requestData := bson.M{
		"_id":        requestMail,
		"access_key": appToken,
		"expire_on":  time.Now(),
	}

	_, err := database.GetConnection(orgid).Collection("temporary_user").InsertOne(ctx, requestData)

	if err != nil {
		// Log the detailed error for debugging
		log.Println("Failed to insert data into the database:", err.Error())
		return shared.BadRequest("Failed to insert data into the database")
	}

	return nil
}

func getFileDetails(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}
	fileCategory := c.Params("folder")
	refId := c.Params("refId")
	//	token := shared.GetUserTokenValue(c)
	query := bson.M{"ref_id": refId, "folder": fileCategory}

	response, err := helper.GetQueryResult(org.Id, "user_files", query, int64(0), int64(200), nil)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, response)
}

func getAllFileDetails(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	fileCategory := c.Params("category")
	//status := c.Params("status")
	page := c.Params("page")
	limit := c.Params("limit")
	query := bson.M{"category": fileCategory}
	response, err := helper.GetQueryResult(org.Id, "user_files", query, helper.Page(page), helper.Limit(limit), nil)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	return shared.SuccessResponse(c, response)
}

func GetLotHistoryFlag(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	if org.Id == "" {
		return shared.BadRequest("Organization Id missing")
	}

	currentdateTime := time.Now()

	// var pipeline = bson.A{
	// 	bson.D{{"$match", bson.D{{"factory_id", c.Params("factory_id")}}}},
	// 	bson.D{{"$match", bson.D{{"type", "Stock Transfer"}}}},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"as", "origin"},
	// 				{"foreignField", "country_code"},
	// 				{"from", "origin"},
	// 				{"localField", "origin_id"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$unwind",
	// 			bson.D{
	// 				{"path", "$origin"},
	// 				{"preserveNullAndEmptyArrays", true},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$project",
	// 			bson.D{
	// 				{"avaliability", 1},
	// 				{"factory_id", 1},
	// 				{"origin._id", 1},
	// 				{"origin.country_code", 1},
	// 				{"origin.description", 1},
	// 				{"origin.name", 1},
	// 				{"origin_id", 1},
	// 				{"purchase_date", 1},
	// 				{"purchase_id", 1},
	// 				{"remaining_weight", 1},
	// 				{"weight", 1},
	// 			},
	// 		},
	// 	},

	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "productions"},
	// 				{"localField", "origin_id"},
	// 				{"foreignField", "origin_id"},
	// 				{"as", "lot_history"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$addFields",
	// 			bson.D{
	// 				{"items",
	// 					bson.D{
	// 						{"$filter",
	// 							bson.D{
	// 								{"input", "$lot_history"},
	// 								{"as", "item"},
	// 								{"cond",
	// 									bson.D{
	// 										{"$and",
	// 											bson.A{
	// 												bson.D{
	// 													{"$gte",
	// 														bson.A{
	// 															"$$item.process_start_date_time",
	// 															time.Date(currentdateTime.Year(), currentdateTime.Month(), currentdateTime.Day(), 0, 0, 0, 0, time.UTC),
	// 														},
	// 													},
	// 												},
	// 												bson.D{
	// 													{"$lte",
	// 														bson.A{
	// 															"$$item.process_start_date_time",
	// 															time.Date(currentdateTime.Year(), currentdateTime.Month(), currentdateTime.Day(), 23, 59, 59, 0, time.UTC),
	// 														},
	// 													},
	// 												},
	// 											},
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// 	bson.D{{"$unset", "lot_history"}},
	// }
	var pipeline = bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"quality_reports.net_weight", bson.D{{"$exists", true}}},
					{"status_type",
						bson.D{
							{"$in",
								bson.A{
									"Cargo Arrived",
									"Contract Signed",
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
					{"from", "sale"},
					{"localField", "_id"},
					{"foreignField", "purchase_id"},
					{"as", "sale_result"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "lots"},
					{"localField", "_id"},
					{"foreignField", "purchase_id"},
					{"as", "lot_result"},
				},
			},
		},
		bson.D{
			{"$addFields",
				bson.D{
					{"total_sold", bson.D{{"$sum", "$sale_result.quantity"}}},
					{"total_lots", bson.D{{"$sum", "$lot_result.weight"}}},
					{"lot_numbers", "$lot_result._id"},
				},
			},
		},
		bson.D{
			{"$set",
				bson.D{
					{"quality_reports.net_weight",
						bson.D{
							{"$subtract",
								bson.A{
									"$quality_reports.net_weight",
									bson.D{
										{"$ifNull",
											bson.A{
												"$total_lots",
												0,
											},
										},
									},
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
					{"from", "origin"},
					{"localField", "country_origin"},
					{"foreignField", "country_code"},
					{"as", "origin"},
				},
			},
		},
		bson.D{
			{"$unwind",
				bson.D{
					{"path", "$origin"},
					{"preserveNullAndEmptyArrays", true},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "company"},
					{"let", bson.D{{"wId", "$warehouse"}}},
					{"pipeline",
						bson.A{
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$and",
													bson.A{
														bson.D{
															{"$eq",
																bson.A{
																	"$$wId",
																	"$_id",
																},
															},
														},
														bson.D{
															{"$eq",
																bson.A{
																	"$inside_factory",
																	true,
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					{"as", "warehouse_result"},
				},
			},
		},
		bson.D{
			{"$set",
				bson.D{
					{"factory_id",
						bson.D{
							{"$arrayElemAt",
								bson.A{
									"$warehouse_result.factory_id",
									0,
								},
							},
						},
					},
					{"origin_id", "$country_origin"},
					{"purchase_id", "$_id"},
					{"purchase_date", "$dop"},
					{"remaining_weight",
						bson.D{
							{"$cond",
								bson.D{
									{"if",
										bson.D{
											{"$eq",
												bson.A{
													"$purchasetype",
													"domestic",
												},
											},
										},
									},
									{"then",
										bson.D{
											{"$subtract",
												bson.A{
													"$quality_reports.net_weight",
													bson.D{
														{"$ifNull",
															bson.A{
																"$total_sold",
																0,
															},
														},
													},
												},
											},
										},
									},
									{"else", "$total_sold"},
								},
							},
						},
					},
					{"weight",
						bson.D{
							{"$cond",
								bson.D{
									{"if",
										bson.D{
											{"$gte",
												bson.A{
													"$purchasetype",
													"international",
												},
											},
										},
									},
									{"then", "$total_sold"},
									{"else", "$quality_reports.net_weight"},
								},
							},
						},
					},
				},
			},
		},
		bson.D{{"$set", bson.D{{"availability", "$remaining_weight"}}}},
		bson.D{{"$match", bson.D{{"factory_id", c.Params("factory_id")}}}},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "productions"},
					{"localField", "lot_numbers"},
					{"foreignField", "lot_number"},
					{"as", "lot_history"},
				},
			},
		},
		bson.D{
			{"$addFields",
				bson.D{
					{"items",
						bson.D{
							{"$filter",
								bson.D{
									{"input", "$lot_history"},
									{"as", "item"},
									{"cond",
										bson.D{
											{"$and",
												bson.A{
													bson.D{
														{"$gte",
															bson.A{
																"$$item.process_start_date_time",
																time.Date(currentdateTime.Year(), currentdateTime.Month(), currentdateTime.Day(), 0, 0, 0, 0, time.UTC),
															},
														},
													},
													bson.D{
														{"$lte",
															bson.A{
																"$$item.process_start_date_time",
																time.Date(currentdateTime.Year(), currentdateTime.Month(), currentdateTime.Day(), 23, 59, 59, 0, time.UTC),
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// var pipeline1 = bson.A{
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "company"},
	// 				{"localField", "warehouse_to"},
	// 				{"foreignField", "_id"},
	// 				{"as", "warehouse_result"},
	// 			},
	// 		},
	// 	}, bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "lots"},
	// 				{"localField", "purchase_id"},
	// 				{"foreignField", "purchase_id"},
	// 				{"as", "lot_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$addFields",
	// 			bson.D{
	// 				{"total_lots", bson.D{{"$sum", "$lot_result.weight"}}},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$set",
	// 			bson.D{
	// 				{"factory_id",
	// 					bson.D{
	// 						{"$arrayElemAt",
	// 							bson.A{
	// 								"$warehouse_result.factory_id",
	// 								0,
	// 							},
	// 						},
	// 					},
	// 				}, {"remaining_weight",
	// 					bson.D{
	// 						{"$subtract",
	// 							bson.A{
	// 								"$weight",
	// 								"$total_lots",
	// 							},
	// 						},
	// 					},
	// 				},
	// 				{"origin_id", "$country_origin"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$match",
	// 			bson.D{
	// 				{"factory_id", c.Params("factory_id")},
	// 				{"transfer_type", "WareHouse to WareHouse"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "purchase"},
	// 				{"localField", "purchase_id"},
	// 				{"foreignField", "_id"},
	// 				{"as", "purchase_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$unwind",
	// 			bson.D{
	// 				{"path", "$purchase_result"},
	// 				{"preserveNullAndEmptyArrays", true},
	// 			},
	// 		},
	// 	},
	// 	bson.D{{"$set", bson.D{
	// 		{"origin_id", "$purchase_result.country_origin"},
	// 	}}},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "origin"},
	// 				{"localField", "origin_id"},
	// 				{"foreignField", "country_code"},
	// 				{"as", "origin"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$unwind",
	// 			bson.D{
	// 				{"path", "$origin"},
	// 				{"preserveNullAndEmptyArrays", true},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "productions"},
	// 				{"localField", "origin_id"},
	// 				{"foreignField", "origin_id"},
	// 				{"as", "lot_history"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$addFields",
	// 			bson.D{
	// 				{"items",
	// 					bson.D{
	// 						{"$filter",
	// 							bson.D{
	// 								{"input", "$lot_history"},
	// 								{"as", "item"},
	// 								{"cond",
	// 									bson.D{
	// 										{"$and",
	// 											bson.A{
	// 												bson.D{
	// 													{"$gte",
	// 														bson.A{
	// 															"$$item.process_start_date_time",
	// 															time.Date(currentdateTime.Year(), currentdateTime.Month(), currentdateTime.Day(), 0, 0, 0, 0, time.UTC),
	// 														},
	// 													},
	// 												},
	// 												bson.D{
	// 													{"$lte",
	// 														bson.A{
	// 															"$$item.process_start_date_time",
	// 															time.Date(currentdateTime.Year(), currentdateTime.Month(), currentdateTime.Day(), 23, 59, 59, 0, time.UTC),
	// 														},
	// 													},
	// 												},
	// 											},
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// }
	fmt.Println(pipeline)
	response, err := helper.GetAggregateQueryResult(org.Id, "purchase", pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	// response1, err := helper.GetAggregateQueryResult(org.Id, "sale", pipeline1)
	// if err != nil {
	// 	return shared.BadRequest(err.Error())
	// }

	for _, row := range response {
		items, ok := row["items"].(primitive.A)
		if !ok {
			row["already_lot_exited"] = false
		} else {
			row["already_lot_exited"] = len(items) > 0
		}
		delete(row, "items")
	}

	// for _, row := range response1 {
	// 	items, ok := row["items"].(primitive.A)
	// 	if !ok {
	// 		row["already_lot_exited"] = false
	// 	} else {
	// 		row["already_lot_exited"] = len(items) > 0
	// 	}
	// 	delete(row, "items")
	// }
	// response = append(response, response1...)

	return shared.SuccessResponse(c, response)

}

func GetLotHistoryCount(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	if org.Id == "" {
		return shared.BadRequest("Organization Id missing")
	}

	start_date := c.Params("start_date")
	st, _ := time.Parse(time.RFC3339, start_date)

	end_date := c.Params("end_date")
	et, _ := time.Parse(time.RFC3339, end_date)
	var pipeline = bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"equipment_id",
						bson.D{
							{"$in",
								bson.A{
									"ASM",
									"MSM",
									"SOU",
									"SOUL",
								},
							},
						},
					},
				},
			},
		},
		bson.D{
			{"$set",
				bson.D{
					{"formatted_activity_date",
						bson.D{
							{"$dateToString",
								bson.D{
									{"date", "$process_start_date_time"},
									{"format", "%d-%m-%Y"},
								},
							},
						},
					},
				},
			},
		},
		bson.D{
			{"$match",
				bson.D{
					{"process_start_date_time",
						bson.D{
							{"$gte", time.Date(st.Year(), st.Month(), st.Day(), 0, 0, 0, 0, time.UTC)},
							{"$lte", time.Date(et.Year(), et.Month(), et.Day(), 23, 59, 59, 0, time.UTC)},
						},
					},
				},
			},
		},

		bson.D{
			{"$lookup",
				bson.D{
					{"from", "factory"},
					{"localField", "factory_id"},
					{"foreignField", "_id"},
					{"as", "fac_result"},
				},
			},
		},
		bson.D{
			{"$set",
				bson.D{
					{"org_id",
						bson.D{
							{"$arrayElemAt",
								bson.A{
									"$fac_result.org_id",
									0,
								},
							},
						},
					},
				},
			},
		},
		bson.D{{"$match", bson.D{{"org_id", org.Id}}}},
		bson.D{
			{"$group",
				bson.D{
					{"_id", bson.D{{"_id", "$equipment_id"}, {"process_start_date_time", "$formatted_activity_date"}}},
					{"worker_count", bson.D{{"$sum", 1}}},
					{"pieces_cashew_total", bson.D{{"$sum", "$pieces"}}},
					{"rejected_cashew_total", bson.D{{"$sum", "$rejected"}}},
					{"whole_cashew_total",
						bson.D{
							{"$sum",
								bson.D{
									{"$cond",
										bson.D{
											{"if",
												bson.D{
													{"$eq",
														bson.A{
															"$equipment_id",
															"ASM",
														},
													},
												},
											},
											{"then", "$output_weight"},
											{"else", "$wholes"},
										},
									},
								},
							},
						},
					},
					{"nw", bson.D{{"$sum", "$nw"}}},
					{"shell", bson.D{{"$sum", "$shell"}}},
					{"scooping_line", bson.D{{"$sum", "$uncut"}}},
					// {"activity_start_date_time", bson.D{{"$first", "$activity_start_date_time"}}},
					{"shelling_type", bson.D{{"$first", "$shelling_type"}}},
					{"process_type", bson.D{{"$first", "$process_type"}}},
				},
			},
		},
		bson.D{
			{"$project",
				bson.D{
					{"worker_count", 1},
					{"uncut", "$scooping_line"},
					{"Pieces", "$pieces_cashew_total"},
					{"Rejected", "$rejected_cashew_total"},
					{"Wholes", "$whole_cashew_total"},
					{"process_start_date_time", bson.D{{"$toDate", "$_id.process_start_date_time"}}},
					{"total_count",
						bson.D{
							{"$add",
								bson.A{
									"$pieces_cashew_total",
									"$rejected_cashew_total",
									"$whole_cashew_total",
								},
							},
						},
					},
					{"_id", "$_id._id"},
					{"shell", "$shell"},
					{"shelling_type", 1},
					{"process_type", 1},
				},
			},
		},
	}

	response, err := helper.GetAggregateQueryResult(org.Id, "productions", pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, response)

}

func CloneAndInsertData(c *fiber.Ctx) error {

	org, errFound := helper.GetOrg(c)
	if !errFound {
		return shared.BadRequest("Organization Id missing")
	}

	orgId := org.Id

	start := time.Now()

	dataMap, errmsg := helper.InsertValidateInDatamodel("organisation", string(c.Body()), orgId)

	// var errmsgs []string
	if errmsg != nil {
		// for _, values := range errmsg {
		// 	errmsgs = append(errmsgs, values)
		// }
		// var inputData map[string]interface{}
		// if err := c.BodyParser(&inputData); err != nil {
		// 	return c.Status(fiber.StatusBadRequest).SendString("Error parsing request body")
		// }

		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"message": errmsg})

	}

	// Define the aggregation pipeline to match and set data

	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"org_type", dataMap["org_type"]},
					// {"acl", bson.D{{"$ne", "N"}}}, // !undone
				},
			},
		},
		bson.D{{"$unset", "_id"}},
		bson.D{{"$set", bson.D{{"org_id", dataMap["_id"]}}}},
	}

	//check the filter to return the data
	orgDataArray, err := helper.GetAggregateQueryResult(orgId, "org_type_data_acl", pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	_, err = database.GetConnection(orgId).Collection("organisation").InsertOne(ctx, dataMap)
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database: " + err.Error())
	}

	//result Came from org_type_data_acl collection
	_, err = database.GetConnection(orgId).Collection("org_data_acl").InsertOne(ctx, orgDataArray[0])
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database: " + err.Error())
	}

	//Once organisation create by default inisde the role collection
	var names = fmt.Sprintf("AD-%s", dataMap["_id"])

	var RolecollectionData = map[string]interface{}{
		"org_id": dataMap["_id"],
		"_id":    names,
		"status": "A",
		"name":   "Admin",
	}

	//todo inbuild struct
	_, err = database.GetConnection(orgId).Collection("role").InsertOne(ctx, RolecollectionData)
	if err != nil {

	}

	filter :=
		bson.A{
			bson.D{
				{"$lookup",
					bson.D{
						{"from", "org_data_acl"},
						{"localField", "org_id"},
						{"foreignField", "org_id"},
						{"as", "result"},
					},
				},
			},
			bson.D{{"$unwind", bson.D{{"path", "$result"}}}},
			bson.D{{"$set", bson.D{{"result.role", "$_id"}}}},
			bson.D{{"$replaceRoot", bson.D{{"newRoot", "$result"}}}},
			bson.D{{"$unset", "_id"}},
			bson.D{{"$match", bson.D{{"acl", bson.D{{"$ne", "N"}}}}}}, //only role
		}

	roleDataArray, err := helper.GetAggregateQueryResult(orgId, "role", filter)

	if err != nil {
		return shared.BadRequest(err.Error())
	}

	_, err = database.GetConnection(orgId).Collection("role_data_acl").InsertOne(ctx, roleDataArray[0])
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database: " + err.Error())
	}

	fmt.Println("End Time", time.Since(start))

	return shared.SuccessResponse(c, "Successfully Data Added")

}

func Clonedatabasedrolecollection(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collectionName") //role collection

	var inputData map[string]interface{}
	if err := c.BodyParser(&inputData); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Error parsing request body")
	}

	_, err := database.GetConnection(org.Id).Collection(collectionName).InsertOne(ctx, inputData) //first insert the data in the role collection
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database: " + err.Error())
	}

	filter :=
		bson.A{
			bson.D{
				{"$lookup",
					bson.D{
						{"from", "org_data_acl"},
						{"localField", "org_id"},
						{"foreignField", "org_id"},
						{"as", "result"},
					},
				},
			},
			bson.D{{"$unwind", bson.D{{"path", "$result"}}}},
			bson.D{{"$set", bson.D{{"result.role", "$_id"}}}},
			bson.D{{"$replaceRoot", bson.D{{"newRoot", "$result"}}}},
			bson.D{{"$unset", "_id"}},
			bson.D{{"$match", bson.D{{"acl", bson.D{{"$ne", "N"}}}}}}, //only role
		}

	data, err := helper.GetAggregateQueryResult(org.Id, collectionName, filter)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	_, err = database.GetConnection(org.Id).Collection("role_data_acl").InsertOne(ctx, data[0])
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database: " + err.Error())
	}

	return shared.SuccessResponse(c, "Successfully Data Added")
}

func GetPurchaseDetailsWithInwards(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	if org.Id == "" {
		return shared.BadRequest("Organization Id missing")
	}

	inwarddomesticresponse, err := helper.GetAggregateQueryResult(org.Id, "inwarddomestic", bson.A{})
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	for _, row := range inwarddomesticresponse {
		row["type"] = "inwarddomestic"
	}

	inwardinternationalresponse, err := helper.GetAggregateQueryResult(org.Id, "purchase", bson.A{})
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	for _, row := range inwardinternationalresponse {
		row["type"] = "purchase"
	}

	var data []interface{}
	for _, row := range inwarddomesticresponse {
		data = append(data, row)
	}

	for _, row := range inwardinternationalresponse {
		data = append(data, row)
	}

	return shared.SuccessResponse(c, data)
}

func GetPurchaseDetailsWithSalesAndFactoryInwards(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	if org.Id == "" {
		return shared.BadRequest("Organization Id missing")
	}

	collectionNmae := c.Params("types")

	Id := c.Params("id")

	var pipeline = bson.A{
		bson.D{{"$match", bson.D{{"_id", Id}}}},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "sale"},
					{"localField", "_id"},
					{"foreignField", "purchase_id"},
					{"as", "sale"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "factoryinwards"},
					{"localField", "_id"},
					{"foreignField", "purchase_id"},
					{"as", "factoryinwards"},
				},
			},
		},
	}

	res, err := helper.GetAggregateQueryResult(org.Id, collectionNmae, pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, res)
}

func PdfGenerator(c *fiber.Ctx) error {

	// var pipeline = bson.A{
	// 	bson.D{{"$match", bson.D{{"_id", "PURCH-INT-2024-028"}}}},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "origin"},
	// 				{"localField", "country_origin"},
	// 				{"foreignField", "country_code"},
	// 				{"as", "origin_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "lots"},
	// 				{"let", bson.D{{"pid", "$_id"}}},
	// 				{"pipeline",
	// 					bson.A{
	// 						bson.D{
	// 							{"$match",
	// 								bson.D{
	// 									{"$expr",
	// 										bson.D{
	// 											{"$eq",
	// 												bson.A{
	// 													"$purchase_id",
	// 													"$$pid",
	// 												},
	// 											},
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 						bson.D{
	// 							{"$set",
	// 								bson.D{
	// 									{"Date",
	// 										bson.D{
	// 											{"$dateToString",
	// 												bson.D{
	// 													{"date", "$lot_start_date_time"},
	// 													{"format", "%d-%m-%Y"},
	// 												},
	// 											},
	// 										},
	// 									},
	// 									{"Lot#", "$_id"},
	// 									{"Weight", "$weight"},
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 				{"as", "lot_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "customer"},
	// 				{"localField", "company_name"},
	// 				{"foreignField", "_id"},
	// 				{"as", "supplier_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "company"},
	// 				{"localField", "warehouse"},
	// 				{"foreignField", "_id"},
	// 				{"as", "warehouse_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$lookup",
	// 			bson.D{
	// 				{"from", "sale"},
	// 				{"let", bson.D{{"pid", "$_id"}}},
	// 				{"pipeline",
	// 					bson.A{
	// 						bson.D{
	// 							{"$match",
	// 								bson.D{
	// 									{"$expr",
	// 										bson.D{
	// 											{"$eq",
	// 												bson.A{
	// 													"$purchase_id",
	// 													"$$pid",
	// 												},
	// 											},
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 						bson.D{
	// 							{"$match",
	// 								bson.D{
	// 									{"$expr",
	// 										bson.D{
	// 											{"$eq",
	// 												bson.A{
	// 													"$type",
	// 													"Sale",
	// 												},
	// 											},
	// 										},
	// 									},
	// 								},
	// 							},
	// 						},
	// 						bson.D{
	// 							{"$lookup",
	// 								bson.D{
	// 									{"from", "customer"},
	// 									{"let", bson.D{{"cName", "$customer_name"}}},
	// 									{"pipeline",
	// 										bson.A{
	// 											bson.D{
	// 												{"$match",
	// 													bson.D{
	// 														{"$expr",
	// 															bson.D{
	// 																{"$eq",
	// 																	bson.A{
	// 																		"$$cName",
	// 																		"$_id",
	// 																	},
	// 																},
	// 															},
	// 														},
	// 													},
	// 												},
	// 											},
	// 										},
	// 									},
	// 									{"as", "customer_result"},
	// 								},
	// 							},
	// 						},
	// 						bson.D{
	// 							{"$set",
	// 								bson.D{
	// 									{"Date",
	// 										bson.D{
	// 											{"$dateToString",
	// 												bson.D{
	// 													{"date", "$created_on"},
	// 													{"format", "%d-%m-%Y"},
	// 												},
	// 											},
	// 										},
	// 									},
	// 									{"Sale Type", "$type"},
	// 									{"Buyer",
	// 										bson.D{
	// 											{"$arrayElemAt",
	// 												bson.A{
	// 													"$customer_result.customer_name",
	// 													0,
	// 												},
	// 											},
	// 										},
	// 									},
	// 									{"Rate", "$price"},
	// 									{"Amount", "$total_price"},
	// 								},
	// 							},
	// 						},
	// 					},
	// 				},
	// 				{"as", "sale_result"},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$addFields",
	// 			bson.D{
	// 				{"total_sold", bson.D{{"$sum", "$sale_result.quantity"}}},
	// 				{"warehouse_result1",
	// 					bson.D{
	// 						{"Warehouse",
	// 							bson.D{
	// 								{"$arrayElemAt",
	// 									bson.A{
	// 										"$warehouse_result.name",
	// 										0,
	// 									},
	// 								},
	// 							},
	// 						},
	// 						{"Available Stock", "$quality_reports.net_weight"},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// 	bson.D{
	// 		{"$set",
	// 			bson.D{
	// 				{"origin_name",
	// 					bson.D{
	// 						{"$arrayElemAt",
	// 							bson.A{
	// 								"$origin_result.name",
	// 								0,
	// 							},
	// 						},
	// 					},
	// 				},
	// 				{"purchase_price", bson.D{{"$toString", "$purchase_price"}}},
	// 				{"supplier_name",
	// 					bson.D{
	// 						{"$arrayElemAt",
	// 							bson.A{
	// 								"$supplier_result.customer_name",
	// 								0,
	// 							},
	// 						},
	// 					},
	// 				},
	// 				{"purchased_date",
	// 					bson.D{
	// 						{"$dateToString",
	// 							bson.D{
	// 								{"date", "$created_on"},
	// 								{"format", "%d-%m-%Y"},
	// 							},
	// 						},
	// 					},
	// 				},
	// 				{"quality_reports.net_weight",
	// 					bson.D{
	// 						{"$subtract",
	// 							bson.A{
	// 								"$quality_reports.net_weight",
	// 								"$total_sold",
	// 							},
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// }

	PurchaseId := c.Params("purchaseId")
	startDate := c.Params("start_date")
	endDate := c.Params("end_date")

	st, _ := time.Parse(time.RFC3339, startDate)
	et, _ := time.Parse(time.RFC3339, endDate)

	var pipeline = bson.A{
		bson.D{{"$match", bson.D{{"_id", PurchaseId}}}},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "origin"},
					{"localField", "country_origin"},
					{"foreignField", "country_code"},
					{"as", "origin_result"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "lots"},
					{"let", bson.D{{"pid", "$_id"}}},
					{"pipeline",
						bson.A{
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$and",
													bson.A{
														bson.D{
															{"$eq",
																bson.A{
																	"$purchase_id",
																	"$$pid",
																},
															},
														},
														bson.D{
															{"$gt",
																bson.A{
																	"$lot_start_date_time",
																	st,
																},
															},
														},
														bson.D{
															{"$lt",
																bson.A{
																	"$lot_start_date_time",
																	et,
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							bson.D{
								{"$set",
									bson.D{
										{"Date",
											bson.D{
												{"$dateToString",
													bson.D{
														{"date", "$lot_start_date_time"},
														{"format", "%d-%m-%Y"},
													},
												},
											},
										},
										{"Lot#", "$_id"},
										{"Weight", "$weight"},
									},
								},
							},
						},
					},
					{"as", "lot_result"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "customer"},
					{"localField", "company_name"},
					{"foreignField", "_id"},
					{"as", "supplier_result"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "company"},
					{"localField", "warehouse"},
					{"foreignField", "_id"},
					{"as", "warehouse_result"},
				},
			},
		},
		bson.D{
			{"$addFields",
				bson.D{
					{"lot_numbers", "$lot_result._id"},
					{"final_product", "Final Product"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "product"},
					{"localField", "final_product"},
					{"foreignField", "type"},
					{"pipeline",
						bson.A{
							bson.D{
								{"$project",
									bson.D{
										{"id",
											bson.D{
												{"$toLower",
													bson.D{
														{"$replaceAll",
															bson.D{
																{"input", "$_id"},
																{"find", " "},
																{"replacement", "_"},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							bson.D{
								{"$set",
									bson.D{
										{"findKey",
											bson.D{
												{"$concat",
													bson.A{
														bson.D{{"$literal", "$$production"}},
														".",
														"$id",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					{"as", "finalResult"},
				},
			},
		},
		bson.D{{"$addFields", bson.D{{"final_products", "$finalResult.findKey"}}}},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "productions"},
					{"localField", "lot_numbers"},
					{"foreignField", "lot_number"},
					{"pipeline",
						bson.A{
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$and",
													bson.A{
														bson.D{
															{"$ne",
																bson.A{
																	"$lot_number",
																	primitive.Null{},
																},
															},
														},
														bson.D{
															{"$eq",
																bson.A{
																	"$factory_id",
																	"FAC0001",
																},
															},
														},
														bson.D{
															{"$in",
																bson.A{
																	"$process_type",
																	bson.A{
																		"MANG",
																		"MANC",
																	},
																},
															},
														},
														bson.D{
															{"$gt",
																bson.A{
																	"$lot_start_date_time",
																	st,
																},
															},
														},
														bson.D{
															{"$lt",
																bson.A{
																	"$lot_start_date_time",
																	et,
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					{"as", "productions_result"},
				},
			},
		},
		bson.D{
			{"$addFields",
				bson.D{
					{"grade",
						bson.A{
							bson.D{
								{"Grade", "White Wholes"},
								{"Weight", bson.D{{"$sum", "$productions_result.white_wholes"}}},
							},
							bson.D{
								{"Grade", "SW Wholes"},
								{"Weight", bson.D{{"$sum", "$productions_result.sw_wholes"}}},
							},
							bson.D{
								{"Grade", "PKW"},
								{"Weight", bson.D{{"$sum", "$productions_result.pkw"}}},
							},
							bson.D{
								{"Grade", "Buds"},
								{"Weight", bson.D{{"$sum", "$productions_result.buds"}}},
							},
							bson.D{
								{"Grade", "Unpeeled Wholes"},
								{"Weight", bson.D{{"$sum", "$productions_result.unpeeled_wholes"}}},
							},
							bson.D{
								{"Grade", "Splits"},
								{"Weight", bson.D{{"$sum", "$productions_result.splits"}}},
							},
							bson.D{
								{"Grade", "Rejection"},
								{"Weight", bson.D{{"$sum", "$productions_result.rejection"}}},
							},
							bson.D{
								{"Grade", "OW"},
								{"Weight", bson.D{{"$sum", "$productions_result.ow"}}},
							},
							bson.D{
								{"Grade", "DW"},
								{"Weight", bson.D{{"$sum", "$productions_result.dw"}}},
							},
							bson.D{
								{"Grade", "White Rejection"},
								{"Weight", bson.D{{"$sum", "$productions_result.white_rejection"}}},
							},
							bson.D{
								{"Grade", "NW"},
								{"Weight", bson.D{{"$sum", "$productions_result.nw"}}},
							},
							bson.D{
								{"Grade", "PKP"},
								{"Weight", bson.D{{"$sum", "$productions_result.pkp"}}},
							},
							bson.D{
								{"Grade", "SPS"},
								{"Weight", bson.D{{"$sum", "$productions_result.sps"}}},
							},
							bson.D{
								{"Grade", "Unpeeled Pieces"},
								{"Weight", bson.D{{"$sum", "$productions_result.unpeeled_pieces"}}},
							},
							bson.D{
								{"Grade", "Oil Pices"},
								{"Weight", bson.D{{"$sum", "$productions_result.oil_pices"}}},
							},
							bson.D{
								{"Grade", "UPP"},
								{"Weight", bson.D{{"$sum", "$productions_result.upp"}}},
							},
							bson.D{
								{"Grade", "Husk Powder"},
								{"Weight", bson.D{{"$sum", "$productions_result.husk_powder"}}},
							},
							bson.D{
								{"Grade", "PKP2"},
								{"Weight", bson.D{{"$sum", "$productions_result.pkp2"}}},
							},
							bson.D{
								{"Grade", "W180"},
								{"Weight", bson.D{{"$sum", "$productions_result.w180"}}},
							},
							bson.D{
								{"Grade", "W210"},
								{"Weight", bson.D{{"$sum", "$productions_result.w210"}}},
							},
							bson.D{
								{"Grade", "W320"},
								{"Weight", bson.D{{"$sum", "$productions_result.w320"}}},
							},
							bson.D{
								{"Grade", "W450"},
								{"Weight", bson.D{{"$sum", "$productions_result.w450"}}},
							},
							bson.D{
								{"Grade", "SW180"},
								{"Weight", bson.D{{"$sum", "$productions_result.sw180"}}},
							},
							bson.D{
								{"Grade", "SW240"},
								{"Weight", bson.D{{"$sum", "$productions_result.sw240"}}},
							},
							bson.D{
								{"Grade", "SW360"},
								{"Weight", bson.D{{"$sum", "$productions_result.sw360"}}},
							},
							bson.D{
								{"Grade", "SSW"},
								{"Weight", bson.D{{"$sum", "$productions_result.ssw"}}},
							},
							bson.D{
								{"Grade", "Testa Unpeeled"},
								{"Weight", bson.D{{"$sum", "$productions_result.testa_unpeeled"}}},
							},
							bson.D{
								{"Grade", "JH"},
								{"Weight", bson.D{{"$sum", "$productions_result.jh"}}},
							},
							bson.D{
								{"Grade", "S"},
								{"Weight", bson.D{{"$sum", "$productions_result.s"}}},
							},
							bson.D{
								{"Grade", "SS"},
								{"Weight", bson.D{{"$sum", "$productions_result.ss"}}},
							},
							bson.D{
								{"Grade", "K"},
								{"Weight", bson.D{{"$sum", "$productions_result.k"}}},
							},
							bson.D{
								{"Grade", "LWP"},
								{"Weight", bson.D{{"$sum", "$productions_result.lwp"}}},
							},
							bson.D{
								{"Grade", "SWP"},
								{"Weight", bson.D{{"$sum", "$productions_result.swp"}}},
							},
							bson.D{
								{"Grade", "W240"},
								{"Weight", bson.D{{"$sum", "$productions_result.w240"}}},
							},
							bson.D{
								{"Grade", "PKW2"},
								{"Weight", bson.D{{"$sum", "$productions_result.pkw2"}}},
							},
							bson.D{
								{"Grade", "W400"},
								{"Weight", bson.D{{"$sum", "$productions_result.w400"}}},
							},
							bson.D{
								{"Grade", "SW400"},
								{"Weight", bson.D{{"$sum", "$productions_result.sw400"}}},
							},
							bson.D{
								{"Grade", "Mixed Kernels"},
								{"Weight", bson.D{{"$sum", "$productions_result.mixed_kernels"}}},
							},
							bson.D{
								{"Grade", "BB"},
								{"Weight", bson.D{{"$sum", "$productions_result.bb"}}},
							},
							bson.D{
								{"Grade", "Husk"},
								{"Weight", bson.D{{"$sum", "$husk_total"}}},
							},
						},
					},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "sale"},
					{"let", bson.D{{"pid", "$_id"}}},
					{"pipeline",
						bson.A{
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$eq",
													bson.A{
														"$purchase_id",
														"$$pid",
													},
												},
											},
										},
									},
								},
							},
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$and",
													bson.A{
														bson.D{
															{"$gt",
																bson.A{
																	"$created_on",
																	st,
																},
															},
														},
														bson.D{
															{"$lt",
																bson.A{
																	"$created_on",
																	et,
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							bson.D{
								{"$match",
									bson.D{
										{"$expr",
											bson.D{
												{"$eq",
													bson.A{
														"$type",
														"Sale",
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
										{"from", "customer"},
										{"let", bson.D{{"cName", "$customer_name"}}},
										{"pipeline",
											bson.A{
												bson.D{
													{"$match",
														bson.D{
															{"$expr",
																bson.D{
																	{"$eq",
																		bson.A{
																			"$$cName",
																			"$_id",
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
										{"as", "customer_result"},
									},
								},
							},
							bson.D{
								{"$set",
									bson.D{
										{"Date",
											bson.D{
												{"$dateToString",
													bson.D{
														{"date", "$created_on"},
														{"format", "%d-%m-%Y"},
													},
												},
											},
										},
										{"Sale Type", "$type"},
										{"Buyer",
											bson.D{
												{"$arrayElemAt",
													bson.A{
														"$customer_result.customer_name",
														0,
													},
												},
											},
										},
										{"Rate", "$price"},
										{"Weight", "$quantity"},
										{"Amount", "$total_price"},
									},
								},
							},
						},
					},
					{"as", "sale_result"},
				},
			},
		},
		bson.D{
			{"$addFields",
				bson.D{
					{"total_sold", bson.D{{"$sum", "$sale_result.quantity"}}},
					{"total_sale_obj",
						bson.D{
							{"Weight", bson.D{{"$sum", "$sale_result.quantity"}}},
							{"Buyer", "Total"},
							{"Amount", bson.D{{"$sum", "$sale_result.total_price"}}},
							{"Date", ""},
							{"Sale Type", ""},
						},
					},
					{"total_lot_obj",
						bson.D{
							{"Date", ""},
							{"Lot#", "Total"},
							{"Weight", bson.D{{"$sum", "$lot_result.weight"}}},
						},
					},
					{"warehouse_result1",
						bson.D{
							{"Warehouse",
								bson.D{
									{"$arrayElemAt",
										bson.A{
											"$warehouse_result.name",
											0,
										},
									},
								},
							},
							{"Available Stock", "$quality_reports.net_weight"},
						},
					},
				},
			},
		},
		bson.D{
			{"$addFields",
				bson.D{
					{"sale_result",
						bson.D{
							{"$concatArrays",
								bson.A{
									"$sale_result",
									bson.A{
										bson.D{{"$mergeObjects", "$total_sale_obj"}},
									},
								},
							},
						},
					},
					{"lot_result",
						bson.D{
							{"$concatArrays",
								bson.A{
									"$lot_result",
									bson.A{
										bson.D{{"$mergeObjects", "$total_lot_obj"}},
									},
								},
							},
						},
					},
				},
			},
		},
		bson.D{
			{"$set",
				bson.D{
					{"origin_name",
						bson.D{
							{"$arrayElemAt",
								bson.A{
									"$origin_result.name",
									0,
								},
							},
						},
					},
					{"purchase_price", bson.D{{"$toString", "$purchase_price"}}},
					{"supplier_name",
						bson.D{
							{"$arrayElemAt",
								bson.A{
									"$supplier_result.customer_name",
									0,
								},
							},
						},
					},
					{"purchased_date",
						bson.D{
							{"$dateToString",
								bson.D{
									{"date", "$created_on"},
									{"format", "%d-%m-%Y"},
								},
							},
						},
					},
					{"quality_reports.net_weight",
						bson.D{
							{"$subtract",
								bson.A{
									"$quality_reports.net_weight",
									"$total_sold",
								},
							},
						},
					},
				},
			},
		},
	}

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	if org.Id == "" {
		return shared.BadRequest("Organization Id missing")
	}
	userToken := utils.GetUserTokenValue(c)
	response, err := helper.GetAggregateQueryResult(org.Id, "purchase", pipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	data, err := helper.GenerateSaleReport(response[0], org, userToken)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, data)

}

func ProductionDataUpdate(c *fiber.Ctx) error {
	var req map[string]interface{}
	var status string
	var doubleProcess bool
	var cycleOne bool
	var updatedRes *mongo.UpdateResult

	err := c.BodyParser(&req)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	updateId := c.Params("Id")
	helper.UpdateDateObject(req)
	userToken := utils.GetUserTokenValue(c)
	status = req["status"].(string)
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	if req["process_type"] == "PACK" {
		var productionData map[string]interface{}
		err := database.GetConnection(org.Id).Collection("productions").FindOne(
			context.Background(),
			bson.M{"_id": updateId},
		).Decode(&productionData)
		if err != nil {
			return shared.InternalServerError(err.Error())
		}
		var newSerialData []map[string]interface{}
		// Cast production serials to primitive.A
		serials, ok := productionData["serials"].(primitive.A)
		if !ok {
			return shared.InternalServerError(fmt.Sprintf("Failed to retrieve serials from production data. Type: %T", productionData["serials"]))
		}

		startSerialNo := helper.InterfaceToInt64(req["start_serial_no"])
		endSerialNo := helper.InterfaceToInt64(req["end_serial_no"])

		// Create a map for fast lookup of existing serial numbers in serials
		//	serialMap := make(map[int64]int)
		// for index, item := range serials {
		// 	// Convert item to map[string]interface{} for easier field access
		// 	if data, ok := item.(map[string]interface{}); ok {
		// 		if sNo, ok := data["s_no"].(int64); ok {
		// 			serialMap[sNo] = index
		// 		}
		// 	}
		// }

		// Iterate over the serial number range to update or add entries
		for i := startSerialNo; i <= endSerialNo; i++ {
			serialData := map[string]interface{}{
				"s_no":   i,
				"status": "packed",
			}
			var dataExists bool
			var data map[string]interface{}
			for _, item := range serials {
				mapItem := item.(map[string]interface{})
				sno := mapItem["s_no"].(int64)
				status := mapItem["status"].(string)
				if sno == i && status != "packed" {
					data = mapItem
					dataExists = true
				}
			}
			if dataExists {
				newSerialData = append(newSerialData, data)
			} else {
				newSerialData = append(newSerialData, serialData)
			}

			// if index, exists := serialMap[i]; exists {
			// 	// Update existing entry in serials only if the status is not already "packed"
			// 	if existingData, ok := serials[index].(map[string]interface{}); ok && existingData["status"] != "packed" {
			// 		serials[index] = serialData
			// 	}
			// } else {
			// 	// Append new entry to serials if it doesn't exist
			// 	serials = append(serials, serialData)
			// }
		}

		// Update production data with modified serials array
		req["serials"] = newSerialData
	}

	if req["process_type"] == "BOR" {
		if req["status"] == "Pause" {
			req["cycle_one_process_end_date_time"] = time.Now()
		} else if req["status"] == "Resume" {
			req["cycle_two_process_start_date_time"] = time.Now()
		}
		bormaProcess := req["borma_process"].(string)
		if bormaProcess == "double" {
			doubleProcess = true
			if req["cycle_one"] != nil {
				cycleOne = req["cycle_one"].(bool)
			}
		}
	}

	if status != "Completed" {
		filter := bson.M{"_id": updateId}
		updateData := bson.M{"$set": req}

		updatedRes, err = database.GetConnection(org.Id).Collection("productions").UpdateOne(context.Background(), filter, updateData)
		if err != nil {
			return shared.InternalServerError(err.Error())
		}
	}

	if status == "Completed" {
		// err := processSectionStocks(req, org.Id)
		// if err != nil {
		// 	return shared.InternalServerError(err.Error())
		// }
		if doubleProcess {
			if !cycleOne {
				req["cycle_two_process_end_date_time"] = time.Now()
			}
		}
		filter := bson.M{"_id": updateId}
		req["process_end_date_time"] = time.Now()
		updateData := bson.M{"$set": req}
		updatedRes, err = database.GetConnection(org.Id).Collection("productions").UpdateOne(context.Background(), filter, updateData)
		if err != nil {
			return err
		}

		// Update stock for non-PACK processes on completion
		// This handles output products that were 0 on creation but now have values
		if req["process_type"] != "PACK" {
			fmt.Printf("INFO: Updating stock for completed production: %s, process: %s\n", updateId, req["process_type"])
			err := PutProductionStock(org.Id, updateId, userToken.UserId, req)
			if err != nil {
				log.Printf("ERROR: Failed to update production stock on completion: %v", err)
				// Don't fail the completion, just log the error
			} else {
				log.Printf("SUCCESS: Stock updated for completed production: %s", updateId)
			}
		}
	}

	if req["process_type"] == "PACK" {
		ProcessKernelSTockInUpdate(org.Id, req, userToken.UserId)
	}

	return shared.SuccessResponse(c, updatedRes)
}

func processLotData1(inputData map[string]interface{}, orgID string) error {

	processId := helper.ToInt32(inputData["process_id"])

	factoryId := inputData["factory_id"].(string)

	// lotNumber, ok := inputData["lot_numbers"].(primitive.A)
	// if !ok {
	// 	return fiber.NewError(500, "invalid lot_number type")
	// }

	// Correcting the filter for FindOne
	filter := bson.D{{Key: "_id", Value: processId}}
	var mapData map[string]interface{}
	err := database.GetConnection(orgID).Collection("process_map").FindOne(context.Background(), filter).Decode(&mapData)
	if err != nil {
		return err
	}

	valueFromEveryIndividual := mapData["value_from_every_individual"].(bool)

	// Correct type assertion for fields
	processLotData, ok := mapData["fields"].(primitive.A)
	if !ok {
		// fmt.Printf("Type of process_id: %T\n", processLotData)
		return fiber.NewError(500, "invalid fields type in mapData")
	}

	incrementData := bson.M{}
	decrementData := bson.M{}
	var totalWeight float64

	for _, obj := range processLotData {

		objMap, ok := obj.(map[string]interface{})
		if !ok {
			return fiber.NewError(500, "invalid field data")
		}

		getFieldName, ok := objMap["field_name"].(string)
		if !ok {
			return fiber.NewError(500, "invalid input_weight field type")
		}

		setFieldName, ok := objMap["db_name"].(string)
		if !ok {
			return fiber.NewError(500, "invalid db_name field type")
		}

		getValue, ok := inputData[getFieldName].(float64)
		if !ok {
			return fiber.NewError(500, fmt.Sprintf("invalid type for field %s", getFieldName))
		}

		totalWeight += getValue

		incrementData[setFieldName] = getValue

		if processId > 0 {
			if valueFromEveryIndividual {
				decrementData[setFieldName] = getValue * -1
			}
		}

	}

	decrementData["remaining_weight"] = totalWeight * -1
	incrementData["remaining_weight"] = totalWeight

	updateData := bson.M{
		"$inc": incrementData,
		"$set": bson.M{
			"factory_id": factoryId,
			"process_id": processId,
		},
	}

	updateFilter := bson.M{
		"factory_id": factoryId,
		"process_id": processId,
	}

	// Using Upsert option
	upsert := true
	opts := options.Update().SetUpsert(upsert)
	_, err = database.GetConnection(orgID).Collection("section_stocks").UpdateOne(context.Background(), updateFilter, updateData, opts)
	if err != nil {
		return err
	}

	if processId > 0 {
		decUpdateData := bson.M{
			"$inc": decrementData,
		}
		decUpdateFilter := bson.M{
			"factory_id": factoryId,
			"process_id": processId - 1,
		}
		// Using Upsert option
		upsert := true
		opts := options.Update().SetUpsert(upsert)
		_, err = database.GetConnection(orgID).Collection("section_stocks").UpdateOne(context.Background(), decUpdateFilter, decUpdateData, opts)
		if err != nil {
			return err
		}
	}

	return nil
}

func processSectionStocks(inputData map[string]interface{}, orgID string) error {

	processId := helper.ToInt32(inputData["process_id"])
	factoryId := inputData["factory_id"].(string)
	processType := inputData["process_type"].(string)

	updateFilter := bson.M{
		"factory_id":   factoryId,
		"process_id":   processId,
		"process_type": processType,
	}

	err, updateData := GetUpdateQueryForSectionStocks(inputData, orgID)
	if err != nil {
		return err
	}

	upsert := true
	opts := options.Update().SetUpsert(upsert)
	_, err = database.GetConnection(orgID).Collection("section_stocks").UpdateOne(context.Background(), updateFilter, updateData, opts)
	if err != nil {
		return err
	}

	return nil
}

func GetUpdateQueryForSectionStocks(processData map[string]interface{}, orgID string) (error, bson.M) {

	incrementData := bson.M{}
	decrementData := bson.M{}
	UpdateData := bson.M{}
	var mapData map[string]interface{}
	var totalWeight float64
	var updateQuery primitive.M
	var nextProcessId int32
	processId := helper.ToInt32(processData["process_id"])
	factoryId := processData["factory_id"].(string)
	processType := processData["process_type"].(string)
	//factoryId :=processData["process_type"].(string)
	lotNumbers, ok := processData["lot_number"].([]interface{})
	if !ok {
		fmt.Printf("Type of process_id: %T\n", lotNumbers)
		return fiber.NewError(500, "invalid fields type in lot Number "), nil
	}

	filter := bson.D{{Key: "process_id", Value: processId}, {Key: "process_type", Value: processType}}

	err := database.GetConnection(orgID).Collection("process_map").FindOne(context.Background(), filter).Decode(&mapData)
	if err != nil {
		return err, nil
	}

	// cookIngFilter := bson.M{
	// 	"factory_id":   factoryId,
	// 	"process_id":   processId,
	// 	"process_type": processType,
	// }

	//valueFromEveryIndividual := mapData["value_from_every_individual"].(bool)

	// Correct type assertion for fields

	processLotData, ok := mapData["fields"].(primitive.A)
	if !ok {
		fmt.Printf("Type of process_id: %T\n", processLotData)
		return fiber.NewError(500, "invalid fields type in mapData"), nil
	}

	nextProcessId, ok = mapData["next_process_id"].(int32)
	if !ok {

		return fiber.NewError(500, "invalid db_name field type"), nil
	}

	for _, obj := range processLotData {

		objMap, ok := obj.(map[string]interface{})
		if !ok {
			return fiber.NewError(500, "invalid field data"), nil
		}

		getFieldName, ok := objMap["field_name"].(string)
		if !ok {
			return fiber.NewError(500, "invalid input_weight field type"), nil
		}

		getValueFromIndividual := objMap["increment_data"].(bool)
		setFieldName, ok := objMap["db_name"].(string)
		if !ok {
			return fiber.NewError(500, "invalid db_name field type"), nil
		}

		getValue, ok := processData[getFieldName].(float64)
		if !ok {
			continue
		}

		if getValueFromIndividual {
			incrementData[setFieldName] = getValue
			totalWeight += getValue
		} else {
			UpdateData[setFieldName] = getValue
		}

		// if processId > 0 {
		// 	if getValueFromIndividual {
		// 		decrementData[setFieldName] = getValue * -1
		// 	}
		// }

	}

	incrementData["total_input_weight"] = totalWeight
	incrementData["total_remaining_weight"] = totalWeight
	UpdateData["next_process_id"] = nextProcessId

	if processId == 0 {
		lotNumber := lotNumbers[0]
		cookingWeight := processData["input_weight"].(float64)
		LotDataFilter := bson.D{{Key: "_id", Value: lotNumber}}
		var LotData map[string]interface{}

		err := database.GetConnection(orgID).Collection("lots").FindOne(context.Background(), LotDataFilter).Decode(&LotData)
		if err != nil {
			return err, nil
		}
		saleId := LotData["sale_id"].(string)
		lotWeight := LotData["weight"].(float64)

		saleDataFilter := bson.D{{Key: "_id", Value: saleId}}
		var saleData map[string]interface{}

		err = database.GetConnection(orgID).Collection("sale").FindOne(context.Background(), saleDataFilter).Decode(&saleData)
		if err != nil {
			return err, nil
		}

		totalWeight := saleData["weight"].(float64)

		var parentSectionStock map[string]interface{}

		parentSectionStockFilter := bson.D{{Key: "process_id", Value: processId}, {Key: "sale_id", Value: saleId}, {Key: "process_type", Value: processType}, {Key: "factory_id", Value: factoryId}}
		err = database.GetConnection(orgID).Collection("section_stocks").FindOne(context.Background(), parentSectionStockFilter).Decode(&parentSectionStock)
		if err != nil {
			if lotWeight+cookingWeight >= totalWeight {
				UpdateData["status"] = "Completed"
			}
			UpdateData["sale_id"] = saleId
			id := uuid.New().String()
			UpdateData["section_id"] = id
		} else {
			if lotWeight+cookingWeight >= totalWeight {
				UpdateData["status"] = "Completed"

			}
		}

	}
	if processId > 0 {
		decrementData["total_remaining_weight"] = totalWeight * -1
		decUpdateData := bson.M{
			"$inc": decrementData,
		}
		decUpdateFilter := bson.M{
			"factory_id": factoryId,
			"process_id": processId - 1,
		}

		parentSectionStockFilter := bson.D{{Key: "process_id", Value: processId}, {Key: "process_type", Value: processType}, {Key: "factory_id", Value: factoryId}}
		var parentSectionStock map[string]interface{}

		err := database.GetConnection(orgID).Collection("section_stocks").FindOne(context.Background(), parentSectionStockFilter).Decode(&parentSectionStock)
		if err != nil {
			return err, nil
		}
		sectionId := parentSectionStock["section_id"].(string)
		UpdateData["parent_id"] = sectionId
		// Using Upsert option
		upsert := true
		opts := options.Update().SetUpsert(upsert)
		_, err = database.GetConnection(orgID).Collection("section_stocks").UpdateOne(context.Background(), decUpdateFilter, decUpdateData, opts)
		if err != nil {
			return err, nil
		}

	}
	updateQuery = bson.M{
		"$inc": incrementData,
		"$set": UpdateData,
	}

	return nil, updateQuery
}

func StockReadjustmentApi(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	invPipe := bson.A{
		bson.D{{"$match", bson.D{{"purchase_id", "041PURCH-DOM-2025-021"}}}},
	}
	invoiceData, err := helper.GetAggregateQueryResult(org.Id, "invoice_details", invPipe)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	for _, invoice := range invoiceData {
		invoiceID := invoice["_id"].(string)
		err := PurchaseLedgerUpdate(nil, "domestic", org, userToken.UserId, invoiceID, "invoice_details")
		if err != nil {
			continue
		}
	}

	return shared.SuccessResponse(c, "Stock readjustment completed successfully")
}

func StockReadjustmentApiForCooking(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	productPipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"process_id", 1},
					{"purchase_id", "041PURCH-DOM-2025-021"},
				},
			},
		},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "invoice_details"},
					{"localField", "purchase_id"},
					{"foreignField", "purchase_id"},
					{"as", "result"},
				},
			},
		},
		bson.D{{"$set", bson.D{{"invSize", bson.D{{"$size", "$result"}}}}}},
		bson.D{{"$match", bson.D{{"invSize", bson.D{{"$gte", 1}}}}}},
	}
	invoiceData, err := helper.GetAggregateQueryResult(org.Id, "productions", productPipeline)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	for _, invoice := range invoiceData {

		origin := ""
		if purchaseId, ok := invoice["purchase_id"].(string); ok {
			if purchase, err := GetDataById(org.Id, purchaseId, "purchase"); err == nil {
				if countryOrigin, ok := purchase["country_origin"].(string); ok {
					origin = countryOrigin
				} else {
					continue
				}
			} else {
				continue
			}
		} else {
			continue
		}
		cokData := map[string]interface{}{
			"filled_tins":    invoice["input_weight"],
			"purchase_id":    invoice["purchase_id"],
			"country_origin": origin,
			"factory_id":     invoice["factory_id"],
			"_id":            invoice["_id"],
			"product_id":     "RCN",
			"process_type":   "COOK",
		}
		ProcessRCNCooking(org.Id, cokData, userToken.UserId)

	}

	return shared.SuccessResponse(c, "Stock readjustment completed successfully")
}

func StreamData(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	modelName := c.Params("modelName")
	type_ := c.Params("type")

	var collectionName string
	var dataset string

	if type_ != "" {
		dataset = modelName
	} else {
		collectionName = modelName
	}

	var datasetConfig map[string]interface{}
	if dataset != "" {
		err := database.GetConnection(org.Id).Collection("dataset_config").FindOne(c.Context(), bson.M{"_id": dataset}).Decode(&datasetConfig)
		if err != nil {
			return shared.BadRequest("Dataset not found")
		}
		collectionName = datasetConfig["dataSetBaseCollection"].(string)
	}

	var requestBody helper.PaginationRequest
	if err := c.QueryParser(&requestBody); err != nil {
		return shared.BadRequest("Invalid query parameters")
	}

	// Parse filter from query parameter
	filterStr := c.Query("filter")
	fmt.Printf("Filter string: %s\n", filterStr)
	var dateRangeFilter bson.M
	if filterStr != "" {
		var filters []map[string]interface{}
		if err := json.Unmarshal([]byte(filterStr), &filters); err == nil && len(filters) > 0 {
			// Handle date range filters
			for _, filter := range filters {
				if conditions, ok := filter["conditions"].([]interface{}); ok {
					for _, cond := range conditions {
						if condition, ok := cond.(map[string]interface{}); ok {
							column := condition["column"].(string)
							operator := condition["operator"].(string)
							value := condition["value"]

							if column == "created_on" {
								if dateRangeFilter == nil {
									dateRangeFilter = bson.M{}
								}
								if dateStr, ok := value.(string); ok {
									if parsedDate, err := time.Parse(time.RFC3339, dateStr); err == nil {
										switch operator {
										case "GREATERTHANOREQUAL":
											dateRangeFilter["$gte"] = parsedDate
										case "LESSTHANOREQUAL":
											dateRangeFilter["$lte"] = parsedDate
										}
									}
								}
							} else {
								filterParam := helper.FilterParam{
									ParamsName:  column,
									Paramsvalue: value,
								}
								requestBody.FilterParam = append(requestBody.FilterParam, filterParam)
							}
						}
					}
				}
			}
		}
	}
	fmt.Printf("FilterParam count: %d, datasetConfig nil: %t\n", len(requestBody.FilterParam), datasetConfig == nil)
	// Build base aggregation pipeline
	var pipeline bson.A
	if dataset != "" {
		if pipelineStr, ok := datasetConfig["pipeline"].(string); ok {
			err := json.Unmarshal([]byte(pipelineStr), &pipeline)
			if err != nil {
				return shared.BadRequest("Invalid pipeline format")
			}
		} else {
			pipeline = datasetConfig["pipeline"].(bson.A)
		}
	}

	matchFilter := bson.M{}
	if len(requestBody.FilterParam) > 0 {
		for _, filterParam := range requestBody.FilterParam {
			if filterParam.Paramsvalue != nil && filterParam.Paramsvalue != "" {
				matchFilter[filterParam.ParamsName] = filterParam.Paramsvalue
			}
		}
	}
	if dateRangeFilter != nil {
		matchFilter["created_on"] = dateRangeFilter
	}
	if len(matchFilter) > 0 {
		pipeline = append(pipeline, bson.M{"$match": matchFilter})
	}
	fmt.Printf("Config pipeline: %+v\n", pipeline)

	// Build a filter for counting (if your helper doesn’t already include match conditions)
	filter := bson.M{}
	if len(pipeline) > 0 {
		if firstStage, ok := pipeline[0].(bson.M); ok {
			if matchStage, ok := firstStage["$match"]; ok {
				filter = matchStage.(bson.M)
			}
		}
	}

	db := database.GetConnection(org.Id)
	ctx := context.Background()
	total, err := db.Collection(collectionName).CountDocuments(ctx, filter)
	if err != nil {
		return shared.InternalServerError("Failed to count documents")
	}

	batchSize := 100
	c.Status(fiber.StatusOK).Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {

		defer w.Flush()

		for offset := 0; offset < int(total); offset += batchSize {
			var p bson.A = pipeline

			// Add pagination stages to your pipeline
			paginationStage := map[string]interface{}{"$skip": offset}
			limitStage := map[string]interface{}{"$limit": batchSize}

			p = append(p, paginationStage)
			p = append(p, limitStage)

			fmt.Printf("Final pipeline: %+v\n", p)
			cursor, err := db.Collection(collectionName).Aggregate(ctx, p)
			if err != nil {
				w.WriteString("event: error\ndata: Failed to fetch data\n\n")
				return
			}

			var results []map[string]interface{}
			if err := cursor.All(ctx, &results); err != nil {
				w.WriteString("event: error\ndata: Failed to decode results\n\n")
				return
			}

			response := map[string]interface{}{
				"data":     results,
				"progress": int((float64(offset+len(results)) / float64(total)) * 100),
			}

			data, _ := json.Marshal(response)
			w.WriteString(fmt.Sprintf("data: %s\n\n", string(data)))
			w.Flush()
		}

		w.WriteString("event: done\ndata: \"complete\"\n\n")
	}))

	return nil
}

// formatSerialNumbers converts []int to compact string format
func formatSerialNumbers(numbers []int) string {
	if len(numbers) == 0 {
		return ""
	}
	if len(numbers) == 1 {
		return fmt.Sprintf("%d", numbers[0])
	}

	sort.Ints(numbers)

	// Check if all numbers are consecutive
	isConsecutive := true
	for i := 1; i < len(numbers); i++ {
		if numbers[i] != numbers[i-1]+1 {
			isConsecutive = false
			break
		}
	}

	if isConsecutive {
		result := fmt.Sprintf("%d-%d", numbers[0], numbers[len(numbers)-1])
		return result
	}

	var result []string
	start := numbers[0]
	end := numbers[0]

	for i := 1; i < len(numbers); i++ {
		if numbers[i] == end+1 {
			end = numbers[i]
		} else {
			if start == end {
				result = append(result, fmt.Sprintf("%d", start))
			} else {
				result = append(result, fmt.Sprintf("%d-%d", start, end))
			}
			start = numbers[i]
			end = numbers[i]
		}
	}

	// Handle the last range
	if start == end {
		result = append(result, fmt.Sprintf("%d", start))
	} else {
		result = append(result, fmt.Sprintf("%d-%d", start, end))
	}

	finalResult := strings.Join(result, ",")
	return finalResult
}

func GetKernalInventory(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	var requestBody map[string]interface{}
	if err := c.BodyParser(&requestBody); err != nil {
		return shared.BadRequest("Invalid JSON payload")
	}

	filter := bson.M{
		"status": "packed",
	}
	if productName, ok := requestBody["product_name"].(string); ok && productName != "" {
		filter["product_id"] = productName
	}
	if purchaseId, ok := requestBody["purchase_id"].(string); ok && purchaseId != "" {
		filter["purchase_id"] = purchaseId
	}
	if originId, ok := requestBody["origin_id"].(string); ok && originId != "" {
		filter["origin_id"] = originId
	}
	if serialNoStr, ok := requestBody["serial_no"].(string); ok && serialNoStr != "" {
		serialsToExclude, err := helper.FormatSerialRange(serialNoStr)
		if err != nil {
			return shared.BadRequest("Invalid serial_no format: " + err.Error())
		}

		if len(serialsToExclude) > 0 {
			filter["s_no"] = bson.M{"$nin": serialsToExclude}
		}
	}

	if requestBody["product_taken_from"] != "oldStock" && requestBody["product_taken_from"] != "newStock" {
		if selectedSerials, ok := requestBody["selected_serials"].(string); ok && selectedSerials != "" {
			serials, err := helper.FormatSerialRange(selectedSerials)
			if err != nil {
				return shared.BadRequest("Invalid serial_no format: " + err.Error())
			}
			filter["s_no"] = bson.M{"$in": serials}
		}
	}
	sortOrder := -1
	if productTakenFrom, ok := requestBody["product_taken_from"].(string); ok && productTakenFrom == "oldStock" {
		sortOrder = 1
	}

	cursor, err := database.GetConnection(org.Id).Collection("kernel_inventory").Find(
		context.Background(), filter, options.Find().SetSort(bson.M{"s_no": sortOrder}))
	if err != nil {
		return shared.BadRequest("Error querying kernel inventory")
	}
	defer cursor.Close(context.Background())

	var results []map[string]interface{}
	if err = cursor.All(context.Background(), &results); err != nil {
		return shared.BadRequest("Error decoding results")
	}
	if len(results) == 0 {
		return shared.BadRequest("No kernel inventory records found")
	}

	var selectedResults []map[string]interface{}
	if quantity, ok := requestBody["quantity"]; ok {
		requiredQty := helper.ToFloat64(quantity)
		currentQty := 0.0
		for _, result := range results {
			if currentQty >= requiredQty {
				break
			}
			selectedResults = append(selectedResults, result)
			resultQty := helper.ToFloat64(result["quantity"])
			currentQty += resultQty
		}
	} else {
		selectedResults = results
	}
	var serialNumbers []int
	tinKg := 0

	for _, result := range selectedResults {
		sNo := helper.ToInt(result["s_no"])
		serialNumbers = append(serialNumbers, sNo)

		if tinKg == 0 {
			if packingType, exists := result["type_of_packing"]; exists {
				var packingDoc map[string]interface{}
				if err := database.GetConnection(org.Id).Collection("lookup").FindOne(
					context.Background(), bson.M{"_id": packingType}).Decode(&packingDoc); err == nil {
					tinKg = helper.ToInt(packingDoc["value"])
				}
			} else if result["stock_from"] == "purchase" {
				tinKg = helper.ToInt(result["quantity"])
			}
		}
	}

	stockQuantity := len(selectedResults) * tinKg

	// Remove duplicates and sort
	uniqueSerials := make(map[int]bool)
	for _, num := range serialNumbers {
		uniqueSerials[num] = true
	}
	serialNumbers = make([]int, 0, len(uniqueSerials))
	for num := range uniqueSerials {
		serialNumbers = append(serialNumbers, num)
	}

	serialNo := formatSerialNumbers(serialNumbers)

	return shared.SuccessResponse(c, bson.M{
		"serial_no":      serialNo,
		"stock_quantity": stockQuantity,
		"tin_count":      len(selectedResults),
		"tin_kg":         tinKg,
		"purchase_id":    selectedResults[0]["purchase_id"],
		"production_id":  selectedResults[0]["production_id"],
		"warehouse_id":   selectedResults[0]["warehouse_id"],
		"origin_id":      selectedResults[0]["origin_id"],
	})
}

func UpdateKernelInventorySerailNumber(inputData map[string]interface{}, orgId string, userId string) error {
	ctx := context.Background()
	db := database.GetConnection(orgId)
	collection := db.Collection("kernel_inventory")

	tinGridData, ok := inputData["tin_grid_data"].([]interface{})
	if !ok {
		return fmt.Errorf("tin_grid_data is not a valid array")
	}

	for i, item := range tinGridData {
		gridItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		serialNoStr, ok := gridItem["serial_no"].(string)
		if !ok || serialNoStr == "" {
			continue
		}

		serials, err := helper.FormatSerialRange(serialNoStr)
		if err != nil {
			return fmt.Errorf("item %d: invalid serial_no format '%s': %v", i+1, serialNoStr, err)
		}

		filter := bson.M{
			"s_no":   bson.M{"$in": serials},
			"status": "packed",
		}

		if originId, ok := gridItem["origin_id"].(string); ok && originId != "" {
			filter["origin_id"] = originId
		}
		if warehouseId, ok := gridItem["warehouse_id"].(string); ok && warehouseId != "" {
			filter["warehouse_id"] = warehouseId
		}
		if purchaseId, ok := gridItem["purchase_id"].(string); ok && purchaseId != "" {
			filter["purchase_id"] = purchaseId
		}
		if productId, ok := inputData["product_id"].(string); ok && productId != "" {
			filter["product_id"] = productId
		}

		// First, check if the serial numbers exist and are available
		count, err := collection.CountDocuments(ctx, filter)
		if err != nil {
			return fmt.Errorf("item %d: failed to check serial number availability: %v", i+1, err)
		}

		expectedCount := int64(len(serials))
		if count < expectedCount {
			return fmt.Errorf("item %d: insufficient serial numbers available - expected %d, found %d (serials: %s)",
				i+1, expectedCount, count, serialNoStr)
		}

		update := bson.M{
			"$set": bson.M{
				"status":     "sold",
				"updated_on": time.Now(),
				"updated_by": userId,
			},
		}

		res, err := collection.UpdateMany(ctx, filter, update)
		if err != nil {
			return fmt.Errorf("item %d: failed to update kernel inventory serial numbers: %v", i+1, err)
		}

		if res.ModifiedCount != expectedCount {
			return fmt.Errorf("item %d: expected to update %d serial numbers but only updated %d (serials: %s)",
				i+1, expectedCount, res.ModifiedCount, serialNoStr)
		}
	}

	return nil
}

func UpdateKernelInventorySerailNumberWithIndices(inputData map[string]interface{}, orgId string, userId string) ([]int, error) {
	ctx := context.Background()
	db := database.GetConnection(orgId)
	collection := db.Collection("kernel_inventory")

	tinGridData, ok := inputData["tin_grid_data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("tin_grid_data is not a valid array")
	}

	var successIndices []int

	for i, item := range tinGridData {
		gridItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		serialNoStr, ok := gridItem["serial_no"].(string)
		if !ok || serialNoStr == "" {
			continue
		}

		serials, err := helper.FormatSerialRange(serialNoStr)
		if err != nil {
			log.Printf("Skipping invalid serial_no format: %s. Error: %v", serialNoStr, err)
			continue
		}

		filter := bson.M{
			"s_no":   bson.M{"$in": serials},
			"status": "packed",
		}

		if originId, ok := gridItem["origin_id"].(string); ok && originId != "" {
			filter["origin_id"] = originId
		}
		if warehouseId, ok := gridItem["warehouse_id"].(string); ok && warehouseId != "" {
			filter["warehouse_id"] = warehouseId
		}
		if purchaseId, ok := gridItem["purchase_id"].(string); ok && purchaseId != "" {
			filter["purchase_id"] = purchaseId
		}
		if productId, ok := inputData["product_id"].(string); ok && productId != "" {
			filter["product_id"] = productId
		}

		update := bson.M{
			"$set": bson.M{
				"status":     "sold",
				"updated_on": time.Now(),
				"updated_by": userId,
			},
		}

		res, err := collection.UpdateMany(ctx, filter, update)
		if err != nil {
			return nil, fmt.Errorf("failed to update kernel inventory serial numbers: %v", err)
		}

		// Only consider it a success if we actually updated something
		if res.ModifiedCount > 0 {
			successIndices = append(successIndices, i)
		}
	}

	return successIndices, nil
}

func UpdateSerials(id string, orgId string, userID string, method string) {
	ctx := context.Background()
	db := database.GetConnection(orgId)

	soldProductsCollection := db.Collection("sold_products_info")
	kernelInventoryCollection := db.Collection("kernel_inventory")

	var soldProductDoc map[string]interface{}
	filter := bson.M{"_id": id}
	err := soldProductsCollection.FindOne(ctx, filter).Decode(&soldProductDoc)
	if err != nil {
		log.Printf("Error fetching sold_products_info document with id %s: %v", id, err)
		return
	}

	tinGridData, ok := soldProductDoc["tin_grid_data"].(primitive.A)
	if !ok {
		log.Printf("tin_grid_data is not a valid array for document %s", id)
		return
	}

	var allSerialNumbers []int
	for _, item := range tinGridData {
		gridItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		serialNoStr, ok := gridItem["serial_no"].(string)
		if !ok || serialNoStr == "" {
			continue
		}

		serials, err := helper.FormatSerialRange(serialNoStr)
		if err != nil {
			log.Printf("Skipping invalid serial_no format: %s. Error: %v", serialNoStr, err)
			continue
		}
		allSerialNumbers = append(allSerialNumbers, serials...)
	}

	if len(allSerialNumbers) == 0 {
		return // Nothing to update
	}

	updateFilter := bson.M{"s_no": bson.M{"$in": allSerialNumbers}, "product_id": soldProductDoc["product_id"], "warehouse_id": soldProductDoc["warehouse_id"], "origin_id": soldProductDoc["origin_id"], "purchase_id": soldProductDoc["purchase_id"]}
	update := bson.M{"$set": bson.M{"status": "packed", "updated_on": time.Now(), "updated_by": userID}}

	if _, err := kernelInventoryCollection.UpdateMany(ctx, updateFilter, update); err != nil {
		log.Printf("Failed to update kernel inventory serial numbers for sold_product %s: %v", id, err)
	}

}

func DeleteLedgerEntry(organizationID, entryID string, userID string) error {
	database := database.GetConnection(organizationID)
	client := database.Client()

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.Background())

	_, err = session.WithTransaction(context.Background(), func(sessionCtx mongo.SessionContext) (interface{}, error) {
		var targetEntry StockLedgerEntry
		err := database.Collection("stock_ledger").FindOne(sessionCtx, bson.M{"_id": entryID}).Decode(&targetEntry)
		if err != nil {
			return nil, errors.New("ledger entry not found: " + entryID)
		}

		quantityToReverse := -targetEntry.TransactionBalance
		if targetEntry.TransactionType == "sale" {
			quantityToReverse = targetEntry.TransactionBalance // Add back to stock
		}

		if err := updateStockBalance(sessionCtx, organizationID, targetEntry.Origin, targetEntry.StockType,
			targetEntry.WarehouseId, targetEntry.FactoryId, targetEntry.PurchaseID, targetEntry.ProductId,
			quantityToReverse, userID, "", ""); err != nil {
			return nil, err
		}

		filter := bson.M{
			"origin":       targetEntry.Origin,
			"purchase_id":  targetEntry.PurchaseID,
			"warehouse_id": targetEntry.WarehouseId,
			"created_on":   bson.M{"$gt": targetEntry.CreatedOn},
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

		newOpeningBalance := targetEntry.OpeningBalance
		for i := range entries {
			entries[i].OpeningBalance = newOpeningBalance
			stockDelta := calculateStockDelta(entries[i].TransactionType, entries[i].TransactionBalance)
			entries[i].ClosingBalance = entries[i].OpeningBalance + stockDelta
			newOpeningBalance = entries[i].ClosingBalance

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

		_, err = database.Collection("stock_ledger").DeleteOne(sessionCtx, bson.M{"_id": entryID})
		if err != nil {
			return nil, err
		}

		return nil, nil
	})

	return err
}

type DBMIGRATE struct {
	SourceOrgID string `json:"source_org_id" bson:"source_org_id"`
	TargetOrgID string `json:"target_org_id" bson:"target_org_id"`
	OverWrite   bool   `json:"overwrite" bson:"overwrite"`
}

func MigrateOneDBToAnotherDB(c *fiber.Ctx) error {
	var req DBMIGRATE

	err := c.BodyParser(&req)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	err = CopyMultipleCollections(req.SourceOrgID, req.TargetOrgID, req.OverWrite, "")
	if err != nil {
		return shared.InternalServerError(err.Error())
	}
	res := map[string]interface{}{
		"message": "DB migrated successfully",
	}
	return shared.SuccessResponse(c, res)
}

func CopyMultipleCollections(sourceOrgID string, targetOrgID string, overwrite bool, roleId string) error {

	// Hardcoded collection names
	var collections []string

	if strings.Contains(targetOrgID, "_demo") {
		collections = []string{
			"user", // ← MOVED TO FIRST
			"process",
			"product",
			"origin",
			"country",
			"screen",
			"dataset_config",
			"jobwork_template",
			"templatetype",
			"role_acl",
			"designation",
			"lookup",
			"product_group",
			"process_product",
			"dashboard_config",
			"master_menu",
		}
	} else {
		collections = []string{
			"process",
			"product",
			"origin",
			"country",
			"screen",
			"dataset_config",
			"jobwork_template",
			"templatetype",
			"role_acl",
			"lookup",
			"product_group",
			"questions",
			"factory_process",
			"dashboard_config",
			"master_menu",
		}
	}

	sourceDB := database.GetConnection(sourceOrgID)
	targetDB := database.GetConnection(targetOrgID)

	ctx := context.Background()

	for _, colName := range collections {

		fmt.Println("Copying collection:", colName)

		sourceCol := sourceDB.Collection(colName)
		targetCol := targetDB.Collection(colName)

		cursor, err := sourceCol.Find(ctx, bson.M{})
		if err != nil {
			return fmt.Errorf("error fetching from %s: %v", colName, err)
		}

		for cursor.Next(ctx) {
			var doc bson.M
			if err := cursor.Decode(&doc); err != nil {
				return err
			}

			id := doc["_id"]
			if colName != "role_acl" {
				if overwrite {
					// Replace or insert
					_, err = targetCol.ReplaceOne(
						ctx,
						bson.M{"_id": id},
						doc,
						options.Replace().SetUpsert(true),
					)
				} else {
					// Insert only if not exists
					_, err = targetCol.InsertOne(ctx, doc)
					if mongo.IsDuplicateKeyError(err) {
						continue // skip duplicates
					}
				}

			} else {
				if roleId == doc["_id"] {

					if overwrite {
						// Replace or insert
						_, err = targetCol.ReplaceOne(
							ctx,
							bson.M{"_id": id},
							doc,
							options.Replace().SetUpsert(true),
						)
					} else {
						// Insert only if not exists
						_, err = targetCol.InsertOne(ctx, doc)
						if mongo.IsDuplicateKeyError(err) {
							continue // skip duplicates
						}
					}
				}
			}

			if err != nil {
				return err
			}
		}

		cursor.Close(ctx)
	}

	return nil
}

func updateMobileJson(templateId string, orgId string, templateJson string, inputData map[string]interface{}) error {

	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"template_id", templateId}}}},
	}

	templateProducts, err := helper.GetAggregateQueryResult(orgId, "process_product", pipeline)
	if err != nil {
		return err
	}

	if len(templateProducts) == 0 {
		return fmt.Errorf("No data found")
	}

	var inputList []string
	var viewProductsList []string
	var outputList []string
	var hasViewScreen bool
	var viewScreenId string
	var baseViewScreenId string
	var screenConfig string
	allPayloadObject := make(map[string]interface{})

	if inputData["has_view_screen"] != nil {

		hasViewScreen = inputData["has_view_screen"].(bool)

		if hasViewScreen {
			viewScreenId = inputData["view_screen_id"].(string)
			if inputData["is_templateBaced_view"] == true {
				viewScreenId = templateId + "_" + inputData["process_id"].(string)
			}
			if inputData["view_screen_id"] != nil {
				baseViewScreenId = inputData["view_screen_id"].(string)
				var screen map[string]interface{}
				database.GetConnection(orgId).Collection("screen").FindOne(context.Background(), bson.M{"_id": baseViewScreenId}).Decode(&screen)
				if screen["config"] != nil {
					screenConfig = screen["config"].(string)
				}

			}
		}
	}

	for _, obj := range templateProducts {

		label := getString(obj["label"])
		errorMessage := getString(obj["error"])
		fieldName := getString(obj["product_id"])
		productType := getString(obj["type"])
		required := getBool(obj["required"])
		needToCal := getBool(obj["need_to_calculate"])
		isCompele := getBool(obj["isCompele"])
		salary_calculate := getBool(obj["salary_calculate"])
		addExtraFiled := getBool(obj["addextra_field"])
		extraFiledRaw := getString(obj["extra_field"])
		haveGroup := getBool(obj["set_group"])
		setOrder := getBool(obj["set_order"])
		haveviewLabel := getBool(obj["set_view_label"])
		haveVieworderId := getBool(obj["set_view_order"])
		readOnly := getBool(obj["readonly"])
		viewLabel := ""
		viewOrder := interface{}(nil)
		if haveviewLabel && obj["view_label"] != nil {
			viewLabel = obj["view_label"].(string)
		}
		if haveVieworderId && obj["view_order_id"] != nil {
			viewOrder = obj["view_order_id"]
		}
		orderId := obj["order_id"]
		groupId := getString(obj["group_id"])
		mappedObject := map[string]interface{}{
			"label":             label + " ( kg )",
			"type":              "number",
			"key":               fieldName,
			"required":          required,
			"errormessage":      errorMessage,
			"need_to_calculate": needToCal,
			"isCompele":         isCompele,
			"salary_calculate":  salary_calculate,
			"filedType":         productType,
			"readonly":          readOnly,
		}

		if haveGroup && groupId != "" {
			mappedObject["group_id"] = groupId
		}
		if setOrder && orderId != nil {
			mappedObject["order_id"] = orderId
		}
		if addExtraFiled && extraFiledRaw != "" {

			extra := make(map[string]interface{})

			var inputFieldNames []string
			var outputFieldNames []string
			parentFiledNames := make(map[string][]string)

			for _, tp := range templateProducts {

				if getString(tp["type"]) == "input" {
					inputFieldNames = append(inputFieldNames, getString(tp["product_id"]))
				} else if getString(tp["type"]) == "output" {
					outputFieldNames = append(outputFieldNames, getString(tp["product_id"]))
				}
				if tp["group_id"] != nil {
					groupKey := fmt.Sprintf("%v", tp["group_id"])
					productID := fmt.Sprintf("%v", tp["product_id"])
					if getString(tp["type"]) == "input" {
						parentFiledNames[groupKey] = append([]string{productID}, parentFiledNames[groupKey]...)
					} else {
						parentFiledNames[groupKey] = append(parentFiledNames[groupKey], productID)
					}
				}
			}

			inputFieldsJSON, err := json.Marshal(inputFieldNames)
			if err != nil {
				log.Println("Error marshaling input fields:", err)
			}

			outputFieldsJSON, err := json.Marshal(outputFieldNames)
			if err != nil {
				log.Println("Error marshaling output fields:", err)
			}

			if groupId != "" && parentFiledNames[groupId] != nil {
				parentFieldsJSON, err := json.Marshal(parentFiledNames[groupId])
				if err != nil {
					log.Println("Error marshaling parent fields:", err)
				}
				extraFiledRaw = strings.ReplaceAll(extraFiledRaw, "{array_of_grouped_fields}", string(parentFieldsJSON))
			}

			extraFiledRaw = strings.ReplaceAll(extraFiledRaw, "{array_of_input_fields}", string(inputFieldsJSON))
			extraFiledRaw = strings.ReplaceAll(extraFiledRaw, "{array_of_output_fields}", string(outputFieldsJSON))
			extraFiledRaw = strings.ReplaceAll(extraFiledRaw, "\"[", "[")
			extraFiledRaw = strings.ReplaceAll(extraFiledRaw, "]\"", "]")
			extraFiledRaw = strings.ReplaceAll(extraFiledRaw, ",\n}", "\n}")
			extraFiledRaw = strings.ReplaceAll(extraFiledRaw, ",}", "}")
			err = json.Unmarshal([]byte(extraFiledRaw), &extra)
			if err != nil {
				log.Println("Error unmarshalling extra fields:", err)
			} else {
				for k, v := range extra {
					mappedObject[k] = v
				}
			}
		}

		if hasViewScreen && viewScreenId != "" {
			labelValue := label
			if haveviewLabel && viewLabel != "" {
				labelValue = viewLabel
			}
			fontSize := 16
			color := "blackColor"

			style := map[string]interface{}{
				"fontSize": fontSize,
				"color":    color,
			}
			if obj["set_view_label_style"] == true {
				if obj["view_fontSize"] != nil {
					if v, ok := obj["view_fontSize"].(float64); ok {
						style["fontSize"] = int(v)
					}
				}

				if obj["view_color"] != nil {
					if v, ok := obj["view_color"].(string); ok {
						style["color"] = v
					}
				}

				if obj["view_fontWeight"] != nil {
					if v, ok := obj["view_fontWeight"].(float64); ok {
						style["fontWeight"] = int(v)
					}
				}
			}

			viewMappedObject := map[string]interface{}{
				"label":              labelValue,
				"type":               "number",
				"value":              fieldName,
				"show_separate_view": true,
				"style":              style,
			}

			if haveVieworderId {
				viewMappedObject["order_id"] = viewOrder
			}
			if obj["hide_view"] == true {
				viewMappedObject["hide_in_view"] = true
			}
			viewJsonBytes, _ := json.Marshal(viewMappedObject)
			viewJsonString := string(viewJsonBytes)
			viewProductsList = append(viewProductsList, viewJsonString)
		}

		allPayloadObject[fieldName] = map[string]interface{}{
			"value":    fieldName,
			"datatype": "double",
		}

		jsonBytes, _ := json.Marshal(mappedObject)
		jsonString := string(jsonBytes)

		if productType == "input" {
			inputList = append(inputList, jsonString)
		} else if productType == "output" {
			outputList = append(outputList, jsonString)
		}
	}

	// Join correct (adds comma only between), no last comma
	inputFields := strings.Join(inputList, ",")
	outputFields := strings.Join(outputList, ",")
	// viewProductsListFields := strings.Join(viewProductsList, ",")

	// Replace in template
	if len(inputFields) == 0 {
		templateJson = strings.ReplaceAll(templateJson, `{"fields":"input"},`, inputFields)
	} else {
		templateJson = strings.ReplaceAll(templateJson, `{"fields":"input"}`, inputFields)

	}
	// if len(viewProductsListFields) == 0 {
	// 	screenConfig = strings.ReplaceAll(screenConfig, `{"fields":"view_products"},`, viewProductsListFields)
	// } else {
	// 	screenConfig = strings.ReplaceAll(screenConfig, `{"fields":"view_products"}`, viewProductsListFields)

	// }
	if len(viewProductsList) > 0 {
		screenConfig, _ = AppendUniqueWithoutRemoving(screenConfig, viewProductsList)

	}

	if len(outputFields) == 0 {

		templateJson = strings.ReplaceAll(templateJson, `{"fields":"output"},`, outputFields)
	} else {
		templateJson = strings.ReplaceAll(templateJson, `{"fields":"output"}`, outputFields)
	}

	b, _ := json.Marshal(allPayloadObject)

	// Convert:  {"a":1,"b":2}  →  "a":1,"b":2
	payloadStr := strings.TrimPrefix(string(b), "{")
	payloadStr = strings.TrimSuffix(payloadStr, "}")

	templateJson = strings.ReplaceAll(templateJson, `"payload":true,`, payloadStr+",")
	filter := bson.M{
		"_id": templateId,
	}
	updateData := bson.M{"$set": bson.M{"mobile_template_config_updated": templateJson}}
	_, err = database.GetConnection(orgId).Collection("templatetype").UpdateOne(context.Background(), filter, updateData)
	if err != nil {
		return err
	}
	if hasViewScreen { //Mobile-Form
		created_on := time.Now()
		if inputData["is_templateBaced_view"] == true {
			viewscreenupdateData := bson.M{
				"$set": bson.M{
					"config":         screenConfig,
					"status":         "A",
					"process_id":     inputData["process_id"].(string),
					"base_screen_id": baseViewScreenId,
					"type":           "Mobile-View",
					"view_type":      "template_view",
					"name":           templateId + "_" + inputData["process_id"].(string) + "_View",
				},
				"$setOnInsert": bson.M{
					"created_on": created_on,
					"created_by": inputData["update_by"].(string),
				},
			}

			_, err = database.GetConnection(orgId).Collection("screen").UpdateOne(context.Background(), helper.DocIdFilter(viewScreenId), viewscreenupdateData, updateOpts)
			if err != nil {
				return err
			}
		} else {
			viewScreenfilter := bson.M{
				"_id": viewScreenId,
			}
			viewscreenupdateData := bson.M{"$set": bson.M{"config": screenConfig}}
			_, err = database.GetConnection(orgId).Collection("screen").UpdateOne(context.Background(), viewScreenfilter, viewscreenupdateData)
			if err != nil {
				return err
			}
		}
	}

	return nil

}
func AppendUniqueWithoutRemoving(screenConfig string, newItems []string) (string, error) {

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(screenConfig), &data); err != nil {
		return screenConfig, err
	}

	fields, ok := data["fields"].([]interface{})
	if !ok {
		return screenConfig, nil
	}

	// Convert newItems → map[value]object
	newMap := map[string]map[string]interface{}{}

	for _, s := range newItems {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			if val, ok := obj["value"].(string); ok {
				newMap[val] = obj
			}
		}
	}

	updatedFields := []interface{}{}
	used := map[string]bool{}

	for _, f := range fields {

		obj, isMap := f.(map[string]interface{})

		// Replace existing field if value matches
		if isMap {
			if val, ok := obj["value"].(string); ok {
				if newObj, exists := newMap[val]; exists {
					updatedFields = append(updatedFields, newObj)
					used[val] = true
					continue
				}
			}
		}

		// Insert missing NEW items before placeholder
		if isMap && obj["fields"] == "view_products" {

			for val, newObj := range newMap {
				if !used[val] {
					updatedFields = append(updatedFields, newObj)
					used[val] = true
				}
			}

			updatedFields = append(updatedFields, f)
			continue
		}

		updatedFields = append(updatedFields, f)
	}

	data["fields"] = updatedFields

	out, _ := json.MarshalIndent(data, "", "    ")
	return string(out), nil
}

// func AppendBeforeViewProducts(screenConfig string, newItems []string) (string, error) {

// 	placeholder := `{"fields":"view_products"}`

// 	// Find placeholder index
// 	idx := strings.Index(screenConfig, placeholder)
// 	if idx == -1 {
// 		return screenConfig, nil // no placeholder found
// 	}

// 	// Build final block with unique items
// 	unique := map[string]bool{}
// 	finalList := []string{}

// 	for _, it := range newItems {
// 		if !unique[it] {
// 			unique[it] = true
// 			finalList = append(finalList, it)
// 		}
// 	}

// 	// Build insertion text
// 	insertText := strings.Join(finalList, ",\n") + ",\n"

// 	// Insert BEFORE placeholder
// 	updated := screenConfig[:idx] + insertText + screenConfig[idx:]

// 	return updated, nil
// }

func getString(v interface{}) string {
	if v == nil {
		return ""
	}
	return v.(string)
}

func getBool(v interface{}) bool {
	if v == nil {
		return false
	}
	return v.(bool)
}

func CreateScreenForActiveProcessesHandler(c *fiber.Ctx) error {
	// Get organization
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)

	// Parse request body
	var requestData struct {
		FactoryID string `json:"factory_id"`
	}

	err := c.BodyParser(&requestData)
	if err != nil {
		fmt.Printf("ERROR: Failed to parse request body: %v\n", err)
		return shared.BadRequest(err.Error())
	}

	// Validate required fields
	if requestData.FactoryID == "" {
		fmt.Printf("ERROR: factory_id is missing\n")
		return shared.BadRequest("factory_id is required")
	}

	// Debug: Log the request details
	fmt.Printf("DEBUG: Processing request for factory_id: %s, org_id: %s\n", requestData.FactoryID, org.Id)

	// First, let's check if the factory exists
	var factoryCheck map[string]interface{}
	err = database.GetConnection(org.Id).Collection("config").FindOne(context.Background(), bson.M{"_id": requestData.FactoryID}).Decode(&factoryCheck)
	if err != nil {
		fmt.Printf("ERROR: Factory %s not found in config collection: %v\n", requestData.FactoryID, err)
		return shared.BadRequest(fmt.Sprintf("Factory %s not found: %v", requestData.FactoryID, err))
	}
	fmt.Printf("DEBUG: Factory found in config collection\n")

	// Create screen collections for active processes
	err = CreateScreenCollectionForActiveProcesses(org.Id, requestData.FactoryID)
	if err != nil {
		fmt.Printf("ERROR: Failed to create screen collections: %v\n", err)
		return shared.BadRequest(fmt.Sprintf("Failed to create screen collections: %v", err))
	}

	// Verify screens were actually created
	screenCount, err := database.GetConnection(org.Id).Collection("screen").CountDocuments(context.Background(), bson.M{"factory_id": requestData.FactoryID})
	if err != nil {
		fmt.Printf("WARNING: Could not verify screen creation: %v\n", err)
	} else {
		fmt.Printf("DEBUG: Total screens created for factory %s: %d\n", requestData.FactoryID, screenCount)
	}

	// Get the screen ID that was created/updated
	screenId := fmt.Sprintf("Home-Screen-%s", requestData.FactoryID)

	responseData := fiber.Map{
		"message":         fmt.Sprintf("Screen collections created successfully for factory %s", requestData.FactoryID),
		"Inserted_ID":     screenId,
		"factory_id":      requestData.FactoryID,
		"created_by":      userToken.UserId,
		"created_at":      time.Now().UTC(),
		"screens_created": screenCount,
	}

	fmt.Printf("DEBUG: Response data: %+v\n", responseData)

	return shared.SuccessResponse(c, responseData)
}

func CreateScreenCollectionForActiveProcesses(orgId string, factoryId string) error {
	// Get active processes first
	fmt.Printf("DEBUG: Getting active processes for org: %s, factory: %s\n", orgId, factoryId)
	activeProcesses, err := GetActiveProcessesByFactoryId(orgId, factoryId)
	if err != nil {
		fmt.Printf("ERROR: Error getting active processes: %v\n", err)
		return err
	}

	fmt.Printf("DEBUG: Found %d factory records\n", len(activeProcesses))

	if len(activeProcesses) == 0 {
		return fmt.Errorf("no factory data found for factory: %s", factoryId)
	}

	factoryRecord := activeProcesses[0]
	factoryProcessData, ok := factoryRecord["factory_process_data"].(primitive.A)
	if !ok {
		return fmt.Errorf("factory_process_data not found or not in expected format for factory: %s", factoryId)
	}

	fmt.Printf("DEBUG: Found %d factory processes to filter cards\n", len(factoryProcessData))

	// Build the config object with proper ordering
	configObj := map[string]interface{}{
		"factory_id":          factoryId,
		"org_id":              orgId,
		"title":               "Home",
		"titleColor":          "#f5faf9",
		"leadingBackIconNeed": false,
		"leadingBackIcon":     "arrow_back_ios",
		"children": []interface{}{
			map[string]interface{}{
				"type":     "GridView",
				"children": getFilteredCards([]interface{}(factoryProcessData)),
			},
			map[string]interface{}{
				"type":   "SizeBox",
				"height": 15.0,
			},
		},
	}
	// Convert config to JSON string with proper formatting
	configJSON, err := json.MarshalIndent(configObj, "", "    ")
	if err != nil {
		fmt.Printf("ERROR: Failed to marshal config: %v\n", err)
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	// Prepare the update document
	updateDocument := bson.M{
		"$set": bson.M{
			"config":     string(configJSON),
			"updated_at": time.Now().UTC(),
			"status":     "Active",
			"org_id":     orgId,
			"factory_id": factoryId,
		},
	}

	// Update the screen document - match by factory_id
	ctx := context.Background()
	db := database.GetConnection(orgId)
	collection := db.Collection("screen")

	// Create unique ID for each factory's home screen
	screenId := fmt.Sprintf("Home-Screen-%s", factoryId)
	filter := bson.M{"_id": screenId}
	result, err := collection.UpdateOne(ctx, filter, updateDocument)
	if err != nil {
		fmt.Printf("ERROR: Failed to update screen for factory %s: %v\n", factoryId, err)
		return fmt.Errorf("failed to update screen: %v", err)
	}

	if result.MatchedCount == 0 {
		fmt.Printf("WARNING: No screen document found for factory %s, creating new one\n", factoryId)
		// Create the document if it doesn't exist

		// Convert config to JSON string for insertion with proper formatting
		configJSON, err := json.MarshalIndent(configObj, "", "    ")
		if err != nil {
			fmt.Printf("ERROR: Failed to marshal config for insertion: %v\n", err)
			return fmt.Errorf("failed to marshal config for insertion: %v", err)
		}

		screenId := fmt.Sprintf("Home-Screen-%s", factoryId)
		screenDocument := map[string]interface{}{
			"_id":        screenId,
			"config":     string(configJSON),
			"created_at": time.Now().UTC(),
			"updated_at": time.Now().UTC(),
			"status":     "Active",
			"org_id":     orgId,
			"factory_id": factoryId,
		}

		_, err = collection.InsertOne(ctx, screenDocument)
		if err != nil {
			fmt.Printf("ERROR: Failed to create screen document: %v\n", err)
			return fmt.Errorf("failed to create screen document: %v", err)
		}
		fmt.Printf("SUCCESS: Created new screen document for factory %s\n", factoryId)
	} else {
		fmt.Printf("SUCCESS: Updated screen document for factory %s, modified %d document(s)\n", factoryId, result.ModifiedCount)
	}

	fmt.Printf("DEBUG: Home-Screen update completed\n")
	return nil
}

func getFilteredCards(factoryProcessData []interface{}) []interface{} {
	// Extract active process IDs from factory_process_data where status is "Active"
	activeProcessIds := make(map[string]bool)
	fmt.Printf("DEBUG: Factory process data received: %d processes\n", len(factoryProcessData))

	for i, process := range factoryProcessData {
		fmt.Printf("DEBUG: Process %d type: %T\n", i, process)

		// Handle different types from MongoDB
		var processMap map[string]interface{}
		var ok bool

		switch v := process.(type) {
		case map[string]interface{}:
			processMap = v
			ok = true
		case primitive.M:
			processMap = make(map[string]interface{})
			for k, val := range v {
				processMap[k] = val
			}
			ok = true
		default:
			fmt.Printf("DEBUG: Process %d is not a map, type: %T\n", i, process)
			continue
		}

		if !ok {
			continue
		}

		// Check if status is "Active"
		if status, exists := processMap["status"]; exists {
			statusStr := fmt.Sprintf("%v", status) // Convert to string for comparison
			fmt.Printf("DEBUG: Process status: '%s' (type: %T)\n", statusStr, status)
			if statusStr == "Active" {
				if processId, exists := processMap["process_id"]; exists {
					if processIdStr, ok := processId.(string); ok {
						activeProcessIds[processIdStr] = true
						fmt.Printf("DEBUG: Found active process ID: %s\n", processIdStr)
					}
				}
			} else {
				if processId, exists := processMap["process_id"]; exists {
					if processIdStr, ok := processId.(string); ok {
						fmt.Printf("DEBUG: Skipping inactive process ID: %s (status: '%s')\n", processIdStr, statusStr)
					}
				}
			}
		} else {
			fmt.Printf("DEBUG: Process has no status field\n")
			// Print all available fields for debugging
			fmt.Printf("DEBUG: Available fields: ")
			for key := range processMap {
				fmt.Printf("%s ", key)
			}
			fmt.Println()
		}
	}
	fmt.Printf("DEBUG: Final active process IDs: %v\n", activeProcessIds)

	// All hardcoded cards
	allCards := []interface{}{
		map[string]interface{}{
			"type":       "card",
			"process_id": "COOK",
			"name":       "STEAMING",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/cooker.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "RcnInward",
				"process_code":   "COK",
				"screen_path":    "Cooking-View",
				"euipment_type":  "COOK",
				"template_based": false,
				"get_local_data": true,
			},
		},
		map[string]interface{}{
			"type":       "card",
			"process_id": "SHELL",
			"name":       "SHELLING",
			"image":      "assets/images/shelling.png",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "ShellingScreen",
				"euipment_type":  "SHELL",
				"process_code":   "SH",
				"screen_path":    "Shelling-view",
				"template_based": true,
				"get_local_data": true,
			},
		},
		map[string]interface{}{
			"type":       "card",
			"process_id": "BORM",
			"name":       "BORMA",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/vending_machine.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "BormaHomeScreen",
				"euipment_type":  "BORM",
				"screen_path":    "Borma-view",
				"process_code":   "BOR",
				"template_based": false,
				"get_local_data": true,
			},
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "COOLING",
			"process_id": "COOL",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/fan.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "CoolingHomeScreen",
				"screen_path":    "Cooling-view",
				"euipment_type":  "COOL",
				"process_code":   "COOL",
				"template_based": false,
				"get_local_data": true,
			},
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "PEELING",
			"process_id": "PEEL",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/peeler.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "PeelingInProgress",
				"euipment_type":  "PEEL",
				"process_code":   "MACP",
				"screen_path":    "Peeling-view",
				"template_based": true,
				"get_local_data": true,
				"show_tab":       true,
			},
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "GRADING",
			"process_id": "GRAD",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/Grading.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "GradingInProgress",
				"euipment_type":  "GRAD",
				"process_code":   "MANG",
				"screen_path":    "Grading-view",
				"template_based": true,
				"get_local_data": true,
				"show_tab":       true,
			},
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "PACKING",
			"process_id": "PACK",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/packing.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "PackingScreen",
				"euipment_type":  "PACK",
				"process_code":   "PAC",
				"screen_path":    "packing-view",
				"packing":        true,
				"template_based": true,
				"get_local_data": true,
				"droupdown-api": map[string]interface{}{
					"type":         "droupdown",
					"process_code": "MANG",
				},
			},
			"euipment_type": "",
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "WORK ENTRY",
			"text_color": "#195f49",
			"card_color": "#D4E4D6",
			"image":      "assets/images/salary.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "SalaryScreen",
				"process_code":   "SAL",
				"screen_path":    "worker-salary-view",
				"euipment_type":  "",
				"template_based": false,
				"get_local_data": true,
			},
			"euipment_type": "",
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "JOB WORK OUTWARD",
			"text_color": "#ffffff",
			"card_color": "#297c62",
			"image":      "assets/images/jobwork.png",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "JobWorkScreen",
				"process_code":   "JOB_OUTWARD",
				"screen_path":    "jobwork-outward-view",
				"template_based": false,
				"params": map[string]interface{}{
					"jobWorkType": "JOB_OUTWARD",
				},
			},
			"euipment_type": "JOB_OUTWARD",
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "JOB WORK INWARD",
			"image":      "assets/images/jobwork.png",
			"text_color": "#ffffff",
			"card_color": "#297c62",
			"onTap": map[string]interface{}{
				"type":           "navigate",
				"screenName":     "JobWorkScreen",
				"process_code":   "JOB_INWARD",
				"screen_path":    "jobwork-inward-view",
				"template_based": false,
				"params": map[string]interface{}{
					"jobWorkType": "JOB_INWARD",
				},
			},
			"euipment_type": "JOB_INWARD",
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "SCHEDULED MAINTENANCE",
			"text_color": "#ffffff",
			"card_color": "#195f49",
			"image":      "assets/images/scheduled.png",
			"onTap": map[string]interface{}{
				"type":        "navigate",
				"screenName":  "ScheduledScreen",
				"screen_path": "scheduled-maintenance-view",
			},
			"euipment_type": "",
		},
		map[string]interface{}{
			"type":       "card",
			"name":       "UNSCHEDULED MAINTENANCE",
			"text_color": "#ffffff",
			"card_color": "#195f49",
			"image":      "assets/images/Unscheduled.png",
			"onTap": map[string]interface{}{
				"type":       "navigate",
				"screenName": "UnScheduledScreen",
			},
			"euipment_type": "",
		},
	}

	// Filter cards based on active process IDs
	var filteredCards []interface{}
	for _, card := range allCards {
		if cardMap, ok := card.(map[string]interface{}); ok {
			if processId, exists := cardMap["process_id"]; exists {
				if processIdStr, ok := processId.(string); ok {
					fmt.Printf("DEBUG: Checking card with process_id: %s\n", processIdStr)
					if activeProcessIds[processIdStr] {
						fmt.Printf("DEBUG: Including card: %s (process_id: %s)\n", cardMap["name"], processIdStr)
						filteredCards = append(filteredCards, card)
					} else {
						fmt.Printf("DEBUG: Excluding card: %s (process_id: %s not active)\n", cardMap["name"], processIdStr)
					}
				}
			} else {
				// Include cards without process_id (like WORK ENTRY, MAINTENANCE, etc.)
				fmt.Printf("DEBUG: Including card without process_id: %s\n", cardMap["name"])
				filteredCards = append(filteredCards, card)
			}
		}
	}
	fmt.Printf("DEBUG: Total filtered cards: %d\n", len(filteredCards))

	return filteredCards
}

func convertToInterfaceSlice(bsonSlice []bson.M) []interface{} {
	result := make([]interface{}, len(bsonSlice))
	for i, item := range bsonSlice {
		result[i] = item
	}
	return result
}

func ReadjusmnetProduction(c *fiber.Ctx) error {
	orgId := "604162a4ce67408c8b22870191199ad3"

	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"process_id", 1},
					{"purchase_id", "DP-TOGO-26-002"},
				},
			},
		},
	}
	proudctionsData, err := helper.GetAggregateQueryResult(orgId, "productions", pipeline)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	updatedCount := 0

	if len(proudctionsData) > 0 {
		for _, obj := range proudctionsData {

			if helper.ToInt(obj["process_id"]) == 1 {

				inputValue := helper.ToFloat64(obj["input_weight"])
				ID := helper.ToString(obj["_id"])

				updateData := bson.M{
					"process_type": "COOK",
					"STEAMEDRCN":   inputValue,
				}

				res, err := database.GetConnection(orgId).
					Collection("productions").
					UpdateOne(
						context.Background(),
						bson.M{"_id": ID},
						bson.M{"$set": updateData},
					)

				if err == nil && res.ModifiedCount > 0 {
					updatedCount++
				}
			}

			// if helper.ToInt(obj["process_id"]) == 2 {

			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	pieces := helper.ToFloat64(obj["pieces"])

			// 	rejected := helper.ToFloat64(obj["rejected"])

			// 	wholes := helper.ToFloat64(obj["wholes"])

			// 	shell := helper.ToFloat64(obj["shell"])

			// 	ID := helper.ToString(obj["_id"])

			// 	updateData := bson.M{
			// 		"process_type": "SHELL",
			// 		"STEAMEDRCN":   inputValue,
			// 		"WHOLES":       wholes,
			// 		"REJECTED":     rejected,
			// 		"PIECES":       pieces,
			// 		"SHELL":        shell,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }
			// if helper.ToInt(obj["process_id"]) == 3 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	// pieces := helper.ToFloat64(obj["pieces"])

			// 	// rejected := helper.ToFloat64(obj["rejected"])

			// 	// wholes := helper.ToFloat64(obj["wholes"])

			// 	// shell := helper.ToFloat64(obj["shell"])

			// 	ID := helper.ToString(obj["_id"])

			// 	updateData := bson.M{
			// 		"process_type":     "BORM",
			// 		"template_id":      "Borma-NW-wholes-fields",
			// 		"WHOLES":           outputWeight,
			// 		"NW_WHOLES_INPUT":  inputValue,
			// 		"NW_WHOLES_OUTPUT": outputWeight,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }
			// if helper.ToInt(obj["process_id"]) == 5 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	husk := helper.ToFloat64(obj["husk"])

			// 	lwp := helper.ToFloat64(obj["lwp"])

			// 	swp := helper.ToFloat64(obj["swp"])

			// 	bb := helper.ToFloat64(obj["bb"])

			// 	splits := helper.ToString(obj["splits"])
			// 	ssp := helper.ToString(obj["ssp"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":     "PEEL",
			// 		"template_id":      "MCWP",
			// 		"WHOLES":           outputWeight,
			// 		"NW_WHOLES_INPUT":  inputValue,
			// 		"NW_WHOLES_OUTPUT": outputWeight,
			// 		"LWP":              lwp,
			// 		"SSP":              ssp,
			// 		"SWP":              swp,
			// 		"BB":               bb,
			// 		"HUSK":             husk,
			// 		"SPLITS":           splits,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }
			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	op := helper.ToFloat64(obj["op"])

			// 	pkp := helper.ToFloat64(obj["pkp"])

			// 	splits := helper.ToFloat64(obj["splits"])

			// 	pieces := helper.ToFloat64(obj["pieces"])

			// 	husk_powder := helper.ToString(obj["husk_powder"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type": "GRAD",
			// 		"template_id":  "Manual-GBC",
			// 		"BUDS":         inputValue,
			// 		"PIECES":       pieces,
			// 		"PKP":          pkp,
			// 		"OP":           op,
			// 		"SPLITS":       splits,
			// 		"HUSK_POWDER":  husk_powder,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }
			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	ssw := helper.ToFloat64(obj["ssw"])

			// 	dw := helper.ToFloat64(obj["dw"])

			// 	sw240 := helper.ToFloat64(obj["sw240"])

			// 	sw360 := helper.ToFloat64(obj["sw360"])

			// 	sw180 := helper.ToFloat64(obj["sw180"])

			// 	testa_unpeeled := helper.ToFloat64(obj["testa_unpeeled"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":   "GRAD",
			// 		"template_id":    "Manual-SWW",
			// 		"WHOLES":         inputValue,
			// 		"SW180":          sw180,
			// 		"SW240":          sw240,
			// 		"SW360":          sw360,
			// 		"SSW":            ssw,
			// 		"DW":             dw,
			// 		"TESTA UNPEELED": testa_unpeeled,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }
			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	op := helper.ToFloat64(obj["op"])

			// 	pkp := helper.ToFloat64(obj["pkp"])

			// 	swp := helper.ToFloat64(obj["swp"])

			// 	upp := helper.ToFloat64(obj["upp"])

			// 	s := helper.ToString(obj["s"])
			// 	sp := helper.ToString(obj["sp"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type": "GRAD",
			// 		"template_id":  "Manual-SWP",
			// 		"PIECES":       inputValue,
			// 		"PKP":          pkp,
			// 		"SWP":          swp,
			// 		"OP":           op,
			// 		"SP":           sp,
			// 		"S":            s,
			// 		"UPP":          upp,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }

			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	ssw := helper.ToFloat64(obj["ssw"])

			// 	w240 := helper.ToFloat64(obj["w240"])
			// 	w320 := helper.ToFloat64(obj["w320"])
			// 	w450 := helper.ToFloat64(obj["w450"])
			// 	w210 := helper.ToFloat64(obj["w210"])
			// 	w180 := helper.ToFloat64(obj["w180"])

			// 	sw240 := helper.ToFloat64(obj["sw240"])

			// 	sw360 := helper.ToFloat64(obj["sw360"])

			// 	sw180 := helper.ToFloat64(obj["sw180"])

			// 	testa_unpeeled := helper.ToFloat64(obj["testa_unpeeled"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":   "GRAD",
			// 		"template_id":    "Manual-GWW",
			// 		"WHOLES":         inputValue,
			// 		"W180":           w180,
			// 		"W210":           w210,
			// 		"W240":           w240,
			// 		"W320":           w320,
			// 		"W450":           w450,
			// 		"SW180":          sw180,
			// 		"SW240":          sw240,
			// 		"SW360":          sw360,
			// 		"SSW":            ssw,
			// 		"TESTA UNPEELED": testa_unpeeled,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }

			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	white_wholes := helper.ToFloat64(obj["white_wholes"])

			// 	sw_wholes := helper.ToFloat64(obj["sw_wholes"])
			// 	pkw := helper.ToFloat64(obj["pkw"])
			// 	buds := helper.ToFloat64(obj["buds_weight"])
			// 	s := helper.ToFloat64(obj["s"])
			// 	pieces := helper.ToFloat64(obj["pieces"])

			// 	dw := helper.ToFloat64(obj["dw"])

			// 	ow := helper.ToFloat64(obj["ow"])

			// 	husk := helper.ToFloat64(obj["husk"])

			// 	rejected := helper.ToFloat64(obj["rejected"])
			// 	upp := helper.ToFloat64(obj["upp"])

			// 	unpeeled_wholes := helper.ToFloat64(obj["unpeeled_wholes"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":    "GRAD",
			// 		"template_id":     "Manual-GSW",
			// 		"WHOLES":          inputValue,
			// 		"WHITE_WHOLES":    white_wholes,
			// 		"SW_WHOLES":       sw_wholes,
			// 		"PKW":             pkw,
			// 		"BUDS":            buds,
			// 		"S":               s,
			// 		"PIECES":          pieces,
			// 		"DW":              dw,
			// 		"OW":              ow,
			// 		"REJECTION":       rejected,
			// 		"UPP":             upp,
			// 		"HUSK":            husk,
			// 		"UNPEELED_WHOLES": unpeeled_wholes,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }

			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	jh := helper.ToFloat64(obj["jh"])

			// 	s := helper.ToFloat64(obj["s"])
			// 	ss := helper.ToFloat64(obj["ss"])
			// 	k := helper.ToFloat64(obj["k"])
			// 	sp := helper.ToFloat64(obj["sp"])
			// 	lwp := helper.ToFloat64(obj["lwp"])

			// 	swp := helper.ToFloat64(obj["swp"])

			// 	op := helper.ToFloat64(obj["op"])

			// 	pkp := helper.ToFloat64(obj["pkp"])

			// 	sps := helper.ToFloat64(obj["sps"])
			// 	buds := helper.ToFloat64(obj["buds"])
			// 	dust := helper.ToFloat64(obj["dust"])

			// 	unpeeled_pieces := helper.ToFloat64(obj["unpeeled_pieces"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":    "GRAD",
			// 		"template_id":     "Manual-GFP",
			// 		"PIECES":          inputValue,
			// 		"JH":              jh,
			// 		"S":               s,
			// 		"SS":              ss,
			// 		"K":               k,
			// 		"LWP":             lwp,
			// 		"SP":              sp,
			// 		"SWP":             swp,
			// 		"OP":              op,
			// 		"PKP":             pkp,
			// 		"SPS":             sps,
			// 		"BUDS":            buds,
			// 		"DUST":            dust,
			// 		"UNPEELED_PIECES": unpeeled_pieces,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }

			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	s := helper.ToFloat64(obj["s"])
			// 	upp := helper.ToFloat64(obj["upp"])
			// 	rejected := helper.ToFloat64(obj["rejected"])
			// 	lwp := helper.ToFloat64(obj["lwp"])

			// 	swp := helper.ToFloat64(obj["swp"])

			// 	op := helper.ToFloat64(obj["op"])

			// 	pkp := helper.ToFloat64(obj["pkp"])

			// 	sps := helper.ToFloat64(obj["sps"])
			// 	buds := helper.ToFloat64(obj["buds"])
			// 	husk := helper.ToFloat64(obj["husk"])

			// 	unpeeled_pieces := helper.ToFloat64(obj["unpeeled_pieces"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":    "GRAD",
			// 		"template_id":     "Manual-GSP",
			// 		"PIECES":          inputValue,
			// 		"PKP":             pkp,
			// 		"S":               s,
			// 		"LWP":             lwp,
			// 		"SWP":             swp,
			// 		"SPS":             sps,
			// 		"HUSK":            husk,
			// 		"OP":              op,
			// 		"UNPEELED_PIECES": unpeeled_pieces,
			// 		"BUDS":            buds,
			// 		"REJECTED":        rejected,
			// 		"UPP":             upp,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }

			// if helper.ToInt(obj["process_id"]) == 6 {
			// 	//output_weight
			// 	inputValue := helper.ToFloat64(obj["input_weight"])

			// 	// outputWeight := helper.ToFloat64(obj["output_weight"])

			// 	s := helper.ToFloat64(obj["s"])
			// 	ss := helper.ToFloat64(obj["ss"])
			// 	sp := helper.ToFloat64(obj["sp"])
			// 	jh := helper.ToFloat64(obj["jh"])
			// 	k := helper.ToFloat64(obj["k"])
			// 	lwp := helper.ToFloat64(obj["lwp"])

			// 	swp := helper.ToFloat64(obj["swp"])

			// 	op := helper.ToFloat64(obj["op"])

			// 	pkp := helper.ToFloat64(obj["pkp"])

			// 	sps := helper.ToFloat64(obj["sps"])
			// 	buds := helper.ToFloat64(obj["buds"])

			// 	unpeeled_pieces := helper.ToFloat64(obj["unpeeled_pieces"])
			// 	ID := helper.ToString(obj["_id"])
			// 	updateData := bson.M{
			// 		"process_type":    "GRAD",
			// 		"template_id":     "Manual-GFS",
			// 		"SPLITS":          inputValue,
			// 		"JH":              jh,
			// 		"S":               s,
			// 		"SS":              ss,
			// 		"K":               k,
			// 		"LWP":             lwp,
			// 		"SP":              sp,
			// 		"SWP":             swp,
			// 		"OP":              op,
			// 		"PKP":             pkp,
			// 		"SPS":             sps,
			// 		"BUDS":            buds,
			// 		"UNPEELED_PIECES": unpeeled_pieces,
			// 	}

			// 	res, err := database.GetConnection(orgId).
			// 		Collection("productions").
			// 		UpdateOne(
			// 			context.Background(),
			// 			bson.M{"_id": ID},
			// 			bson.M{"$set": updateData},
			// 		)

			// 	if err == nil && res.ModifiedCount > 0 {
			// 		updatedCount++
			// 	}
			// }
		}
	} else {
		fmt.Println("LEN IS ZERO ")
	}

	return c.JSON(fiber.Map{
		"status":        true,
		"updated_count": updatedCount,
	})
}

type ChildRule struct {
	Collection string // child collection name
	ForeignKey string // child field referencing parent
}

func CheckDelete(c *fiber.Ctx) error {
	var req struct {
		ParentCollection string      `json:"parent_collection"`
		ParentID         string      `json:"parent_id"`
		Children         []ChildRule `json:"children"`
	}

	// Parse JSON
	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest(err.Error())
	}

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// Call rule checker
	msg, err := CheckDeleteRule(
		org.Id,
		req.ParentCollection,
		req.ParentID,
		req.Children,
	)

	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, msg)

}

func CheckDeleteRule(orgID string, parentCollection string, parentID interface{}, children []ChildRule) (string, error) {

	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", parentID}}}},
	}

	// Build lookup + project dynamically
	projectFields := bson.D{{"_id", 1}}

	for _, child := range children {

		lookupName := child.Collection + "_result"

		// Lookup stage
		pipeline = append(pipeline, bson.D{
			{"$lookup",
				bson.D{
					{"from", child.Collection},
					{"localField", "_id"},
					{"foreignField", child.ForeignKey},
					{"as", lookupName},
				},
			},
		})

		// Boolean field
		projectFields = append(projectFields, bson.E{
			Key: "has_" + child.Collection,
			Value: bson.D{{"$gt", bson.A{
				bson.D{{"$size", "$" + lookupName}},
				0,
			}}},
		})
	}

	// Add project stage
	pipeline = append(pipeline, bson.D{{"$project", projectFields}})

	// Run aggregation
	result, err := helper.GetAggregateQueryResult(orgID, parentCollection, pipeline)
	if err != nil {
		return "", err
	}

	if len(result) == 0 {
		return "", fmt.Errorf("record not found")
	}

	doc := result[0]

	// Check boolean flags
	for _, child := range children {
		field := "has_" + child.Collection
		if exists, ok := doc[field].(bool); ok && exists {
			return "", fmt.Errorf("cannot delete: %s has child records in %s", parentCollection, child.Collection)
		}
	}

	return "can_delete", nil
}

func recalculateStockDetails(c *fiber.Ctx) error {
	var req map[string]interface{}
	var collectionName string
	var ID string
	// Parse JSON
	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest(err.Error())
	}
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}
	userToken := utils.GetUserTokenValue(c)

	if req["collectionName"] != "" {
		collectionName = req["collectionName"].(string)
	}

	if req["ID"] != "" {
		ID = req["ID"].(string)
	}

	// pipeline := bson.A{
	// 	bson.D{{"$match", bson.D{{"_id", ID}}}},
	// }

	pipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"purchase_id", ID},
					{"process_type", "COOL"},
				},
			},
		},
	}

	fmt.Println(ID, collectionName)
	data, err := helper.GetAggregateQueryResult(org.Id, collectionName, pipeline)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	if len(data) > 0 {
		for _, obj := range data {
			if collectionName == "invoice_details" {
				fmt.Println("log", obj)
				PurchaseLedgerUpdate(nil, "domestic", org, userToken.UserId, ID, collectionName)
			} else if collectionName == "productions" {

				if collectionName == "productions" && obj["other_worker_salary"] == nil {

					if obj["process_type"] == "PACK" {
						startSerialNo := helper.InterfaceToInt64(obj["start_serial_no"])
						endSerialNo := helper.InterfaceToInt64(obj["end_serial_no"])
						packingTypeData, _ := GetDataById(org.Id, obj["type_of_packing"].(string), "lookup")
						packingValue := helper.ToFloat64(packingTypeData["value"])
						for i := startSerialNo; i <= endSerialNo; i++ {
							facId := obj["factory_id"].(string)
							facPrefix := strings.ToUpper(facId[:3])
							seqData := "kernel-pack-" + facId
							seq, _ := helper.GetNextSeqNumber(seqData, org.Id)
							pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)
							serialData := map[string]interface{}{
								"_id":             pac,
								"s_no":            i,
								"status":          "packed",
								"production_id":   obj["_id"],
								"purchase_id":     obj["purchase_id"],
								"stock_from":      "production",
								"created_on":      time.Now(),
								"created_by":      userToken.UserId,
								"quantity":        packingValue,
								"product_id":      obj["product_id"],
								"type_of_packing": obj["type_of_packing"],
							}
							Insert(org.Id, "kernel_inventory", serialData)
						}

						ProductionKernelSTockInUpdate(org.Id, obj, userToken.UserId, false)
					}

					// ProductionProductLevelUpdates(org.Id, inputData, nil)

					//process operation ledger update

					// err := ProcessOperation(org.Id, inputData, userToken.UserId)
					// if err != nil {
					// 	// Log error but don't fail the main operation
					// 	log.Printf("ProcessOperation failed: %v", err)
					// }

					// Update production summary in background
					// go func() {
					// 	err := UpdateProductionSummary(org.Id, inputData)
					// 	if err != nil {
					// 		log.Printf("Failed to update production summary: %v", err)
					// 	}
					// }()

					// Update production stock in background
					if obj["process_type"] != "PACK" {
						insertedId := helper.ToString(obj["_id"])
						err := PostProductionStock(org.Id, insertedId, userToken.UserId, obj)
						if err != nil {
							log.Printf("Failed to post production stock: %v", err)
						}
					}

				}

			}

		}
	}
	return shared.SuccessResponse(c, data)

}

func CreateOrg(c *fiber.Ctx) error {
	// Get organization
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// collectionName := "organization"
	var inputData map[string]interface{}

	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	fmt.Println(org)

	return nil
}

func CreateServiceProvider(inputData map[string]interface{}, orgId string, ID string) error {
	inputData["parent_org"] = orgId
	_, err := database.GetConnection("shared").
		Collection("temporary_user").
		UpdateOne(
			context.Background(),
			bson.M{"_id": ID},
			bson.M{"$set": inputData},
			options.Update().SetUpsert(true),
		)
	toEmail := inputData["primary_contact_email"].(string)
	userName := inputData["primary_contact_name"].(string)
	SendOnBoardingMail("", userName, toEmail)

	return err
}

func SendOnBoardingMail(onboardingURL string, userName string, toEmail string) error {

	var template = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" 
  "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta http-equiv="X-UA-Compatible" content="IE=edge" />
  <title>Complete Your Onboarding</title>

  <link href="https://fonts.googleapis.com/css2?family=Imprima&display=swap" rel="stylesheet">

  <style>
    body {
      margin: 0;
      padding: 0;
      background-color: #f5f6f8;
      font-family: Imprima, Arial, sans-serif;
    }
    .container {
      max-width: 600px;
      margin: 40px auto;
      background-color: #ffffff;
      border-radius: 8px;
      padding: 40px;
    }
    .logo {
      text-align: center;
      margin-bottom: 20px;
    }
    .logo img {
      width: 100px;
    }
    h2 {
      color: #2D3142;
      text-align: center;
    }
    p {
      color: #2D3142;
      font-size: 16px;
      line-height: 1.6;
    }
    .info-box {
      background-color: #f4f4f4;
      padding: 15px;
      border-radius: 6px;
      margin: 20px 0;
    }
    .btn {
      display: inline-block;
      background-color: #2D3142;
      color: #ffffff !important;
      padding: 14px 28px;
      text-decoration: none;
      border-radius: 6px;
      font-size: 16px;
      font-weight: bold;
      margin-top: 20px;
    }
    .footer {
      margin-top: 30px;
      font-size: 14px;
      color: #777;
      text-align: center;
    }
  </style>
</head>

<body>
  <div class="container">

    <div class="logo">
      <img src="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/logo-1-removebg-preview__2025-07-23-14-55-30.png" alt="KajuPro Logo" />
    </div>

    <h2>You’ve Been Added as a Service Provider</h2>

    <p>Hello <strong>{{username}}</strong>,</p>

    <p>
      You have been added as a <strong>Service Provider</strong> in the
      <strong>KajuPro</strong> system.
    </p>

    <p>
      To activate your account, please complete your onboarding by setting
      your password and verifying your profile.
    </p>

    <div class="info-box">
      <p style="margin:0;">
        <strong>Username:</strong> {{username}}
      </p>
    </div>

    <div style="text-align:center;">
      <a href="{{onboarding_url}}" class="btn">
        Complete Onboarding
      </a>
    </div>

    <p style="margin-top:25px;">
      For security reasons, this onboarding link is valid for a limited time.
      Please complete the process as soon as possible.
    </p>

    <p>
      If you were not expecting this invitation, you can safely ignore this email
      or contact our support team.
    </p>

    <p>
      Thanks,<br />
      <strong>KajuPro Team</strong>
    </p>

    <div class="footer">
      © 2025 KajuPro. All rights reserved.<br />
      Need help? Contact support from your dashboard.
    </div>

  </div>
</body>
</html>

`

	// Replace placeholders
	template = strings.ReplaceAll(template, "{{username}}", userName)
	template = strings.ReplaceAll(template, "{{onboarding_url}}", onboardingURL)

	// Print or send the email
	// fmt.Println(onboardingTemplate)
	err := helper.SendEmailS(toEmail, os.Getenv("CLIENT_EMAIL"), "Onboarding", template)
	if err != nil {
		fmt.Println(err.Error(), "error")
		return err
	}
	return nil

}

func SendOnBoardingCompletedMail(loginURL string, userName string, toEmail string, password string) error {

	var template = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" 
  "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta http-equiv="X-UA-Compatible" content="IE=edge" />
  <title>Onboarding Completed</title>

  <link href="https://fonts.googleapis.com/css2?family=Imprima&display=swap" rel="stylesheet">

  <style>
    body {
      margin: 0;
      padding: 0;
      background-color: #f5f6f8;
      font-family: Imprima, Arial, sans-serif;
    }
    .container {
      max-width: 600px;
      margin: 40px auto;
      background-color: #ffffff;
      border-radius: 8px;
      padding: 40px;
    }
    .logo {
      text-align: center;
      margin-bottom: 20px;
    }
    .logo img {
      width: 100px;
    }
    h2 {
      color: #2D3142;
      text-align: center;
    }
    p {
      color: #2D3142;
      font-size: 16px;
      line-height: 1.6;
    }
    .info-box {
      background-color: #f4f4f4;
      padding: 15px;
      border-radius: 6px;
      margin: 20px 0;
    }
    .btn {
      display: inline-block;
      background-color: #2D3142;
      color: #ffffff !important;
      padding: 14px 28px;
      text-decoration: none;
      border-radius: 6px;
      font-size: 16px;
      font-weight: bold;
      margin-top: 20px;
    }
    .footer {
      margin-top: 30px;
      font-size: 14px;
      color: #777;
      text-align: center;
    }
  </style>
</head>

<body>
  <div class="container">

    <div class="logo">
      <img src="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/logo-1-removebg-preview__2025-07-23-14-55-30.png" alt="KajuPro Logo" />
    </div>

    <h2>Onboarding Completed Successfully 🎉</h2>

    <p>Hello <strong>{{username}}</strong>,</p>

    <p>
      Your onboarding process has been <strong>successfully completed</strong>.
      You can now log in to your account and start using the <strong>KajuPro</strong> platform.
    </p>

    <div class="info-box">
      <p style="margin:0;">
  <strong>User ID:</strong> {{username}}
</p>

<p style="margin:0;">
  <strong>Password:</strong> {{your_password}}
</p>
    </div>

    <div style="text-align:center;">
      <a href="{{login_url}}" class="btn">
        Log In to Your Account
      </a>
    </div>

    <p style="margin-top:25px;">
      If you experience any issues while logging in, please contact our support team for assistance.
    </p>

    <p>
      Welcome aboard! We’re glad to have you with us.
    </p>

    <p>
      Thanks,<br />
      <strong>KajuPro Team</strong>
    </p>

    <div class="footer">
      © 2025 KajuPro. All rights reserved.<br />
      Need help? Contact support from your dashboard.
    </div>

  </div>
</body>
</html>
`

	// Replace placeholders
	template = strings.ReplaceAll(template, "{{username}}", userName)
	template = strings.ReplaceAll(template, "{{login_url}}", loginURL)
	template = strings.ReplaceAll(template, "{{your_password}}", password)
	// Send email
	err := helper.SendEmailS(
		toEmail,
		os.Getenv("CLIENT_EMAIL"),
		"Onboarding Completed – You Can Now Log In",
		template,
	)
	if err != nil {
		fmt.Println(err.Error(), "error")
		return err
	}

	return nil
}

const (
	accountID  = "7fa08604e1e6fe6da2f5ce4d5b0e9f11"
	project    = "kajupro"
	apiToken   = "WJgDLZVOjDkJROPTwPJlOQfr8voykI32wBF-0bgS"
	apiBaseURL = "https://api.cloudflare.com/client/v4"
)

func CreateDomainAndSendOnboardingMail(emailId string, domainName string) error {
	err := CreatesubdomainAndSendMail(apiToken, accountID, project, domainName)
	if err != nil {
		return err
	}
	return nil
}

type DomainRequest struct {
	Name string `json:"name"`
}

type CloudflareResponse struct {
	Success bool        `json:"success"`
	Errors  []any       `json:"errors"`
	Result  interface{} `json:"result"`
}

func listPagesDomains(
	apiToken string,
	accountID string,
	project string,
) ([]string, error) {

	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/pages/projects/%s/domains",
		accountID,
		project,
	)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list domains failed: %s", body)
	}

	var res struct {
		Success bool `json:"success"`
		Result  []struct {
			Name string `json:"name"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	domains := make([]string, 0, len(res.Result))
	for _, d := range res.Result {
		domains = append(domains, d.Name)
	}

	return domains, nil
}

func CreatesubdomainAndSendMail(
	apiToken string,
	accountID string,
	project string,
	domain string,
) error {

	// Step 1: List existing domains
	existingDomains, err := listPagesDomains(apiToken, accountID, project)
	if err != nil {
		return err
	}
	extDomainName := domain + ".kajupro.com"
	// Step 2: Check if domain already exists
	for _, d := range existingDomains {
		if d == extDomainName {
			return fmt.Errorf("domain already exists: %s", domain)
		}
	}

	// Step 3: Create new domain
	cloudflareMethod.CreateDNSRecord(domain)

	return nil
}

func MigrateDB(orgId string, roleId string) {
	dbName := strings.ToLower(orgId)
	parentOrgId := "604162a4ce67408c8b22870191199ad4"
	database.CreateNewMongoDatabase(dbName, orgId)
	CopyMultipleCollections(parentOrgId, orgId, false, roleId)
}

func ExcelUploadToProductions(c *fiber.Ctx) error {

	var req []map[string]interface{}

	err := c.BodyParser(&req)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	userToken := utils.GetUserTokenValue(c)
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	for _, obj := range req {
		obj["created_on"] = time.Now()
		productId := obj["product_id"].(string)
		product, err := GetDataById(org.Id, productId, "product")
		if err != nil {
			continue
		}
		templateId := product["template_id"].(string)
		obj["template_id"] = templateId
		helper.InsertData(c, org.Id, "productions", obj)

		// if obj["process_type"] == "PACK" {
		// 	startSerialNo := helper.InterfaceToInt64(obj["start_serial_no"])
		// 	endSerialNo := helper.InterfaceToInt64(obj["end_serial_no"])
		// 	packingTypeData, _ := GetDataById(org.Id, obj["type_of_packing"].(string), "lookup")
		// 	packingValue := helper.ToFloat64(packingTypeData["value"])
		// 	for i := startSerialNo; i <= endSerialNo; i++ {
		// 		facId := obj["factory_id"].(string)
		// 		facPrefix := strings.ToUpper(facId[:3])
		// 		seqData := "kernel-pack-" + facId
		// 		seq, _ := helper.GetNextSeqNumber(seqData, org.Id)
		// 		pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)

		// 		serialData := map[string]interface{}{
		// 			"_id":             pac,
		// 			"s_no":            i,
		// 			"status":          "packed",
		// 			"production_id":   obj["_id"],
		// 			"purchase_id":     obj["purchase_id"],
		// 			"stock_from":      "production",
		// 			"created_on":      time.Now(),
		// 			"created_by":      userToken.UserId,
		// 			"quantity":        packingValue,
		// 			"product_id":      obj["product_id"],
		// 			"type_of_packing": obj["type_of_packing"],
		// 		}
		// 		Insert(org.Id, "kernel_inventory", serialData)
		// 	}

		// 	ProductionKernelSTockInUpdate(org.Id, obj, userToken.UserId)
		// }
		if obj["process_type"] == "PACK" {

			serialInput := helper.ToString(obj["serial_no"])
			serialNumbers := ExpandSerialNumbers(serialInput)

			packingTypeData, _ := GetDataById(org.Id, obj["type_of_packing"].(string), "lookup")
			packingValue := helper.ToFloat64(packingTypeData["value"])

			for _, i := range serialNumbers {

				facId := obj["factory_id"].(string)
				facPrefix := strings.ToUpper(facId[:3])
				seqData := "kernel-pack-" + facId
				seq, _ := helper.GetNextSeqNumber(seqData, org.Id)
				pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)

				serialData := map[string]interface{}{
					"_id":             pac,
					"s_no":            i,
					"status":          "packed",
					"production_id":   obj["_id"],
					"purchase_id":     obj["purchase_id"],
					"stock_from":      "production",
					"created_on":      time.Now(),
					"created_by":      userToken.UserId,
					"quantity":        packingValue,
					"product_id":      obj["product_id"],
					"type_of_packing": obj["type_of_packing"],
				}

				Insert(org.Id, "kernel_inventory", serialData)
			}

			ProductionKernelSTockInUpdate(org.Id, obj, userToken.UserId, true)
		}

		// Update production stock in background
		if obj["process_type"] != "PACK" {
			insertedId := helper.ToString(obj["_id"])
			err := PostProductionStock(org.Id, insertedId, userToken.UserId, obj)
			if err != nil {
				log.Printf("Failed to post production stock: %v", err)
			}
		}

	}

	return shared.SuccessResponse(c, nil)

}

func ExpandSerialNumbers(input string) []int64 {
	var result []int64

	parts := strings.Split(input, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				var start, end int64

				_, err1 := fmt.Sscan(rangeParts[0], &start)
				_, err2 := fmt.Sscan(rangeParts[1], &end)

				if err1 == nil && err2 == nil && end >= start {
					for i := start; i <= end; i++ {
						result = append(result, i)
					}
				}
			}
		} else {
			var num int64
			_, err := fmt.Sscan(part, &num)
			if err == nil {
				result = append(result, num)
			}
		}
	}

	return result
}
func UpdateProductField(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	db := database.GetConnection(org.Id)
	collection := db.Collection("process_product")

	ctx := context.TODO()

	buildPipeline := func(value int) bson.A {
		return bson.A{
			bson.D{
				{"$set",
					bson.D{
						{"dynamicField",
							bson.D{
								{"$arrayToObject",
									bson.A{
										bson.A{
											bson.D{
												{"k", "$product_id"},
												{"v", value},
											},
										},
									},
								},
							},
						},
						{"expression", value},
					},
				},
			},
			bson.D{
				{"$replaceRoot",
					bson.D{
						{"newRoot",
							bson.D{
								{"$mergeObjects",
									bson.A{
										"$$ROOT",
										"$dynamicField",
									},
								},
							},
						},
					},
				},
			},
			bson.D{{"$unset", "dynamicField"}},
		}
	}

	// input → -1
	_, err := collection.UpdateMany(
		ctx,
		bson.M{"type": "input"},
		buildPipeline(-1),
	)
	if err != nil {
		return err
	}

	// output → 1
	_, err = collection.UpdateMany(
		ctx,
		bson.M{"type": "output"},
		buildPipeline(1),
	)
	if err != nil {
		return err
	}

	// ignore_stock → 0
	_, err = collection.UpdateMany(
		ctx,
		bson.M{"ignore_stock": true},
		buildPipeline(0),
	)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"status":  "success",
		"message": "Product fields updated",
	})
}
func EmployeeBulkPostHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	var inputs []map[string]interface{}
	if err := c.BodyParser(&inputs); err != nil || len(inputs) == 0 {
		return shared.BadRequest("Invalid or empty request body")
	}

	db := database.GetConnection(org.Id)
	collectionName := "employee"
	col := db.Collection(collectionName)
	var duplicateDBRecords []map[string]interface{}

	models, validItems := BuildEmployeeBulkOps(inputs, &duplicateDBRecords)
	if collectionName == "employee" && len(validItems) > 0 {

		var emails []string
		for _, item := range validItems {
			if email, ok := item["email"].(string); ok && email != "" {
				emails = append(emails, strings.ToLower(strings.TrimSpace(email)))
			}
		}

		// query DB
		filter := bson.M{
			"email": bson.M{"$in": emails},
		}

		cursor, err := col.Find(context.Background(), filter)
		if err != nil {
			return shared.BadRequest(err.Error())
		}

		var existing []bson.M
		if err := cursor.All(context.Background(), &existing); err != nil {
			return shared.BadRequest(err.Error())
		}

		existingEmailMap := make(map[string]bool)
		for _, rec := range existing {
			if email, ok := rec["email"].(string); ok {
				existingEmailMap[strings.ToLower(email)] = true
			}
		}

		var filteredModels []mongo.WriteModel
		var filteredValidItems []map[string]interface{}

		for i, item := range validItems {

			email, _ := item["email"].(string)
			email = strings.ToLower(strings.TrimSpace(email))

			if existingEmailMap[email] {
				item["error"] = "Email already exists in DB"
				duplicateDBRecords = append(duplicateDBRecords, item)
				continue
			}

			filteredModels = append(filteredModels, models[i])
			filteredValidItems = append(filteredValidItems, item)
		}

		models = filteredModels
		validItems = filteredValidItems
	}
	if len(models) == 0 {

		if len(duplicateDBRecords) > 0 {
			return shared.SuccessResponse(c, fiber.Map{
				"message":           fmt.Sprintf("%s Bulk upload completed 0 succeeded, %d failed", collectionName, len(inputs)),
				"total":             len(inputs),
				"success":           0,
				"failed":            len(inputs),
				"duplicate_records": duplicateDBRecords,
			})
		}

		return shared.BadRequest("No valid records")
	}

	opts := options.BulkWrite().SetOrdered(false)

	result, err := col.BulkWrite(context.Background(), models, opts)

	if err != nil {
		if writeErr, ok := err.(mongo.BulkWriteException); ok {

			for _, e := range writeErr.WriteErrors {

				if e.Code == 11000 { // duplicate key

					idx := e.Index

					if idx < len(validItems) {
						duplicateDBRecords = append(duplicateDBRecords, validItems[idx])
					}
				}
			}

		} else {
			return shared.BadRequest(err.Error())
		}
	}

	successCount := int(result.InsertedCount)
	failedCount := len(inputs) - successCount

	return shared.SuccessResponse(c, fiber.Map{
		"message":           fmt.Sprintf("%s Bulk upload completed %d succeeded, %d failed", collectionName, successCount, failedCount),
		"total":             len(inputs),
		"success":           successCount,
		"failed":            failedCount,
		"duplicate_records": duplicateDBRecords,
	})
}
func BuildEmployeeBulkOps(
	inputs []map[string]interface{},
	duplicateDBRecords *[]map[string]interface{},
) ([]mongo.WriteModel, []map[string]interface{}) {

	var models []mongo.WriteModel
	var validItems []map[string]interface{}

	recordMap := make(map[string]bool)
	emailMap := make(map[string]bool)
	for _, item := range inputs {

		id, ok := item["_id"].(string)
		if !ok || id == "" {
			continue
		}
		email, _ := item["email"].(string)
		email = strings.ToLower(strings.TrimSpace(email))
		if recordMap[id] || emailMap[email] {
			*duplicateDBRecords = append(*duplicateDBRecords, item)
			continue
		}

		recordMap[id] = true

		helper.UpdateDateObject(item)

		model := mongo.NewInsertOneModel().SetDocument(item)

		models = append(models, model)
		validItems = append(validItems, item)
	}

	return models, validItems
}
func EquipmentBulkPostHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	var inputs []map[string]interface{}
	if err := c.BodyParser(&inputs); err != nil || len(inputs) == 0 {
		return shared.BadRequest("Invalid or empty request body")
	}

	db := database.GetConnection(org.Id)

	equipmentCol := db.Collection("equipments")
	maintenanceCol := db.Collection("maintance_details")

	var equipmentDocs []interface{}
	var maintenanceDocs []interface{}
	var duplicateRecords []map[string]interface{}

	for _, item := range inputs {
		facId, ok := item["factory"].(string)
		if !ok || len(facId) < 3 {
			continue
		}

		// prefix := facId + "EQU-" + time.Now().Format("2006") + "-"
		maintenanceArr, _ := item["maintenance_details"].([]interface{})
		delete(item, "maintance_details")

		// ✅ Generate ID safely
		// seq, err := helper.GetNextSeqNumber(prefix, org.Id)
		// if err != nil {
		// 	continue
		// }

		// equipmentID := fmt.Sprintf("%s%03d", prefix, seq)

		// item["_id"] = equipmentID
		item["org_id"] = org.Id
		item["created_on"] = time.Now()
		helper.UpdateDateObject(item)

		equipmentDocs = append(equipmentDocs, item)

		for _, m := range maintenanceArr {

			mMap, ok := m.(map[string]interface{})
			if !ok {
				continue
			}

			mID := uuid.New().String()

			mMap["_id"] = mID
			// mMap["equipment_id"] = equipmentID
			mMap["org_id"] = org.Id
			mMap["created_on"] = time.Now()

			helper.UpdateDateObject(mMap)

			maintenanceDocs = append(maintenanceDocs, mMap)
		}
	}

	if len(equipmentDocs) == 0 {
		return shared.BadRequest("No valid records")
	}

	var models []mongo.WriteModel
	for _, doc := range equipmentDocs {
		models = append(models, mongo.NewInsertOneModel().SetDocument(doc))
	}

	opts := options.BulkWrite().SetOrdered(false)

	result, err := equipmentCol.BulkWrite(context.Background(), models, opts)

	failedIndexes := map[int]bool{}

	if err != nil {
		if bulkErr, ok := err.(mongo.BulkWriteException); ok {

			for _, e := range bulkErr.WriteErrors {

				failedIndexes[e.Index] = true

				if e.Code == 11000 {
					if e.Index < len(equipmentDocs) {
						if rec, ok := equipmentDocs[e.Index].(map[string]interface{}); ok {
							duplicateRecords = append(duplicateRecords, rec)
						}
					}
				}
			}

		} else {
			return shared.BadRequest(err.Error())
		}
	}

	var finalMaintenanceDocs []interface{}

	for i, doc := range maintenanceDocs {

		if failedIndexes[i] {
			continue
		}
		finalMaintenanceDocs = append(finalMaintenanceDocs, doc)
	}

	if len(finalMaintenanceDocs) > 0 {
		_, err = maintenanceCol.InsertMany(context.Background(), finalMaintenanceDocs)
		if err != nil {
			return shared.BadRequest("Maintenance insert failed: " + err.Error())
		}
	}

	for _, m := range finalMaintenanceDocs {

		mMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}

		maintenanceID, ok := mMap["_id"].(string)
		if !ok {
			continue
		}

		parentId := maintenanceID

		err := helper.GenerateMultipleMaintenanceData(
			mMap,
			org.Id,
			parentId,
			nil,
			true,
		)
		if err != nil {
			return shared.BadRequest("Failed to generate maintenance data: " + err.Error())
		}
	}

	successCount := int(result.InsertedCount)
	failedCount := len(failedIndexes)

	return shared.SuccessResponse(c, fiber.Map{
		"message":           fmt.Sprintf("Equipment bulk upload completed %d succeeded, %d failed", successCount, failedCount),
		"total":             len(inputs),
		"success":           successCount,
		"failed":            failedCount,
		"duplicate_records": duplicateRecords,
	})
}
func processFactory(factory map[string]interface{}) map[string]interface{} {

	aggrid := factory["aggrid_factory_processes"].(map[string]interface{})

	processList, _ := aggrid["factory_processes"].([]interface{})
	empMap, _ := aggrid["no_of_Employee"].(map[string]interface{})
	equipMap, _ := aggrid["equipment_count"].(map[string]interface{})

	finalEmp := make(map[string]interface{})
	finalEquip := make(map[string]interface{})
	processSet := make(map[string]bool)

	for _, p := range processList {
		proc := p.(string)

		// Normalize
		if proc == "MC-SH" || proc == "ML-SH" {
			proc = "SHELL"
		}

		processSet[proc] = true

		if val, ok := empMap[p.(string)].(float64); ok {
			current := 0
			if existing, ok := finalEmp[proc].(int); ok {
				current = existing
			}
			finalEmp[proc] = current + int(val)
		}

		if val, ok := equipMap[p.(string)].(float64); ok {
			current := 0
			if existing, ok := finalEquip[proc].(int); ok {
				current = existing
			}
			finalEquip[proc] = current + int(val)
		}
	}

	// Convert process set → slice
	var finalProcesses []string
	for k := range processSet {
		finalProcesses = append(finalProcesses, k)
	}

	return map[string]interface{}{
		"factory_name":      factory["factory_name"],
		"factory_address":   factory["factory_address"],
		"factory_contact":   factory["factory_contact"],
		"factory_processes": finalProcesses,
		"no_of_Employee":    finalEmp,
		"equipment_count":   finalEquip,
	}
}
func generateSampleData(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Org not found")
	}

	var payload map[string]interface{}
	if err := c.BodyParser(&payload); err != nil {
		return shared.BadRequest("Invalid payload")
	}

	dbName := strings.ToLower(org.Id + "_demo")

	database.CreateNewMongoDatabase(dbName, dbName)
	err := CreateOrgDemo(org.Id, payload)
	if err != nil {
		return shared.InternalServerError("Failed to create demo org")
	}

	err = CopyMultipleCollections(org.Id, dbName, false, "")
	if err != nil {
		return shared.InternalServerError("Failed to copy collections")
	}
	factoriesData, ok := payload["factories"].([]interface{})
	warehouseData, _ := payload["warehouse"].([]interface{})
	if !ok || len(factoriesData) == 0 {
		return shared.BadRequest("Invalid factories array")
	}

	var finalFactories []map[string]interface{}

	for _, f := range factoriesData {
		if factory, ok := f.(map[string]interface{}); ok {
			finalFactories = append(finalFactories, processFactory(factory))
		}
	}
	var finalWarehouses []map[string]interface{}

	for _, w := range warehouseData {
		if wh, ok := w.(map[string]interface{}); ok {
			finalWarehouses = append(finalWarehouses, wh)
		}
	}

	sampleData.GenerateSampleFactory(finalFactories, finalWarehouses, dbName, org.Id)
	// var totalShellingRecords int
	res, err := sampleData.GenerateSampleDomesticPurchase(3, dbName)
	if err != nil {
		log.Println(err)
		return shared.InternalServerError("Failed to generate purchase data")
	}
	consignmentCol := database.GetConnection(dbName).Collection("consignment_status")
	productionCol := database.GetConnection(dbName).Collection("productions")

	factory_processCol := database.GetConnection(dbName).Collection("factory_process")
	stockin_hand := database.GetConnection(dbName).Collection("stock_in_hand")
	for _, item := range res {

		doc, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		orgID, _ := doc["org_id"].(string)

		org := helper.Organization{
			Id: orgID,
		}
		purchase_ID, _ := doc["purchase_id"].(string)
		var docID string
		switch v := doc["_id"].(type) {
		case string:
			docID = v
		case primitive.ObjectID:
			docID = v.Hex()
		}

		if err := PurchaseLedgerUpdate(
			nil,
			"domestic",
			org,
			"LV",
			docID,
			"invoice_details",
		); err != nil {
			return err
		}
		warehouseID, _ := doc["warehouse_id"].(string)
		data, err := GetDataById(orgID, warehouseID, "company")
		if err != nil {
			fmt.Println("Error fetching:", err)
			continue
		}
		factoryId, _ := data["factory_id"].(string)
		factoryFilter := bson.M{
			"factory_id": factoryId,
		}

		cursor, err := factory_processCol.Find(ctx, factoryFilter)
		if err != nil {
			fmt.Println("Error fetching:", err)
			continue
		}
		defer cursor.Close(ctx)

		processMap := make(map[string]bool)

		for cursor.Next(ctx) {

			var proc map[string]interface{}

			// ✅ Decode directly into map
			if err := cursor.Decode(&proc); err != nil {
				continue
			}

			id, ok := proc["process_id"].(string)
			if !ok {
				continue
			}

			processMap[id] = true
		}
		consignment := map[string]interface{}{
			"_id":          uuid.New().String(),
			"created_by":   "LV",
			"created_on":   time.Now(),
			"factory_id":   factoryId,
			"purchase_id":  doc["purchase_id"],
			"start_date":   time.Now(),
			"start_remark": "auto",
			"status":       "In progress",
			"warehouse_id": warehouseID,
		}

		_, err = consignmentCol.InsertOne(context.Background(), consignment)
		if err != nil {
			fmt.Println("Insert error:", err)
			continue
		}
		invoiceData := []interface{}{doc}
		if processMap["COOK"] {
			result, err := sampleData.SampleCookingData(factoryId, invoiceData, dbName)
			if err != nil {
				return err
			}

			var validData []interface{}
			var failed []string

			for _, item := range result {

				doc, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				err := PostProductionStock(dbName, doc["_id"].(string), "LV", doc)

				if err != nil {
					fmt.Println("Validation failed:", err)
					failed = append(failed, fmt.Sprintf("%v", doc["_id"]))
					continue
				}

				validData = append(validData, doc)

			}

			if len(validData) > 0 {
				_, err = productionCol.InsertMany(context.Background(), validData)
				if err != nil {
					fmt.Println("Insert error:", err)
				}
			}
		}
		if processMap["MC-SH"] || processMap["ML-SH"] {
			filter := bson.M{
				"process_type":  "COOK",
				"product_id":    "STEAMEDRCN",
				"purchase_id":   doc["purchase_id"],
				"available_qty": bson.M{"$gt": 0}}
			cursor, err := stockin_hand.Find(ctx, filter)
			if err != nil {
				fmt.Println("Error fetching:", err)
				continue
			}

			var stocks []interface{}

			for cursor.Next(ctx) {
				var doc map[string]interface{}

				if err := cursor.Decode(&doc); err != nil {
					fmt.Println("Decode error:", err)
					continue
				}

				stocks = append(stocks, doc)
			}

			cursor.Close(ctx)

			shellingResult, err := sampleData.SampleShellingData(factoryId, stocks, dbName)
			if err != nil {
				fmt.Println("Error generating shelling data:", err)
				continue
			}
			ProcessAndInsertProduction(dbName, productionCol, shellingResult, "Shelling")
		}
		if processMap["BORM"] {
			bormaFilter := bson.M{
				"process_type":  "SHELL",
				"purchase_id":   doc["purchase_id"],
				"available_qty": bson.M{"$gt": 0}}
			bormares, err := stockin_hand.Find(ctx, bormaFilter)
			if err != nil {
				fmt.Println("Error fetching:", err)
				continue
			}

			var bormastock []interface{}

			for bormares.Next(ctx) {
				var doc map[string]interface{}

				if err := bormares.Decode(&doc); err != nil {
					fmt.Println("Decode error:", err)
					continue
				}

				bormastock = append(bormastock, doc)
			}
			var totalWholes float64
			var totalPieces float64
			var bormawarehouseID, purchaseID string

			for _, item := range bormastock {
				doc := item.(map[string]interface{})

				product := fmt.Sprintf("%v", doc["product_id"])

				qty := toFloat(doc["available_qty"])

				if product == "SH_WHOLES" {
					totalWholes += qty
				} else if product == "SH_PIECES" {
					totalPieces += qty
				}

				bormawarehouseID = fmt.Sprintf("%v", doc["warehouse_id"])
				purchaseID = fmt.Sprintf("%v", doc["purchase_id"])
			}
			bormaInput := []interface{}{
				map[string]interface{}{
					"SH_WHOLES":    totalWholes,
					"SH_PIECES":    totalPieces,
					"warehouse_id": bormawarehouseID,
					"purchase_id":  purchaseID,
				},
			}
			bormares.Close(ctx)

			bormaResult, err := sampleData.SampleBormaData(factoryId, bormaInput, dbName)
			ProcessAndInsertProduction(dbName, productionCol, bormaResult, "Borma")
		}
		if processMap["COOL"] {
			coolFilter := bson.M{
				"process_type":  "BORM",
				"purchase_id":   doc["purchase_id"],
				"available_qty": bson.M{"$gt": 0},
			}

			coolres, err := stockin_hand.Find(ctx, coolFilter)
			if err != nil {
				fmt.Println("Error fetching:", err)
				continue
			}

			var coolstock []interface{}

			for coolres.Next(ctx) {
				var doc map[string]interface{}

				if err := coolres.Decode(&doc); err != nil {
					fmt.Println("Decode error:", err)
					continue
				}

				coolstock = append(coolstock, doc)
			}

			coolres.Close(ctx)
			var totalBRWholes float64
			var totalBRPieces float64
			var coolWarehouseID, cooolpurchaseID string

			for _, item := range coolstock {
				doc := item.(map[string]interface{})

				product := fmt.Sprintf("%v", doc["product_id"])
				qty := toFloat(doc["available_qty"])

				if product == "BR_WHOLES" {
					totalBRWholes += qty
				} else if product == "BR_PIECES" {
					totalBRPieces += qty
				}

				coolWarehouseID = fmt.Sprintf("%v", doc["warehouse_id"])
				cooolpurchaseID = fmt.Sprintf("%v", doc["purchase_id"])
			}
			coolInput := []interface{}{
				map[string]interface{}{
					"BR_WHOLES":    totalBRWholes,
					"BR_PIECES":    totalBRPieces,
					"warehouse_id": coolWarehouseID,
					"purchase_id":  cooolpurchaseID,
				},
			}
			coolResult, err := sampleData.SampleCoolingData(factoryId, coolInput, dbName)
			if err != nil {
				fmt.Println("Cooling error:", err)
				continue
			}
			ProcessAndInsertProduction(dbName, productionCol, coolResult, "Cooling")
		}
		if processMap["PEEL"] {
			peelFilter := bson.M{
				"process_type":  "COOL",
				"purchase_id":   doc["purchase_id"],
				"available_qty": bson.M{"$gt": 0},
			}

			peelRes, err := stockin_hand.Find(ctx, peelFilter)
			if err != nil {
				fmt.Println("Error fetching:", err)
				continue
			}

			var peelStock []interface{}

			for peelRes.Next(ctx) {
				var doc map[string]interface{}

				if err := peelRes.Decode(&doc); err != nil {
					fmt.Println("Decode error:", err)
					continue
				}

				peelStock = append(peelStock, doc)
			}

			peelRes.Close(ctx)
			var totalCLWholes float64
			var totalCLPieces float64
			var peelWarehouseID, peelPurchaseID string

			for _, item := range peelStock {
				doc := item.(map[string]interface{})

				product := fmt.Sprintf("%v", doc["product_id"])
				qty := toFloat(doc["available_qty"])

				if product == "CL_WHOLES" {
					totalCLWholes += qty
				} else if product == "CL_PIECES" {
					totalCLPieces += qty
				}

				peelWarehouseID = fmt.Sprintf("%v", doc["warehouse_id"])
				peelPurchaseID = fmt.Sprintf("%v", doc["purchase_id"])
			}
			peelInput := []interface{}{
				map[string]interface{}{
					"CL_WHOLES":    totalCLWholes,
					"CL_PIECES":    totalCLPieces,
					"warehouse_id": peelWarehouseID,
					"purchase_id":  peelPurchaseID,
				},
			}
			peelResult, err := sampleData.SamplePeelingDataFinal(factoryId, peelInput, dbName)
			if err != nil {
				fmt.Println("Peeling error:", err)
				continue
			}
			ProcessAndInsertProduction(dbName, productionCol, peelResult, "Peeling")
		}
		if processMap["GRAD"] {

			gradFilter := bson.M{
				"process_type":  "PEEL",
				"purchase_id":   doc["purchase_id"],
				"available_qty": bson.M{"$gt": 0},
			}

			cursor, err := stockin_hand.Find(ctx, gradFilter)
			if err != nil {
				fmt.Println("Error fetching:", err)
				continue
			}
			defer cursor.Close(ctx)

			totals := make(map[string]float64)

			for cursor.Next(ctx) {

				var record map[string]interface{}
				if err := cursor.Decode(&record); err != nil {
					continue
				}

				productID, _ := record["product_id"].(string)
				qty := toFloat(record["available_qty"])

				totals[productID] += qty
			}

			totalPLWholes := totals["PL_WHOLES"]
			totalPLAllPiece := totals["PL_ALL_PIECE"]

			gradingInput := []interface{}{
				map[string]interface{}{
					"PL_WHOLES":    totalPLWholes,
					"PL_ALL_PIECE": totalPLAllPiece,
					"warehouse_id": doc["warehouse_id"],
					"purchase_id":  doc["purchase_id"],
				},
			}

			gradingResult, err := sampleData.SampleGradingDataFinal(factoryId, gradingInput, dbName)
			if err != nil {
				fmt.Println("Grading error:", err)
				continue
			}

			ProcessAndInsertProduction(dbName, productionCol, gradingResult, "Grading")
		}
		if processMap["PACK"] {
			packFilter := bson.M{
				"process_type":  "GRAD",
				"purchase_id":   doc["purchase_id"],
				"available_qty": bson.M{"$gt": 0},
				"product_id": bson.M{
					"$in": bson.A{"W210", "PKP", "SW210"},
				},
			}

			gradingres, err := stockin_hand.Find(ctx, packFilter)
			if err != nil {
				fmt.Println("Error fetching:", err)
				return err
			}

			// var packingFinal []interface{}

			for gradingres.Next(ctx) {

				var item map[string]interface{}
				if err := gradingres.Decode(&item); err != nil {
					fmt.Println("Decode error:", err)
					continue
				}

				packingInput := []map[string]interface{}{
					{
						"product_id":    item["product_id"],
						"available_qty": item["available_qty"],
						"warehouse_id":  item["warehouse_id"],
						"purchase_id":   item["purchase_id"],
					},
				}

				packingResult, err := sampleData.SamplePackingData(factoryId, packingInput, dbName)
				if err != nil {
					fmt.Println("Packing error:", err)
					continue
				}

				for _, p := range packingResult {

					packingMap := p.(map[string]interface{})
					production_id, _ := packingMap["_id"].(string)
					startSerialNo := helper.InterfaceToInt64(packingMap["start_serial_no"])
					endSerialNo := helper.InterfaceToInt64(packingMap["end_serial_no"])

					packingTypeData, _ := GetDataById(dbName, packingMap["type_of_packing"].(string), "lookup")
					packingValue := helper.ToFloat64(packingTypeData["value"])

					for i := startSerialNo; i <= endSerialNo; i++ {

						facId := packingMap["factory_id"].(string)
						facPrefix := strings.ToUpper(facId[:3])

						seqData := "kernel-pack-" + facId
						seq, _ := helper.GetNextSeqNumber(seqData, dbName)

						pac := "PAC-KER-" + facPrefix + "-" + helper.ToString(seq)

						serialData := map[string]interface{}{
							"_id":             pac,
							"s_no":            i,
							"status":          "packed",
							"production_id":   production_id,
							"purchase_id":     packingMap["purchase_id"],
							"stock_from":      "production",
							"warehouse_id":    packingMap["warehouse_id"],
							"created_on":      time.Now(),
							"created_by":      "LV",
							"quantity":        packingValue,
							"product_id":      packingMap["product_id"],
							"type_of_packing": packingMap["type_of_packing"],
						}

						Insert(dbName, "kernel_inventory", serialData)
					}

					ProductionKernelSTockInUpdate(dbName, packingMap, "LV", false)
					ProcessAndInsertProduction(dbName, productionCol, packingResult, "PACKING")

					// packingFinal = append(packingFinal, packingMap)
				}
			}
			if err := gradingres.Err(); err != nil {
				fmt.Println("Cursor error:", err)
			}

		}
		_, err = consignmentCol.UpdateOne(
			ctx,
			bson.M{
				"purchase_id": purchase_ID,
			},
			bson.M{
				"$set": bson.M{
					"status": "Closed",
				},
			},
		)
		if err != nil {
			return err
		}

	}
	collection := database.GetConnection("shared").Collection("organization")

	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": org.Id},
		bson.M{
			"$set": bson.M{
				"firstLogin": false,
			},
		},
	)
	if err != nil {
		fmt.Println("Failed to update firstLogin:", err)

	}
	stock_filter := mongo.Pipeline{

		{{"$match", bson.D{
			{"status", "packed"},
			{"product_id", "W210"},
		}}},

		{{"$lookup", bson.D{
			{"from", "lookup"},
			{"localField", "type_of_packing"},
			{"foreignField", "_id"},
			{"as", "result"},
		}}},

		{{"$lookup", bson.D{
			{"from", "purchase"},
			{"localField", "purchase_id"},
			{"foreignField", "_id"},
			{"as", "orign"},
		}}},

		{{"$lookup", bson.D{
			{"from", "origin"},
			{"localField", "origin_id"},
			{"foreignField", "_id"},
			{"as", "originData"},
		}}},

		{{"$unwind", bson.D{
			{"path", "$originData"},
			{"preserveNullAndEmptyArrays", true},
		}}},

		{{"$addFields", bson.D{
			{"tin_name", bson.D{
				{"$cond", bson.A{
					bson.D{{"$eq", bson.A{"$stock_from", "production"}}},
					bson.D{{"$arrayElemAt", bson.A{"$result.name", 0}}},
					"Tin",
				}},
			}},
			{"orgin_name", bson.D{
				{"$cond", bson.A{
					bson.D{{"$eq", bson.A{"$stock_from", "production"}}},
					bson.D{{"$arrayElemAt", bson.A{"$orign.country_origin", 0}}},
					bson.D{{"$ifNull", bson.A{"$originData.name", ""}}},
				}},
			}},
			{"total_weight", bson.D{
				{"$cond", bson.A{
					bson.D{{"$eq", bson.A{"$stock_from", "production"}}},
					bson.D{{"$multiply", bson.A{"$quantity", 1}}},
					"$quantity",
				}},
			}},
		}}},

		{{"$unset", bson.A{"result", "orign"}}},

		{{"$group", bson.D{
			{"_id", bson.D{
				{"product_id", "$product_id"},
				{"tin_name", "$tin_name"},
				{"orgin_name", "$orgin_name"},
				{"purchase_id", "$purchase_id"}, {"warehouse_id", "$warehouse_id"},
			}},
			{"total_quantity", bson.D{{"$first", "$quantity"}}},
			{"total_filled_tins", bson.D{{"$sum", 1}}},
			{"total_weight", bson.D{{"$sum", "$total_weight"}}},
			{"origin_id", bson.D{{"$first", "$origin_id"}}},
		}}},

		{{"$project", bson.D{
			{"_id", "$_id.product_id"},
			{"product_id", "$_id.product_id"},
			{"tin_name", "$_id.tin_name"},
			{"orgin_name", "$_id.orgin_name"},
			{"purchase_id", "$_id.purchase_id"},
			{"warehouse_id", "$_id.warehouse_id"},

			{"total_quantity", 1},
			{"total_weight", 1},

			{"total_filled_tins", 1},
			{"origin_id", 1},
		}}},
	}
	cursor, err := database.GetConnection(dbName).
		Collection("kernel_inventory").
		Aggregate(ctx, stock_filter)

	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err := cursor.All(ctx, &results); err != nil {
		return err
	}
	for _, stock := range results {
		err = sampleSales(dbName, "LV", stock)
		if err != nil {
			return err
		}
	}

	fmt.Println(res)

	// Create demo user for login
	fmt.Printf("[Sample Data] Creating demo user for database: %s\n", dbName)
	// err = CreateDemoUser(dbName, org.Id, payload)
	if err != nil {
		fmt.Printf("[Sample Data Error] Failed to create demo user: %v\n", err)
		// Don't fail the whole process, just log the error
	} else {
		fmt.Printf("[Sample Data] Demo user created successfully\n")
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message":        "Sample data generated",
		"factory_count":  len(finalFactories),
		"database":       dbName,
		"invoice_sample": res,
	})
}
func toFloat(val interface{}) float64 {
	switch v := val.(type) {

	case float64:
		return v

	case float32:
		return float64(v)

	case int:
		return float64(v)

	case int32:
		return float64(v)

	case int64:
		return float64(v)

	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}

	case nil:
		return 0
	}

	return 0
}
func ProcessAndInsertProduction(
	dbName string,
	productionCol *mongo.Collection,
	data []interface{},
	stage string,
) {

	var validData []interface{}

	for _, item := range data {

		doc, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		if stage != "PACKING" {
			id, ok := doc["_id"].(string)
			if !ok {
				fmt.Println(stage + " invalid _id")
				continue
			}

			err := PostProductionStock(dbName, id, "LV", doc)
			if err != nil {
				fmt.Println(stage+" validation failed:", err)
				continue
			}
		}

		validData = append(validData, doc)
	}

	if len(validData) > 0 {
		_, err := productionCol.InsertMany(context.Background(), validData)
		if err != nil {
			fmt.Println(stage+" insert error:", err)
		}
	}
}
func CreateOrgDemo(id string, payload map[string]interface{}) error {
	ctx := context.Background()

	collection := database.GetConnection("shared").Collection("organization")

	// Find existing org
	filter := bson.M{"_id": id}

	var doc bson.M
	err := collection.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		return err
	}

	oldID := doc["_id"].(string)
	newID := oldID + "_demo"

	// Check already exists
	count, _ := collection.CountDocuments(ctx, bson.M{"_id": newID})
	if count > 0 {
		return fmt.Errorf("already exists")
	}

	// Remove old _id and assign new
	delete(doc, "_id")
	doc["_id"] = newID
	doc["is_demo"] = true

	if orgPayload, ok := payload["organization"].(map[string]interface{}); ok {

		if newModules, exists := orgPayload["modules_enabled"]; exists {

			// Replace entire modules_enabled
			doc["modules_enabled"] = newModules
		}
	}

	// Insert new doc
	_, err = collection.InsertOne(ctx, doc)
	return err
}
func sampleSales(orgID, userID string, stock map[string]interface{}) error {

	saleData, err := sampleData.InsertSaleDirect(orgID, userID)
	if err != nil {
		return err
	}
	saleMap, ok := saleData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid saleData type")
	}

	SaleLedgerUpdate(saleMap, orgID, userID)

	saleproductData, err := sampleData.InsertSoldProductDirect(stock, saleMap, orgID, userID)
	if err != nil {
		return err
	}

	// err = UpdateKernelInventorySerailNumber(saleproductData, orgID, "LV")
	// 	if err != nil {
	// 		return fmt.Errorf("inventory update failed: %v", err)
	// 	}

	err = KernalAndOtherSaleUpdate(saleproductData, orgID, "LV")
	if err != nil {
		return fmt.Errorf("sale update failed: %v", err)
	}
	return nil
}
func GetDemoFactory(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Org not found")
	}

	ctx := context.Background()

	factoryCol := database.GetConnection(org.Id).Collection("factory")
	warehouseCol := database.GetConnection(org.Id).Collection("company")

	factoryCursor, err := factoryCol.Find(ctx, bson.M{})
	if err != nil {
		return shared.InternalServerError("Failed to fetch factories")
	}
	defer factoryCursor.Close(ctx)

	var factoryIDs []string

	for factoryCursor.Next(ctx) {
		var doc map[string]interface{}
		if err := factoryCursor.Decode(&doc); err != nil {
			continue
		}

		id := fmt.Sprintf("%v", doc["_id"])
		factoryIDs = append(factoryIDs, id)
	}

	// 🔹 Fetch warehouses
	warehouseCursor, err := warehouseCol.Find(ctx, bson.M{})
	if err != nil {
		return shared.InternalServerError("Failed to fetch warehouses")
	}
	defer warehouseCursor.Close(ctx)

	var warehouseIDs []string

	for warehouseCursor.Next(ctx) {
		var doc map[string]interface{}
		if err := warehouseCursor.Decode(&doc); err != nil {
			continue
		}

		id := fmt.Sprintf("%v", doc["_id"])
		warehouseIDs = append(warehouseIDs, id)
	}

	// ✅ Response
	return shared.SuccessResponse(c, fiber.Map{
		"factory_ids":   factoryIDs,
		"warehouse_ids": warehouseIDs,
	})
}
