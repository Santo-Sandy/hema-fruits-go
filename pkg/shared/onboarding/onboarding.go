// Package onboarding provides functionality for automating the setup of new organizations,
// including database creation, collections provisioning, and sample data generation.
package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/sampleData"
)

const (
	COMMON_CONFIG_DB     = "common_config"
	DEMO_ORG_ID          = "604162a4ce67408c8b22870191199ad4" // Template Org ID
	ONBOARD_CONFIG_TABLE = "onboarding_configs"
)

type OnboardingRequest struct {
	Welcome        interface{} `json:"welcome" bson:"welcome"`
	ModulesEnabled interface{} `json:"modules_enabled" bson:"modules_enabled"`
	Organization   struct {
		OrgName        string      `json:"org_name" bson:"org_name"`
		OrgType        string      `json:"org_type" bson:"org_type"`
		Email          string      `json:"email" bson:"email"`
		Contact        string      `json:"contact" bson:"contact"`
		OrgAddress     string      `json:"org_address" bson:"org_address"`
		FactoryCount   interface{} `json:"factory_count" bson:"factory_count"`
		WarehouseCount interface{} `json:"warehouse_count" bson:"warehouse_count"`
		ModulesEnabled interface{} `json:"modules_enabled" bson:"modules_enabled"`
	} `json:"organization" bson:"organization"`
	Factories []FactoryReq   `json:"factories" bson:"factories"`
	Warehouse []WarehouseReq `json:"warehouse" bson:"warehouse"`
	TrialData struct {
		WantTrialData bool   `json:"wantTrialData" bson:"wantTrialData"`
		TrialDays     int    `json:"trialDays" bson:"trialDays"`
		Language      string `json:"language" bson:"language"`
	} `json:"trialData" bson:"trialData"`
	EmployeeID string `json:"employee_id" bson:"employee_id"`
	OrgID      string `json:"org_id" bson:"org_id"`
}

type FactoryReq struct {
	FactoryName     string      `json:"factory_name" bson:"factory_name"`
	NoOfEmployee    interface{} `json:"no_of_Employee" bson:"no_of_Employee"`
	FactoryAddress  string      `json:"factory_address" bson:"factory_address"`
	FactoryContact  string      `json:"factory_contact" bson:"factory_contact"`
	PurchaseOutTurn float64     `json:"purchaseOutTurn" bson:"purchaseOutTurn"`
	PerDayOpInBags  int         `json:"op_bag_per_day" bson:"op_bag_per_day"`
	SelectedProcess []struct {
		ProcessName    string `json:"process_name" bson:"process_name"`
		NoOfEmployee   int    `json:"no_of_Employee" bson:"no_of_Employee"`
		EquipmentCount int    `json:"equipment_count" bson:"equipment_count"`
		OpBagPerDay    int    `json:"op_bag_per_day" bson:"op_bag_per_day"`
	} `json:"selected_process" bson:"selected_process"`
	FactoryIndex int `json:"factoryIndex" bson:"factoryIndex"`
}

type WarehouseReq struct {
	WarehouseName     string `json:"warehouse_name" bson:"warehouse_name"`
	WarehouseType     string `json:"warehouse_type" bson:"warehouse_type"`
	WarehouseLocation string `json:"warehouse_location" bson:"warehouse_location"`
	WarehouseContact  string `json:"warehouse_contact" bson:"warehouse_contact"`
	WarehouseIndex    int    `json:"warehouseIndex" bson:"warehouseIndex"`
}

// 2. ProvisionOrgHandler - Combined flow (Sync + Store + DB Creation)
// ProvisionOrgHandler - Step 1: Just Store Config (Syncs templates as well)
func ProvisionOrgHandler(c *fiber.Ctx) error {
	var payload OnboardingRequest
	if err := c.BodyParser(&payload); err != nil {
		return shared.BadRequest("Invalid payload: " + err.Error())
	}

	// Use orgId from header, or fallback to employee_id or org_id from payload
	orgID := c.Get("orgId")
	if orgID == "" {
		orgID = c.Get("OrgId")
	}
	if orgID == "" {
		orgID = payload.EmployeeID
	}
	if orgID == "" {
		orgID = payload.OrgID
	}
	if orgID == "" {
		return shared.BadRequest("Organization Identifier (orgId header or employee_id) is required")
	}

	// 1. Sync the absolute latest templates to common_config (ensures latest metadata is available)
	fmt.Println("[Onboarding] Syncing master templates from demo to common_config...")
	if err := SyncDemoDataToCommonConfig(); err != nil {
		fmt.Printf("[Onboarding] Warning: Template sync partial failure: %v\n", err)
	}

	// 2. Store the config for record keeping (Includes Trial Data)
	// Use employee_id as the primary key if available, otherwise fallback to orgId header
	saveID := payload.EmployeeID
	if saveID == "" {
		saveID = orgID
	}

	// Move modules_enabled from root to organization object
	if payload.ModulesEnabled != nil {
		payload.Organization.ModulesEnabled = payload.ModulesEnabled
	}

	err := SaveConfigToCommonDB(saveID, payload)
	if err != nil {
		return shared.InternalServerError("Failed to save organization config: " + err.Error())
	}

	// Update organization collection in shareddb
	database.SharedDB.Collection("organization").UpdateOne(
		context.Background(),
		bson.M{"_id": saveID},
		bson.M{"$set": payload},
	)

	// 3. Mark user profile as complete in shared database using org_id
	updateData := map[string]interface{}{
		"is_profile_completed": true,
	}
	fmt.Printf("[Onboarding] Marking user record for org %s as profile complete\n", saveID)
	helper.UpdateDataToDb("shared", bson.M{"org_id": saveID}, updateData, "user")

	// 4. If organization is ALREADY approved, trigger infrastructure setup immediately
	var orgDoc map[string]interface{}
	err = database.SharedDB.Collection("organization").FindOne(context.Background(), bson.M{"_id": saveID}).Decode(&orgDoc)
	if err == nil && orgDoc != nil && orgDoc["domain_status"] == "Approved" {
		fmt.Printf("[Onboarding] Organization %s is already approved. Triggering immediate activation...\n", saveID)
		InitializeInfrastructure(saveID, payload)
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Organization configuration stored and processed successfully.",
		"org_id":  saveID,
	})
}

// ActivateOrgHandler - Step 2: Trigger DB creation, cloning and sample records from existing OrgID
func ActivateOrgHandler(c *fiber.Ctx) error {
	orgID := c.Params("orgId")
	ctx := context.Background()
	db := database.SharedDB.Client().Database(COMMON_CONFIG_DB)
	col := db.Collection(ONBOARD_CONFIG_TABLE)

	var storedConfig struct {
		Payload OnboardingRequest `bson:"payload"`
	}

	// 1. Try finding by ID (Current Standard)
	err := col.FindOne(ctx, bson.M{"_id": orgID}).Decode(&storedConfig)
	if err != nil {
		// 2. Fallback: Try finding by Name (Legacy Support)
		err = col.FindOne(ctx, bson.M{"org_name": orgID}).Decode(&storedConfig)
		if err != nil {
			return shared.BadRequest(fmt.Sprintf("Configuration not found for ID/Name: %s. Please ensure Step 1 (/onboard-org) was successful.", orgID))
		}
	}

	newOrgID, err := InitializeInfrastructure(orgID, storedConfig.Payload)
	if err != nil {
		return shared.InternalServerError("Failed to initialize organization setup: " + err.Error())
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Activation started: Database initialized, cloning and sample data generation triggered",
		"org_id":  newOrgID,
	})
}

// SeedRandomNames - Loads the local random_name.json into common_config for dynamic employee generation
func SeedRandomNames() error {
	ctx := context.Background()
	db := database.SharedDB.Client().Database(COMMON_CONFIG_DB)
	col := db.Collection("random_name")

	// Read file
	// Try multiple possible paths to find the file from different entry points
	paths := []string{
		"pkg/shared/onboarding/random_name.json",
		"../../pkg/shared/onboarding/random_name.json",
		"../pkg/shared/onboarding/random_name.json",
	}

	var data []byte
	var err error
	for _, p := range paths {
		data, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}

	if err != nil {
		fmt.Printf("[Onboarding] Warning: random_name.json not found in any expected location: %v\n", err)
		return nil
	}

	var names map[string][]string
	if err := json.Unmarshal(data, &names); err != nil {
		return err
	}

	for region, list := range names {
		col.UpdateOne(ctx, bson.M{"_id": region}, bson.M{"$set": bson.M{"names": list}}, options.Update().SetUpsert(true))
	}
	return nil
}

// SyncDemoDataToCommonConfig - Shared internal logic to refresh common_config from the trial/demo repository
func SyncDemoDataToCommonConfig() error {
	ctx := context.Background()
	sourceDB := database.GetConnection(DEMO_ORG_ID)
	targetDB := database.SharedDB.Client().Database(COMMON_CONFIG_DB)

	// Refresh Random Names pool first
	SeedRandomNames()

	collections := []string{
		"lookup", "master_menu", "screen", "designation", "origin",
		"country", "dataset_config", "jobwork_template", "role_acl",
		"factory_process", "dashboard_config", "process", "product",
		"product_group", "templatetype",
	}

	for _, col := range collections {
		cursor, err := sourceDB.Collection(col).Find(ctx, bson.M{})
		if err != nil {
			continue
		}
		var docs []interface{}
		for cursor.Next(ctx) {
			var doc bson.M
			if err := cursor.Decode(&doc); err == nil {
				docs = append(docs, doc)
			}
		}
		if len(docs) > 0 {
			targetDB.Collection(col).DeleteMany(ctx, bson.M{})
			targetDB.Collection(col).InsertMany(ctx, docs)
		}
	}
	return nil
}

func SaveConfigToCommonDB(orgID string, payload OnboardingRequest) error {
	ctx := context.Background()

	// Ensure we use the initialized SharedDB client
	db := database.SharedDB.Client().Database(COMMON_CONFIG_DB)
	col := db.Collection(ONBOARD_CONFIG_TABLE)

	doc := bson.M{
		"_id":        orgID,
		"org_name":   payload.Organization.OrgName,
		"payload":    payload,
		"status":     "Stored",
		"created_at": time.Now(),
	}

	fmt.Printf("[Onboarding Storage] Saving config for org: %s (Name: %s) into %s.%s\n", orgID, payload.Organization.OrgName, COMMON_CONFIG_DB, ONBOARD_CONFIG_TABLE)

	opts := options.Update().SetUpsert(true)
	_, err := col.UpdateOne(ctx, bson.M{"_id": orgID}, bson.M{"$set": doc}, opts)
	if err != nil {
		fmt.Printf("[Onboarding Storage Error] Failed to save for %s: %v\n", orgID, err)
	}
	return err
}

func InitializeInfrastructure(orgID string, payload OnboardingRequest) (string, error) {
	// Always create the production database (clean, no trial data)
	dbName := strings.ToLower(orgID)
	fmt.Printf("[Onboarding] provisioning production database %s\n", dbName)
	_, err := database.CreateNewMongoDatabase(dbName, orgID)
	if err != nil {
		fmt.Printf("[Onboarding] Error: Production database provisioning failed for %s: %v\n", orgID, err)
		return "", fmt.Errorf("failed to provision database: %w", err)
	}

	// Background: setup for production DB — skip everything if demo mode
	go func(id string, p OnboardingRequest) {
		fmt.Printf("[Onboarding] Starting production setup for Org: %s\n", id)
		if err := FilterAndCloneDemoData(id, p); err != nil {
			fmt.Printf("[Onboarding] Error cloning for production %s: %v\n", id, err)
			return
		}
		if err := SetupFactoriesAndWarehouses(id, p); err != nil {
			fmt.Printf("[Onboarding] Error factory setup for production %s: %v\n", id, err)
			return
		}
		fmt.Printf("[Onboarding] Production setup completed for Org: %s\n", id)
	}(orgID, payload)

	// If demo requested, also create a separate _demo database with trial data
	if payload.TrialData.WantTrialData {
		demoOrgID := orgID + "_demo"
		demoDBName := strings.ToLower(demoOrgID)
		fmt.Printf("[Onboarding] provisioning demo database %s\n", demoDBName)
		_, err := database.CreateNewMongoDatabase(demoDBName, demoOrgID)
		if err != nil {
			fmt.Printf("[Onboarding] Error: Demo database provisioning failed for %s: %v\n", demoOrgID, err)
		} else {
			go func(id string, p OnboardingRequest) {
				fmt.Printf("[Onboarding] Starting demo setup for Org: %s\n", id)

				if err := FilterAndCloneDemoData(id, p); err != nil {
					fmt.Printf("[Onboarding] Error cloning for demo %s: %v\n", id, err)
					return
				}
				if err := SetupFactoriesAndWarehouses(id, p); err != nil {
					fmt.Printf("[Onboarding] Error factory setup for demo %s: %v\n", id, err)
					return
				}
				if err := InitializeAccountingCollections(id); err != nil {
					fmt.Printf("[Onboarding] Error accounting setup for demo %s: %v\n", id, err)
				}
				simReq := TrialDataRequest{
					OrgID:    id,
					NoOfDays: p.TrialData.TrialDays,
					Region:   p.TrialData.Language,
				}
				if simReq.NoOfDays == 0 {
					simReq.NoOfDays = 30
				}
				if simReq.Region == "" {
					simReq.Region = "Tamil"
				}
				if err := PerformTrialDataSimulation(simReq); err != nil {
					fmt.Printf("[Onboarding] Error trial data for demo %s: %v\n", id, err)
				}
				fmt.Printf("[Onboarding] Demo setup completed for Org: %s\n", id)
			}(demoOrgID, payload)
		}
	}

	return orgID, nil
}

// FilterAndCloneDemoData clones foundational data and selectively clones process-specific
// configurations based on the organization's onboarding payload.
func FilterAndCloneDemoData(orgID string, payload OnboardingRequest) error {
	ctx := context.Background()
	// Use common_config as the single source for all organization templates
	sourceDB := database.SharedDB.Client().Database(COMMON_CONFIG_DB)
	targetDB := database.GetConnection(orgID)

	// Phase 1: Clone Universal Base Collections (1:1 from common_config)
	baseCols := []string{
		"screen", "role_acl", "lookup", "master_menu", "dashboard_config",
		"designation", "origin", "country", "jobwork_template", "dataset_config",
		"product", "product_group",
	}
	for _, col := range baseCols {
		err := cloneCollection(ctx, sourceDB, targetDB, col, bson.M{}, true)
		if err != nil {
			fmt.Printf("Warning: Failed to clone base collection %s: %v\n", col, err)
		}
	}

	// Process-specific filtering
	var selectedProcessIDs []string
	for _, f := range payload.Factories {
		for _, p := range f.SelectedProcess {
			if p.ProcessName != "" {
				selectedProcessIDs = append(selectedProcessIDs, p.ProcessName)
			}
		}
	}

	if len(selectedProcessIDs) > 0 {
		fmt.Printf("[Onboarding] Resolving actual process IDs for: %v\n", selectedProcessIDs)
		// 1. Fetch process records to identify their template_ids and actual IDs
		cursor, err := sourceDB.Collection("process").Find(ctx, bson.M{
			"$or": []bson.M{
				{"_id": bson.M{"$in": selectedProcessIDs}},
				{"process_name": bson.M{"$in": selectedProcessIDs}},
				{"process_type": bson.M{"$in": selectedProcessIDs}},
			},
		})
		if err == nil {
			defer cursor.Close(ctx)
			var processes []bson.M
			if err := cursor.All(ctx, &processes); err == nil && len(processes) > 0 {
				var templateIDs []string
				templateIDMap := make(map[string]bool)

				for _, p := range processes {
					if tid, ok := p["template_id"].(string); ok && tid != "" {
						if !templateIDMap[tid] {
							templateIDMap[tid] = true
							templateIDs = append(templateIDs, tid)
						}
					}
				}

				// Clone selected process records (Idempotent)
				var pModels []mongo.WriteModel
				for _, p := range processes {
					pModels = append(pModels, mongo.NewReplaceOneModel().
						SetFilter(bson.M{"_id": p["_id"]}).
						SetReplacement(p).
						SetUpsert(true))
				}
				if len(pModels) > 0 {
					targetDB.Collection("process").DeleteMany(ctx, bson.M{}) // Truncate before cloning
					targetDB.Collection("process").BulkWrite(ctx, pModels)
				}

				if len(templateIDs) > 0 {
					fmt.Printf("[Onboarding] Identified %d Template IDs for cloning templatetype\n", len(templateIDs))
					// 2. Clone templatetype records
					err := cloneCollection(ctx, sourceDB, targetDB, "templatetype", bson.M{"_id": bson.M{"$in": templateIDs}}, true)
					if err != nil {
						fmt.Printf("[Onboarding] Warning: templatetype clone failed: %v\n", err)
					}

					// 3. Clone process_product records
					cloneCollection(ctx, sourceDB, targetDB, "process_product", bson.M{"template_id": bson.M{"$in": templateIDs}}, true)
				} else {
					fmt.Println("[Onboarding] No Template IDs found in selected processes")
				}
			}
		}
	}

	return nil
}

func SetupFactoriesAndWarehouses(orgID string, payload OnboardingRequest) error {
	ctx := context.Background()
	targetDB := database.GetConnection(orgID)

	// Clean all sample-related collections to allow retries without duplicate key errors
	cols := []string{"factory", "warehouse", "factory_process", "unit", "employee", "equipments", "maintance_details", "bank_details"}
	for _, col := range cols {
		targetDB.Collection(col).DeleteMany(ctx, bson.M{})
	}

	var finalFactories []map[string]interface{}
	for _, f := range payload.Factories {
		finalFactories = append(finalFactories, transformFactory(f))
	}
	var finalWarehouses []map[string]interface{}
	for _, w := range payload.Warehouse {
		finalWarehouses = append(finalWarehouses, transformWarehouse(w, orgID))
	}

	// Copy user from shareddb to this org database
	var userData bson.M
	err := database.SharedDB.Collection("user").FindOne(ctx, bson.M{"org_id": orgID}).Decode(&userData)
	if err == nil {
		targetDB.Collection("user").DeleteMany(ctx, bson.M{"org_id": orgID})
		targetDB.Collection("user").InsertOne(ctx, userData)
		fmt.Printf("[Onboarding] User copied to org database %s\n", orgID)
	} else {
		fmt.Printf("[Onboarding] Warning: No user found in shareddb for org %s: %v\n", orgID, err)
	}

	_, err = sampleData.GenerateSampleFactory(finalFactories, finalWarehouses, orgID, orgID)
	return err
}

func cloneCollection(ctx context.Context, sourceDB, targetDB *mongo.Database, colName string, filter bson.M, truncate bool) error {
	if truncate {
		targetDB.Collection(colName).DeleteMany(ctx, bson.M{})
	}
	cursor, err := sourceDB.Collection(colName).Find(ctx, filter)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var models []mongo.WriteModel
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err == nil {
			// Use ReplaceOne with Upsert to handle existing records gracefully
			models = append(models, mongo.NewReplaceOneModel().
				SetFilter(bson.M{"_id": doc["_id"]}).
				SetReplacement(doc).
				SetUpsert(true))
		}
	}

	if len(models) > 0 {
		_, err := targetDB.Collection(colName).BulkWrite(ctx, models)
		return err
	}
	return nil
}

func transformFactory(f FactoryReq) map[string]interface{} {
	finalEmp := make(map[string]interface{})
	finalEquip := make(map[string]interface{})
	var processes []string

	for _, p := range f.SelectedProcess {
		proc := p.ProcessName
		// Normalize
		if proc == "MC-SH" || proc == "ML-SH" {
			proc = "SHELL"
		}
		processes = append(processes, proc)
		finalEmp[proc] = p.NoOfEmployee
		finalEquip[proc] = p.EquipmentCount
	}

	id := fmt.Sprintf("FAC--%03d", f.FactoryIndex)
	if f.FactoryIndex == 0 {
		id = "FAC--001"
	}

	// Logic: Use provided name or generate a random professional name
	fName := f.FactoryName
	if fName == "" {
		factoryNames := []string{"Titan Mfg", "Nexus Hub", "Elite Works", "Prime Center", "Global Facility"}
		fName = factoryNames[rand.Intn(len(factoryNames))] + "-" + strings.TrimPrefix(id, "FAC--")
	}

	return map[string]interface{}{
		"_id":               id,
		"factory_name":      fName,
		"factory_address":   f.FactoryAddress,
		"factory_contact":   f.FactoryContact,
		"factory_processes": processes,
		"no_of_Employee":    finalEmp,
		"equipment_count":   finalEquip,
	}
}

func transformWarehouse(w WarehouseReq, orgID string) map[string]interface{} {
	id := fmt.Sprintf("WAR%03d", w.WarehouseIndex)
	if w.WarehouseIndex == 0 {
		id = "WAR001"
	}

	// Logic: Use provided name or generate a random professional name
	wName := w.WarehouseName
	if wName == "" {
		warehouseNames := []string{"Central Logics", "North Depot", "Bay Area Hub", "East Storage", "South Station"}
		wName = warehouseNames[rand.Intn(len(warehouseNames))] + "-" + strings.TrimPrefix(id, "WAR")
	}

	return map[string]interface{}{
		"_id":                 id,
		"name":                wName,
		"warehouse_type":      w.WarehouseType,
		"address":             w.WarehouseLocation,
		"mobile_number":       w.WarehouseContact,
		"status":              "Active",
		"org_id":              orgID,
		"inside_factory":      false,
		"contact_person_name": "Warehouse Manager",
		"created_on":          time.Now(),
	}
}

func CopyMultipleCollections(sourceOrgID string, targetOrgID string, collections []string, overwrite bool) error {
	ctx := context.Background()
	sourceDB := database.GetConnection(sourceOrgID)
	targetDB := database.GetConnection(targetOrgID)

	for _, col := range collections {
		err := cloneCollection(ctx, sourceDB, targetDB, col, bson.M{}, overwrite)
		if err != nil {
			return fmt.Errorf("failed to copy collection %s: %w", col, err)
		}
	}
	return nil
}

// RemoveCollectionDataHandler - Deletes or soft-deletes data from specified collections
func RemoveCollectionDataHandler(c *fiber.Ctx) error {
	var req struct {
		OrgID       string   `json:"org_id"`
		Collections []string `json:"collections"`
		SoftDelete  bool     `json:"soft_delete"`
	}
	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest("Invalid payload")
	}

	if req.OrgID == "" || len(req.Collections) == 0 {
		return shared.BadRequest("org_id and collections are required")
	}

	ctx := context.Background()
	db := database.GetConnection(req.OrgID)

	for _, colName := range req.Collections {
		col := db.Collection(colName)
		if req.SoftDelete {
			// Update status to Is_deleted
			update := bson.M{
				"$set": bson.M{
					"status":    "Is_deleted",
					"is_delete": true,
				},
			}
			_, err := col.UpdateMany(ctx, bson.M{}, update)
			if err != nil {
				return shared.InternalServerError(fmt.Sprintf("Failed to soft delete %s: %v", colName, err))
			}
		} else {
			// Permanent delete
			_, err := col.DeleteMany(ctx, bson.M{})
			if err != nil {
				return shared.InternalServerError(fmt.Sprintf("Failed to permanently delete %s: %v", colName, err))
			}
		}
	}

	return shared.SuccessResponse(c, fiber.Map{"message": "Data removed successfully"})
}

// GetGenerationStatusHandler - Allows frontend to poll for background task progress
// func GetGenerationStatusHandler(c *fiber.Ctx) error {
// 	orgID := c.Params("orgId")
// 	if orgID == "" {
// 		return shared.BadRequest("orgId is required")
// 	}

// 	// ctx := context.Background()
// 	// db := database.GetConnection(orgID)
// 	// var status bson.M
// 	// // err := db.Collection("generation_status").FindOne(ctx, bson.M{"_id": "trial_data"}).Decode(&status)
// 	// if err != nil {
// 	// 	return shared.SuccessResponse(c, fiber.Map{
// 	// 		"status":  "NotStarted",
// 	// 		"message": "No background task found",
// 	// 	})
// 	// }

// 	// return shared.SuccessResponse(c, status)
// }

// ManualTriggerDailyDataHandler - Manually trigger daily data generation for testing
func ManualTriggerDailyDataHandler(c *fiber.Ctx) error {
	orgID := c.Query("orgId")
	if orgID == "" {
		return shared.BadRequest("orgId query parameter is required")
	}

	ctx := context.Background()
	commonDB := database.SharedDB.Client().Database(COMMON_CONFIG_DB)

	var config struct {
		ID      string            `bson:"_id"`
		Payload OnboardingRequest `bson:"payload"`
	}

	err := commonDB.Collection(ONBOARD_CONFIG_TABLE).FindOne(ctx, bson.M{"_id": orgID}).Decode(&config)
	if err != nil {
		return shared.BadRequest(fmt.Sprintf("Organization %s not found or trial data not enabled", orgID))
	}

	if !config.Payload.TrialData.WantTrialData {
		return shared.BadRequest("Trial data is not enabled for this organization")
	}

	// Run in background
	go func() {
		req := TrialDataRequest{
			OrgID:    orgID,
			NoOfDays: 1,
			Region:   config.Payload.TrialData.Language,
		}
		if req.Region == "" {
			req.Region = "Tamil"
		}

		if err := GenerateDailyTrialData(req, config.Payload); err != nil {
			fmt.Printf("[Manual Trigger] Error: %v\n", err)
		} else {
			fmt.Printf("[Manual Trigger] Success for %s\n", orgID)
		}
	}()

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Daily data generation triggered in background",
		"org_id":  orgID,
	})
}

// InitializeAccountingCollections creates account_head and cash_ledger collections
// This runs during organization approval, regardless of trial data settings
func InitializeAccountingCollections(orgID string) error {
	ctx := context.Background()
	db := database.GetConnection(orgID)
	startTime := time.Now()

	// 1. Create Account Heads
	existingAccountHeadCount, _ := db.Collection("account_head").CountDocuments(ctx, bson.M{})
	if existingAccountHeadCount == 0 {
		accountHeads := []interface{}{
			bson.M{
				"_id":                    "FUEL",
				"name":                   "Fuel",
				"description":            "Fuel Expenses",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     5000,
				"level_1_approve_limit":  10000,
				"level_2_approve_limit":  20000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
			bson.M{
				"_id":                    "FUEL-VAN",
				"name":                   "Fuel Van",
				"description":            "Fuel Expenses for Van",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     2000,
				"level_1_approve_limit":  5000,
				"level_2_approve_limit":  10000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "FUEL",
			},
			bson.M{
				"_id":                    "FUEL-TRUCK",
				"name":                   "Fuel Truck",
				"description":            "Fuel Expenses for Truck",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     3000,
				"level_1_approve_limit":  7000,
				"level_2_approve_limit":  15000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "FUEL",
			},
			bson.M{
				"_id":                    "SALARY",
				"name":                   "Salary",
				"description":            "Employee Salary Expenses",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     50000,
				"level_1_approve_limit":  100000,
				"level_2_approve_limit":  200000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
			bson.M{
				"_id":                    "MAINTENANCE",
				"name":                   "Maintenance",
				"description":            "Equipment and Facility Maintenance",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     5000,
				"level_1_approve_limit":  15000,
				"level_2_approve_limit":  30000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
			bson.M{
				"_id":                    "UTILITIES",
				"name":                   "Utilities",
				"description":            "Electricity, Water and Other Utilities",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     10000,
				"level_1_approve_limit":  25000,
				"level_2_approve_limit":  50000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
			bson.M{
				"_id":                    "TRANSPORT",
				"name":                   "Transport",
				"description":            "Transportation Expenses",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     3000,
				"level_1_approve_limit":  10000,
				"level_2_approve_limit":  20000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
			bson.M{
				"_id":                    "OFFICE-SUPPLIES",
				"name":                   "Office Supplies",
				"description":            "Office Supplies and Stationery",
				"status":                 "Active",
				"transactionType":        "Debit / Expense",
				"auto_approve_limit":     2000,
				"level_1_approve_limit":  5000,
				"level_2_approve_limit":  10000,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
			bson.M{
				"_id":                    "SALES-REVENUE",
				"name":                   "Sales Revenue",
				"description":            "Revenue from Sales",
				"status":                 "Active",
				"transactionType":        "Credit / Income",
				"auto_approve_limit":     0,
				"level_1_approve_limit":  0,
				"level_2_approve_limit":  0,
				"level_1_approve_role":   "OA",
				"level_2_approve_role":   "SA",
				"created_on":             startTime,
				"created_by":             "SYSTEM",
				"parent_id":              "",
			},
		}
		_, err := db.Collection("account_head").InsertMany(ctx, accountHeads)
		if err != nil {
			fmt.Printf("[Onboarding] Error creating account heads for %s: %v\n", orgID, err)
			return err
		}
		fmt.Printf("[Onboarding] Created %d account heads for %s\n", len(accountHeads), orgID)
	}

	// 2. Create Cash Ledger with opening balance
	existingCashLedgerCount, _ := db.Collection("cash_ledger").CountDocuments(ctx, bson.M{})
	if existingCashLedgerCount == 0 {
		// Fetch factories to create opening balance for each
		var factories []bson.M
		fCursor, _ := db.Collection("factory").Find(ctx, bson.M{})
		if fCursor != nil {
			fCursor.All(ctx, &factories)
			fCursor.Close(ctx)
		}

		if len(factories) > 0 {
			openingBalance := 100000.0
			var cashLedgerEntries []interface{}

			for _, factory := range factories {
				factoryID := factory["_id"].(string)

				// Opening balance entry
				cashLedgerEntries = append(cashLedgerEntries, bson.M{
					"_id":               uuid.New().String(),
					"created_on":        startTime,
					"created_by":        "SYSTEM",
					"opening_balance":   0,
					"closing_balance":   openingBalance,
					"factory_id":        factoryID,
					"transactionDate":   startTime,
					"amount":            openingBalance,
					"transactionType":   "Credit / Income",
					"description":       "Opening Balance",
					"status":            "Active",
					"available_balance": openingBalance,
					"account_head":      "SALES-REVENUE",
					"purchase_head":     "SALES-REVENUE",
				})
			}

			if len(cashLedgerEntries) > 0 {
				_, err := db.Collection("cash_ledger").InsertMany(ctx, cashLedgerEntries)
				if err != nil {
					fmt.Printf("[Onboarding] Error creating cash ledger for %s: %v\n", orgID, err)
					return err
				}
				fmt.Printf("[Onboarding] Created %d cash ledger entries for %s\n", len(cashLedgerEntries), orgID)
			}
		} else {
			fmt.Printf("[Onboarding] No factories found for %s, skipping cash ledger creation\n", orgID)
		}
	}

	return nil
}
