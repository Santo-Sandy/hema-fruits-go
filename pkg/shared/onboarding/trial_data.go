package onboarding

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

type TrialDataRequest struct {
	OrgID     string `json:"orgId"`
	TrialData bool   `json:"traildata"`
	NoOfDays  int    `json:"no_of_days"`
	Region    string `json:"region"`
}

func InitiateTrialDataSimulationHandler(c *fiber.Ctx) error {
	var req TrialDataRequest
	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest("Invalid payload")
	}

	if req.OrgID == "" {
		return shared.BadRequest("orgId is required")
	}

	if req.NoOfDays <= 0 {
		req.NoOfDays = 60
	}
	if req.Region == "" {
		req.Region = "Tamil"
	}

	// Move to background to avoid timeout
	go func(r TrialDataRequest) {
		err := PerformTrialDataSimulation(r)
		if err != nil {
			fmt.Printf("[Background] Error generating trial data for %s: %v\n", r.OrgID, err)
		} else {
			fmt.Printf("[Background] Successfully generated trial data for %s\n", r.OrgID)
		}
	}(req)

	return shared.SuccessResponse(c, fiber.Map{
		"message": fmt.Sprintf("Trial data generation started in background for %d days", req.NoOfDays),
		"orgId":   req.OrgID,
	})
}

func PerformTrialDataSimulation(req TrialDataRequest) error {
	ctx := context.Background()
	db := database.GetConnection(req.OrgID)
	db.Collection("init_collection").Drop(ctx)

	// Simulation Parameters (Defaults)
	noOfEmployee := 20
	perDayOpInBags := 60
	purchaseOutTurn := 45.0

	// 1. Fetch Config and Overrides from common_config
	commonDB := database.SharedDB.Client().Database(COMMON_CONFIG_DB)
	var storedConfig struct {
		Payload OnboardingRequest `bson:"payload"`
	}
	factoryConfigs := make(map[string]struct {
		Bags    int
		OutTurn float64
	})

	if err := commonDB.Collection(ONBOARD_CONFIG_TABLE).FindOne(ctx, bson.M{"_id": req.OrgID}).Decode(&storedConfig); err == nil {
		config := storedConfig.Payload
		totalEmp := 0
		totalBags := 0
		pOutTurn := 0.0
		for _, f := range config.Factories {
			fID := fmt.Sprintf("FAC--%03d", f.FactoryIndex)
			if f.FactoryIndex == 0 {
				fID = "FAC--001"
			}

			fBags := f.PerDayOpInBags          // Use factory level bags as base
			fOutTurnLocal := f.PurchaseOutTurn // Use factory level outturn

			for _, p := range f.SelectedProcess {
				totalEmp += p.NoOfEmployee
				if p.OpBagPerDay > 0 {
					fBags += p.OpBagPerDay
				}
			}
			totalBags += fBags
			if fOutTurnLocal > 0 {
				pOutTurn = fOutTurnLocal
			}
			factoryConfigs[fID] = struct {
				Bags    int
				OutTurn float64
			}{Bags: fBags, OutTurn: fOutTurnLocal}
		}
		if totalEmp > 0 {
			noOfEmployee = totalEmp
		}
		if totalBags > 0 {
			perDayOpInBags = totalBags
		}
		if pOutTurn > 0 {
			purchaseOutTurn = pOutTurn
		}
		fmt.Printf("[TrialData] Applied Config: Emp=%d, Bags=%d, OutTurn=%.2f\n", noOfEmployee, perDayOpInBags, purchaseOutTurn)
	}

	// 2. Fetch Regional Names pool
	var regionalNames []string
	nameDoc := bson.M{}
	if err := commonDB.Collection("random_name").FindOne(ctx, bson.M{"_id": strings.ToLower(req.Region)}).Decode(&nameDoc); err == nil {
		if names, ok := nameDoc["names"].(primitive.A); ok {
			for _, n := range names {
				regionalNames = append(regionalNames, fmt.Sprintf("%v", n))
			}
		}
	}

	startTime := time.Now().AddDate(0, 0, -req.NoOfDays)

	// statusCol := db.Collection("generation_status")

	updateStatus := func(msg string, status string) {
		// statusCol.UpdateOne(ctx, bson.M{"_id": "trial_data"}, bson.M{"$set": bson.M{
		// 	"message":    msg,
		// 	"status":     status,
		// 	"updated_on": time.Now(),
		// }}, options.Update().SetUpsert(true))
	}

	updateStatus("Initializing background generation...", "InProgress")

	// 1. Fetch Master Data
	var factories []bson.M
	fCursor, _ := db.Collection("factory").Find(ctx, bson.M{})
	fCursor.All(ctx, &factories)
	if len(factories) == 0 {
		return fmt.Errorf("no factories found")
	}

	var warehouses []bson.M
	wCursor, _ := db.Collection("company").Find(ctx, bson.M{})
	wCursor.All(ctx, &warehouses)
	if len(warehouses) == 0 {
		return fmt.Errorf("no warehouses found")
	}

	templatesByProcess := make(map[string]string)
	simulationGroupMap := make(map[string]string) // Map UserProcessName -> SimulationGroup (e.g. "MC-SH" -> "SHELL")

	tCursor, _ := db.Collection("process").Find(ctx, bson.M{})
	fmt.Printf("[TrialData] Building simulationGroupMap for %s...\n", req.OrgID)
	for tCursor.Next(ctx) {
		var t bson.M
		tCursor.Decode(&t)
		pID := fmt.Sprintf("%v", t["_id"])

		// Normalize pID for mapping aliases
		matchID := pID
		if pID == "MC-SH" || pID == "ML-SH" || pID == "SHELL" {
			matchID = "SHELL"
		} else if pID == "BORM" || pID == "BORMA" {
			matchID = "BORMA"
		} else if pID == "GRAD" || pID == "GRADING" {
			matchID = "GRAD"
		}

		if tid, ok := t["template_id"].(string); ok {
			templatesByProcess[matchID] = tid
		}
		// Dynamic Mapping: Check for simulation_group or use _id as fallback
		simGroup, ok := t["simulation_group"].(string)
		if !ok {
			simGroup, _ = t["process_type"].(string) // try process_type as fallback
		}
		if simGroup == "" {
			simGroup = matchID
		}
		simulationGroupMap[pID] = simGroup
		simulationGroupMap[matchID] = simGroup
		fmt.Printf("[TrialData]   Process Map: %s -> %s\n", pID, simGroup)
	}

	// 2. Account Heads Generation
	updateStatus("Generating account heads...", "InProgress")
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
		db.Collection("account_head").InsertMany(ctx, accountHeads)
		fmt.Printf("[TrialData] Created %d account heads\n", len(accountHeads))
	}

	// 2a. Cash Ledger Generation
	updateStatus("Generating cash ledger entries...", "InProgress")
	existingCashLedgerCount, _ := db.Collection("cash_ledger").CountDocuments(ctx, bson.M{})
	if existingCashLedgerCount == 0 && len(factories) > 0 {
		// Create initial opening balance for each factory
		openingBalance := 100000.0
		for _, factory := range factories {
			factoryID := factory["_id"].(string)
			
			// Opening balance entry
			db.Collection("cash_ledger").InsertOne(ctx, bson.M{
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
		
		// Create sample expense transactions
		cashLedgerEntries := []interface{}{}
		expenseTypes := []struct {
			AccountHead string
			Description string
			MinAmount   float64
			MaxAmount   float64
		}{
			{"FUEL-VAN", "VAN - DIESEL", 500, 2000},
			{"FUEL-TRUCK", "TRUCK - DIESEL", 1000, 3000},
			{"MAINTENANCE", "Equipment Maintenance", 2000, 5000},
			{"UTILITIES", "Electricity Bill", 5000, 10000},
			{"TRANSPORT", "Transportation Charges", 1000, 3000},
			{"OFFICE-SUPPLIES", "Office Stationery", 500, 1500},
		}
		
		for _, factory := range factories {
			factoryID := factory["_id"].(string)
			currentBalance := openingBalance
			
			// Create 5-10 random expense entries per factory
			numEntries := 5 + rand.Intn(6)
			for i := 0; i < numEntries; i++ {
				// Pick random expense type
				expenseType := expenseTypes[rand.Intn(len(expenseTypes))]
				amount := expenseType.MinAmount + rand.Float64()*(expenseType.MaxAmount-expenseType.MinAmount)
				
				// Random date within the trial period
				daysOffset := rand.Intn(req.NoOfDays + 1)
				transactionDate := startTime.AddDate(0, 0, daysOffset)
				
				openingBal := currentBalance
				currentBalance -= amount
				
				cashLedgerEntries = append(cashLedgerEntries, bson.M{
					"_id":               uuid.New().String(),
					"created_on":        transactionDate,
					"created_by":        "SYSTEM",
					"opening_balance":   openingBal,
					"closing_balance":   currentBalance,
					"factory_id":        factoryID,
					"transactionDate":   transactionDate,
					"amount":            amount,
					"transactionType":   "Debit / Expense",
					"description":       expenseType.Description,
					"status":            "Active",
					"available_balance": currentBalance,
					"account_head":      expenseType.AccountHead,
					"purchase_head":     expenseType.AccountHead,
				})
			}
		}
		
		if len(cashLedgerEntries) > 0 {
			db.Collection("cash_ledger").InsertMany(ctx, cashLedgerEntries)
			fmt.Printf("[TrialData] Created %d cash ledger entries\n", len(cashLedgerEntries)+len(factories))
		}
	}

	// Equipment Type Generation
	updateStatus("Generating equipment types...", "InProgress")
	existingEqTypeCount, _ := db.Collection("equipmenttype").CountDocuments(ctx, bson.M{})
	if existingEqTypeCount == 0 {
		equipmentTypes := []interface{}{
			bson.M{"_id": "COK", "equipment_type_name": "Steaming", "process_type": "ST", "description": "Boiling Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "CR", "equipment_type_name": "Cooling Room", "process_type": "CR", "description": "Cooling Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "BOR", "equipment_type_name": "Borma", "process_type": "BO", "description": "Borma Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "SH01", "equipment_type_name": "Shelling Machine", "process_type": "SH", "description": "Cashew Shelling Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "MG01", "equipment_type_name": "Manual Grading", "process_type": "GR", "description": "Manual Grading", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "AG01", "equipment_type_name": "Auto Grading", "process_type": "GR", "description": "Automatic Grading Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "PEEL_01", "equipment_type_name": "Manual Peeling", "process_type": "PE", "description": "Manual Peeling", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "PEEL_02", "equipment_type_name": "Machine Peeling", "process_type": "PE", "description": "Automatic Peeling Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "PACK_01", "equipment_type_name": "Packing Machine", "process_type": "PK", "description": "Cashew Packing Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "PACK_02", "equipment_type_name": "Vacuum Packing", "process_type": "PK", "description": "Vacuum Packing Machine", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "WGH_01", "equipment_type_name": "Weighing Scale", "process_type": "WG", "description": "Digital Weighing Scale", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "CNV_01", "equipment_type_name": "Conveyor Belt", "process_type": "CV", "description": "Conveyor Belt System", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "DRY_01", "equipment_type_name": "Dryer", "process_type": "DR", "description": "Industrial Dryer", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "STR_01", "equipment_type_name": "Storage Tank", "process_type": "ST", "description": "Raw Material Storage Tank", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
			bson.M{"_id": "QC_01", "equipment_type_name": "Quality Check", "process_type": "QC", "description": "Quality Control Equipment", "status": "Active", "created_on": startTime, "created_by": "SYSTEM", "org_id": req.OrgID},
		}
		db.Collection("equipmenttype").InsertMany(ctx, equipmentTypes)
		fmt.Printf("[TrialData] Created %d equipment types\n", len(equipmentTypes))
	}

	// Maintenance Config Generation
	updateStatus("Generating maintenance config...", "InProgress")
	existingMCCount, _ := db.Collection("maintenance_config").CountDocuments(ctx, bson.M{})
	if existingMCCount == 0 {
		maintenanceConfigs := []interface{}{
			bson.M{"_id": "6a6UMC--001", "name": "Machine Cleaning", "description": "Machine Cleaning", "bg_color": "#29b6f6", "text_color": "#f9a825", "status": "Active", "org_id": req.OrgID, "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "6a6UMC--002", "name": "Service", "description": "Service the Equipment", "bg_color": "#03a9f4", "text_color": "#ffebee", "status": "Active", "org_id": req.OrgID, "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "6a6UMC--003", "name": "Conveyor Belt Check", "description": "Check the Conveyor Belt", "bg_color": "#e91e63", "text_color": "#03a9f4", "status": "Active", "org_id": req.OrgID, "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "6a6UMC--004", "name": "Oil Change", "description": "Change the Machine Oil", "bg_color": "#ff9800", "text_color": "#ffffff", "status": "Active", "org_id": req.OrgID, "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "6a6UMC--005", "name": "Belt Replacement", "description": "Replace the Drive Belt", "bg_color": "#4caf50", "text_color": "#ffffff", "status": "Active", "org_id": req.OrgID, "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "6a6UMC--006", "name": "Lubrication", "description": "Lubricate Moving Parts", "bg_color": "#9c27b0", "text_color": "#ffffff", "status": "Active", "org_id": req.OrgID, "created_on": startTime, "created_by": "SYSTEM"},
		}
		db.Collection("maintenance_config").InsertMany(ctx, maintenanceConfigs)
		fmt.Printf("[TrialData] Created %d maintenance configs\n", len(maintenanceConfigs))
	}

	// Holiday Configuration Generation
	updateStatus("Generating holiday configuration...", "InProgress")
	existingHLCount, _ := db.Collection("holiday_configuration").CountDocuments(ctx, bson.M{})
	if existingHLCount == 0 {
		holidayConfigs := []interface{}{
			bson.M{"_id": "HL001", "name": "New Year", "fixeddate": time.Date(startTime.Year(), 1, 1, 0, 0, 0, 0, time.UTC), "occurrence": "Single", "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "HL002", "name": "Independence Day", "fixeddate": time.Date(startTime.Year(), 8, 15, 0, 0, 0, 0, time.UTC), "occurrence": "Single", "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "HL003", "name": "Shut Down", "fixeddate": time.Date(startTime.Year(), 10, 2, 0, 0, 0, 0, time.UTC), "occurrence": "Single", "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": "HL004", "name": "Diwali", "fixeddate": time.Date(startTime.Year(), 11, 1, 0, 0, 0, 0, time.UTC), "occurrence": "Single", "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
		}
		db.Collection("holiday_configuration").InsertMany(ctx, holidayConfigs)
		fmt.Printf("[TrialData] Created %d holiday configurations\n", len(holidayConfigs))
	}

	// Worker Salary Config Generation
	updateStatus("Generating worker salary config...", "InProgress")
	existingWSCount, _ := db.Collection("worker_salary_config").CountDocuments(ctx, bson.M{})
	if existingWSCount == 0 {
		workerSalaryConfigs := []interface{}{
			bson.M{"_id": uuid.New().String(), "process_type": "COOK", "process_id": 1, "type_of_work": "Cooking", "salary": 400, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "SHELL", "process_id": 2, "type_of_work": "Shell Checking", "salary": 350, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "SHELL", "process_id": 2, "type_of_work": "Shelling", "salary": 380, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "BORM", "process_id": 3, "type_of_work": "Borma Operation", "salary": 420, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "GRAD", "process_id": 6, "type_of_work": "Yard Working 350", "salary": 350, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "GRAD", "process_id": 6, "type_of_work": "Yard Working 380", "salary": 380, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "GRAD", "process_id": 6, "type_of_work": "Yard Working 400", "salary": 400, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "PEEL", "process_id": 5, "type_of_work": "Peeling", "salary": 360, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
			bson.M{"_id": uuid.New().String(), "process_type": "PACK", "process_id": 6, "type_of_work": "Packing", "salary": 390, "status": "Active", "created_on": startTime, "created_by": "SYSTEM"},
		}
		db.Collection("worker_salary_config").InsertMany(ctx, workerSalaryConfigs)
		fmt.Printf("[TrialData] Created %d worker salary configs\n", len(workerSalaryConfigs))
	}

	// Stock Value Generation
	updateStatus("Generating stock values...", "InProgress")
	existingSVCount, _ := db.Collection("stock_value").CountDocuments(ctx, bson.M{})
	if existingSVCount == 0 {
		// Fetch origins from DB
		var originList []bson.M
		svOriginCursor, _ := db.Collection("origin").Find(ctx, bson.M{})
		if svOriginCursor != nil {
			svOriginCursor.All(ctx, &originList)
		}
		if len(originList) > 0 {
			productValues := []struct {
				ProductID string
				Value     float64
			}{
				{"W180", 205}, {"W210", 195}, {"W240", 185}, {"W320", 170},
				{"W450", 155}, {"SW240", 300}, {"SW360", 280}, {"SWP", 150},
				{"LWP", 140}, {"P_WHOLES", 75}, {"SPLITS", 120}, {"BB", 100},
			}
			var stockValues []interface{}
			for _, pv := range productValues {
				origin := originList[rand.Intn(len(originList))]
				originID := fmt.Sprintf("%v", origin["_id"])
				stockValues = append(stockValues, bson.M{
					"_id":        uuid.New().String(),
					"product_id": pv.ProductID,
					"origin_id":  originID,
					"value":      pv.Value,
					"created_on": startTime,
					"created_by": "SYSTEM",
					"update_by":  "SYSTEM",
					"update_on":  startTime,
				})
			}
			db.Collection("stock_value").InsertMany(ctx, stockValues)
			fmt.Printf("[TrialData] Created %d stock values\n", len(stockValues))
		}
	}

	// Leave Details Generation
	updateStatus("Generating leave details...", "InProgress")
	existingLDCount, _ := db.Collection("leave_details").CountDocuments(ctx, bson.M{})
	if existingLDCount == 0 {
		// Fetch employees from DB
		var empList []bson.M
		ldEmpCursor, _ := db.Collection("employee").Find(ctx, bson.M{"status": "Active"})
		if ldEmpCursor != nil {
			ldEmpCursor.All(ctx, &empList)
		}
		if len(empList) > 0 {
			reasons := []string{"Sick leave", "Family Emergency", "Personal Work", "Medical", "Casual Leave"}
			leaveStatuses := []string{"Approved", "Pending", "Approved", "Approved"}
			var leaveDocs []interface{}
			for i := 0; i < 5; i++ {
				emp := empList[rand.Intn(len(empList))]
				empID := fmt.Sprintf("%v", emp["_id"])
				numDays := 1 + rand.Intn(5)
				leaveDate := startTime.AddDate(0, 0, rand.Intn(req.NoOfDays))
				endDate := leaveDate.AddDate(0, 0, numDays)
				leaveDocs = append(leaveDocs, bson.M{
					"_id":            uuid.New().String(),
					"employee_id":    empID,
					"org_id":         req.OrgID,
					"reason":         reasons[rand.Intn(len(reasons))],
					"number_of_days": numDays,
					"leave_date":     leaveDate,
					"start_date":     leaveDate,
					"end_date":       endDate,
					"leave_status":   leaveStatuses[rand.Intn(len(leaveStatuses))],
					"status":         "Active",
					"created_on":     leaveDate,
					"created_by":     "SYSTEM",
				})
			}
			db.Collection("leave_details").InsertMany(ctx, leaveDocs)
			fmt.Printf("[TrialData] Created %d leave details\n", len(leaveDocs))
		}
	}

	// 3. Workforce Generation
	updateStatus("Generating workforce...", "InProgress")
	existingEmpCount, _ := db.Collection("employee").CountDocuments(ctx, bson.M{})
	if int(existingEmpCount) < noOfEmployee {
		// Fetch units to assign employees
		var units []bson.M
		uCursor, _ := db.Collection("unit").Find(ctx, bson.M{})
		uCursor.All(ctx, &units)

		if len(units) > 0 {
			needed := noOfEmployee - int(existingEmpCount)
			var empDocs []interface{}
			for e := 0; e < needed; e++ {
				// Pick a random unit and its factory
				unit := units[rand.Intn(len(units))]
				unitID := fmt.Sprintf("%v", unit["_id"])
				factoryID := fmt.Sprintf("%v", unit["factory_id"])

				empDocs = append(empDocs, generateFullEmployeeDocLocal(factoryID, unitID, req.OrgID, "Worker", regionalNames))
			}
			if len(empDocs) > 0 {
				db.Collection("employee").InsertMany(ctx, empDocs)
			}
		}
	}

	// 3. Simulation Loop
	// Multi-Warehouse Stock State (Virtual)
	stockRCN := make(map[string]float64)
	stockSteamed := make(map[string]float64)
	stockBorma := make(map[string]float64)
	stockGrading := make(map[string]float64)
	stockPeeled := make(map[string]float64)
	stockPacked := make(map[string]float64)
	latestPurchaseByWh := make(map[string]string)

	// Fetch actual product IDs from product collection
	productMap := make(map[string]string) // Map: process -> actual product_id
	allProducts := []string{}              // Store all product IDs
	productCursor, _ := db.Collection("product").Find(ctx, bson.M{"status": "Active"})
	if productCursor != nil {
		for productCursor.Next(ctx) {
			var prod bson.M
			productCursor.Decode(&prod)
			prodID := fmt.Sprintf("%v", prod["_id"])
			allProducts = append(allProducts, prodID)
			section := strings.ToUpper(fmt.Sprintf("%v", prod["section"]))
			
			// Map products to processes based on section
			if section == "PURCHASE" || prodID == "RCN" {
				productMap["RCN"] = prodID
			} else if section == "COOKING" || section == "COOK" || prodID == "STEAMEDRCN" {
				productMap["STEAMEDRCN"] = prodID
			} else if section == "SHELLING" || section == "SHELL" {
				productMap["SH_WHOLES"] = prodID
			} else if section == "BORMA" {
				productMap["BORMA"] = prodID
			} else if section == "GRADING" || section == "GRAD" {
				productMap["GRADING"] = prodID
			} else if section == "PACKING" || section == "PACK" {
				productMap["PEELED"] = prodID
			}
		}
		productCursor.Close(ctx)
	}
	
	// Fallback to default IDs if not found
	if productMap["RCN"] == "" {
		productMap["RCN"] = "RCN"
	}
	if productMap["STEAMEDRCN"] == "" {
		productMap["STEAMEDRCN"] = "STEAMEDRCN"
	}
	if productMap["SH_WHOLES"] == "" {
		productMap["SH_WHOLES"] = "SH_WHOLES"
	}
	if productMap["BORMA"] == "" {
		productMap["BORMA"] = "BORMA"
	}
	if productMap["GRADING"] == "" {
		productMap["GRADING"] = "GRADING"
	}
	if productMap["PEELED"] == "" {
		productMap["PEELED"] = "PEELED"
	}
	
	fmt.Printf("[TrialData] Product Mapping: %+v\n", productMap)
	fmt.Printf("[TrialData] Total Products: %d\n", len(allProducts))
	
	// Fetch origins to use dynamically
	var origins []bson.M
	oCursor, _ := db.Collection("origin").Find(ctx, bson.M{})
	if oCursor != nil {
		oCursor.All(ctx, &origins)
	}
	defaultOriginID := "INDIA"
	if len(origins) > 0 {
		// Pick first one as default
		defaultOriginID = fmt.Sprintf("%v", origins[0]["_id"])
	}
	
	// Initialize stock for ALL products in all warehouses
	for _, wh := range warehouses {
		whID := wh["_id"].(string)
		
		// Get first origin as default
		defaultOrigin := defaultOriginID
		
		for _, prodID := range allProducts {
			// Get product details
			var productDoc bson.M
			db.Collection("product").FindOne(ctx, bson.M{"_id": prodID}).Decode(&productDoc)
			
			groupID := "DEFAULT"
			if productDoc != nil {
				if gid, ok := productDoc["group_id"].(string); ok && gid != "" {
					groupID = gid
				}
			}
			
			// Create initial stock entry with 100 kg for each product
			initialStock := 100.0
			stockID := uuid.New().String()
			
			// Insert into stock_in_hand
			db.Collection("stock_in_hand").InsertOne(ctx, bson.M{
				"_id":           stockID,
				"org_id":        req.OrgID,
				"product_id":    prodID,
				"warehouse_id":  whID,
				"stock_type":    "WIP",
				"available_qty": initialStock,
				"quantity":      initialStock,
				"status":        "Active",
				"created_on":    startTime,
				"location":      whID,
				"origin":        defaultOrigin,
				"group_id":      groupID,
			})
			
			// Insert into stock_ledger
			db.Collection("stock_ledger").InsertOne(ctx, bson.M{
				"_id":                 uuid.New().String(),
				"org_id":              req.OrgID,
				"product_id":          prodID,
				"warehouse_id":        whID,
				"stock_type":          "WIP",
				"transaction_type":    "opening_stock",
				"transaction_balance": initialStock,
				"opening_balance":     0,
				"closing_balance":     initialStock,
				"transaction_date":    startTime,
				"created_on":          startTime,
				"created_by":          "SYSTEM",
				"status":              "Active",
				"remarks":             "Initial stock for trial data",
				"location":            whID,
				"origin":              defaultOrigin,
				"group_id":            groupID,
			})
		}
	}
	fmt.Printf("[TrialData] Initialized stock for %d products in %d warehouses\n", len(allProducts), len(warehouses))

	purchaseOriginMap := make(map[string]string)

	bagWeight := 80.0
	for i := 0; i <= req.NoOfDays; i++ {
		currentDate := startTime.AddDate(0, 0, i)
		updateStatus(fmt.Sprintf("Simulating data for %s...", currentDate.Format("2006-01-02")), "InProgress")

		for _, factory := range factories {
			factoryID := factory["_id"].(string)

			// Factory-specific Bags and OutTurn
			fBags := perDayOpInBags
			fOutTurn := purchaseOutTurn
			if cfg, ok := factoryConfigs[factoryID]; ok {
				if cfg.Bags > 0 {
					fBags = cfg.Bags
				}
				if cfg.OutTurn > 0 {
					fOutTurn = cfg.OutTurn
				}
			}
			dailyBatchSize := float64(fBags) * bagWeight
			// Assign a consistent warehouse for this factory's operations today
			warehouseIdx := i % len(warehouses)
			warehouseID := warehouses[warehouseIdx]["_id"].(string)

			fProcesses, _ := factory["factory_processes"].(primitive.A)
			// Fallback: If processes are missing in the factory doc, fetch from factory_process collection
			if len(fProcesses) == 0 {
				var fpList []bson.M
				fpCursor, _ := db.Collection("factory_process").Find(ctx, bson.M{"factory_id": factoryID})
				if fpCursor != nil {
					fpCursor.All(ctx, &fpList)
					for _, fp := range fpList {
						if pID, ok := fp["process_id"].(string); ok {
							fProcesses = append(fProcesses, pID)
						}
					}
				}
			}

			hasProc := func(p string) bool {
				normalize := func(name string) string {
					name = strings.ToUpper(name)
					if name == "MC-SH" || name == "ML-SH" || name == "SHELL" {
						return "SHELL"
					}
					if name == "BORM" || name == "BORMA" {
						return "BORMA"
					}
					if name == "GRAD" || name == "GRADING" {
						return "GRAD"
					}
					if name == "COOKING" {
						return "COOK"
					}
					if name == "PACK" || name == "PACKING" {
						return "PACK"
					}
					return name
				}

				target := normalize(p)
				for _, fp := range fProcesses {
					sfp := normalize(fmt.Sprintf("%v", fp))
					mapped := normalize(simulationGroupMap[sfp])
					if sfp == target || mapped == target {
						return true
					}
				}
				return false
			}

			fmt.Printf("[TrialData] Day %d | Fact: %s | Whse: %s | Procs: %v\n", i, factoryID, warehouseID, fProcesses)

			// A. Purchase (Refill if low in THIS warehouse)
			if stockRCN[warehouseID] < dailyBatchSize {
				// Pick a random origin for each purchase if available
				originID := defaultOriginID
				if len(origins) > 0 {
					originID = fmt.Sprintf("%v", origins[rand.Intn(len(origins))]["_id"])
				}
				
				// Pick a random customer from customer collection
				var customers []bson.M
				customerID := "SYS-SUPPLIER" // Default fallback
				cursor, _ := db.Collection("customer").Find(ctx, bson.M{"status": "Active"})
				if cursor != nil {
					cursor.All(ctx, &customers)
					if len(customers) > 0 {
						randomCustomer := customers[rand.Intn(len(customers))]
						customerID = fmt.Sprintf("%v", randomCustomer["_id"])
					}
				}

				purchaseQty := dailyBatchSize * 5
				id := fmt.Sprintf("DP-%s-%s-%03d", originID, currentDate.Format("06"), i+1)

				invoiceDoc := bson.M{
					"_id":               uuid.New().String(),
					"purchase_id":       id,
					"warehouse_id":      warehouseID,
					"quantity":          purchaseQty,
					"invoice_amount":    purchaseQty * 180,
					"invoice_total":     purchaseQty * 180,
					"invoice_date":      currentDate,
					"invoice_number":    fmt.Sprintf("INV-%d", 1000+i),
					"no_of_bags":        int(purchaseQty / 80),
					"e_way_bill_number": fmt.Sprintf("EWB-%d", 5000+i),
					"gst_amount":        (purchaseQty * 180) * 0.05,
					"update_by":         "SYSTEM",
					"update_on":         currentDate,
					"cno_to":            "Warehouse",
				}

				purchaseDoc := bson.M{
					"_id":                id,
					"purchase_out_turn":  fOutTurn,
					"purchasetype":       "domestic",
					"purchase_weight":    purchaseQty,
					"quantity":           purchaseQty, // Legacy support
					"product_id":         productMap["RCN"],
					"product_name":       "RAW CASHEW NUT",
					"dop":                currentDate,
					"customer_id":        customerID,
					"gst_number":         "33AACCD8919J2ZG",
					"purchase_nut_count": 150 + rand.Intn(20),
					"status":             "Active",
					"is_available_for_production_and_warehouse_transfer": true,
					"country_origin":  originID,
					"reference":       "Automated Refill",
					"bill_to":         uuid.New().String(),
					"org_id":          req.OrgID,
					"status_type":     "Advanced Paid",
					"created_on":      currentDate,
					"created_by":      "SYSTEM",
					"purchase_price":  180 + rand.Intn(20),
					"invoice_details": []bson.M{invoiceDoc},
					"payment_details": []interface{}{},
				}

				db.Collection("purchase").InsertOne(ctx, purchaseDoc)
				db.Collection("invoice_details").InsertOne(ctx, invoiceDoc)
				updateStock(db, req.OrgID, productMap["RCN"], warehouseID, factoryID, "purchase", id, purchaseQty, currentDate, "RCN", warehouseID, false, id, originID, customerID, "")
				stockRCN[warehouseID] += purchaseQty
				latestPurchaseByWh[warehouseID] = id
				purchaseOriginMap[id] = originID
			}

			// B. COOK
			hasCook := hasProc("COOK")
			if hasCook && stockRCN[warehouseID] >= dailyBatchSize {
				fmt.Printf("[TrialData]   -> Executing COOK (Ref: %v)\n", warehouseID)
				stockRCN[warehouseID] -= dailyBatchSize
				pID := latestPurchaseByWh[warehouseID]
				pOrigin := purchaseOriginMap[pID]
				if pOrigin == "" {
					pOrigin = defaultOriginID
				}
				id := uuid.New().String()

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

				// Get equipment from equipments collection
				equipmentName := "COOKERS"
				equipmentID := ""
				eqID, eqName := getEquipmentFromDB(db, "COOK", factoryID)
				if eqID != "" {
					equipmentID = eqID
					equipmentName = eqName
				}

				prodDoc := bson.M{
					"_id":                     id,
					"factory_id":              factoryID,
					"warehouse_id":            warehouseID,
					"process_type":            "COOK",
					"input_weight":            dailyBatchSize,
					"STEAMEDRCN":              dailyBatchSize,
					"purchase_id":             pID,
					"status":                  "Start",
					"origin":                  pOrigin,
					"product_name":            "RAW CASHEW NUT",
					"created_by":              "SYSTEM",
					"created_on":              currentDate,
					"org_id":                  req.OrgID,
					"duration":                8,
					"stones_weight":           0,
					"template_id":             templatesByProcess["COOK"],
					"machine_name":            equipmentName,
					"process_start_date_time": currentDate,
					"price_per_kg":            12,
					"worker_id":               workerID,
					"equipment_name":          equipmentName,
					"equipment_id":            equipmentID,
				}

				db.Collection("productions").InsertOne(ctx, prodDoc)
				// Input RCN
				updateStock(db, req.OrgID, productMap["RCN"], warehouseID, factoryID, "production", id, -dailyBatchSize, currentDate, "RCN", warehouseID, false, pID, pOrigin, "", "")
				// Output STEAMEDRCN
				updateStock(db, req.OrgID, productMap["STEAMEDRCN"], warehouseID, factoryID, "production", id, dailyBatchSize, currentDate, "WIP", "COOK", true, pID, pOrigin, "", "")
				stockSteamed[warehouseID] += dailyBatchSize
			} else if hasCook {
				fmt.Printf("[TrialData]   -> Skipped COOK (Low Stock: %v/%v)\n", stockRCN[warehouseID], dailyBatchSize)
			}

			// C. SHELL
			hasShell := hasProc("SHELL")
			if hasShell && stockSteamed[warehouseID] >= dailyBatchSize {
				fmt.Printf("[TrialData]   -> Executing SHELL\n")
				stockSteamed[warehouseID] -= dailyBatchSize
				pID := latestPurchaseByWh[warehouseID]
				pOrigin := purchaseOriginMap[pID]
				if pOrigin == "" {
					pOrigin = defaultOriginID
				}
				id := uuid.New().String()
				output := dailyBatchSize * 0.25

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

				// Get equipment from equipments collection
				equipmentName := "SHELLING MACHINE"
				equipmentID := ""
				eqID, eqName := getEquipmentFromDB(db, "SHELL", factoryID)
				if eqID != "" {
					equipmentID = eqID
					equipmentName = eqName
				}

				prodDoc := bson.M{
					"_id":                     id,
					"factory_id":              factoryID,
					"warehouse_id":            warehouseID,
					"process_type":            "SHELL",
					"input_weight":            dailyBatchSize,
					"SH_WHOLES":               output,
					"purchase_id":             pID,
					"status":                  "Start",
					"origin":                  pOrigin,
					"created_by":              "SYSTEM",
					"created_on":              currentDate,
					"org_id":                  req.OrgID,
					"duration":                8,
					"stones_weight":           0,
					"template_id":             templatesByProcess["SHELL"],
					"process_start_date_time": currentDate,
					"price_per_kg":            12,
					"worker_id":               workerID,
					"equipment_name":          equipmentName,
					"equipment_id":            equipmentID,
					"machine_name":            equipmentName,
					"equipmentName":           equipmentName,
				}

				db.Collection("productions").InsertOne(ctx, prodDoc)
				// Input STEAMEDRCN
				updateStock(db, req.OrgID, productMap["STEAMEDRCN"], warehouseID, factoryID, "production", id, -dailyBatchSize, currentDate, "WIP", "COOK", false, pID, pOrigin, "", "")
				// Output SH_WHOLES
				updateStock(db, req.OrgID, productMap["SH_WHOLES"], warehouseID, factoryID, "production", id, output, currentDate, "WIP", "SHELL", true, pID, pOrigin, "", "")
				stockBorma[warehouseID] += output
			} else if hasShell {
				fmt.Printf("[TrialData]   -> Skipped SHELL (Stock: %v)\n", stockSteamed[warehouseID])
			}

			// D. BORMA
			hasBorma := hasProc("BORMA") || hasProc("BORM")
			if hasBorma && stockBorma[warehouseID] > 0 {
				fmt.Printf("[TrialData]   -> Executing BORMA\n")
				input := stockBorma[warehouseID]
				stockBorma[warehouseID] = 0
				pID := latestPurchaseByWh[warehouseID]
				pOrigin := purchaseOriginMap[pID]
				if pOrigin == "" {
					pOrigin = defaultOriginID
				}
				id := uuid.New().String()
				output := input * 0.98

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

				// Get equipment from equipments collection
				equipmentName := "BORMA MACHINE"
				equipmentID := ""
				eqID, eqName := getEquipmentFromDB(db, "BORM", factoryID)
				if eqID != "" {
					equipmentID = eqID
					equipmentName = eqName
				}

				prodDoc := bson.M{
					"_id":                     id,
					"factory_id":              factoryID,
					"warehouse_id":            warehouseID,
					"process_type":            "BORMA",
					"input_weight":            input,
					"BR_WHOLES":               output,
					"purchase_id":             pID,
					"status":                  "Start",
					"origin":                  pOrigin,
					"created_by":              "SYSTEM",
					"created_on":              currentDate,
					"org_id":                  req.OrgID,
					"template_id":             templatesByProcess["BORMA"],
					"process_start_date_time": currentDate,
					"price_per_kg":            12,
					"worker_id":               workerID,
					"equipment_name":          equipmentName,
					"equipment_id":            equipmentID,
					"machine_name":            equipmentName,
					"equipmentName":           equipmentName,
				}

				db.Collection("productions").InsertOne(ctx, prodDoc)
				// Input SH_WHOLES
				updateStock(db, req.OrgID, productMap["SH_WHOLES"], warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "SHELL", false, pID, pOrigin, "", "")
				// Output BORMA
				updateStock(db, req.OrgID, productMap["BORMA"], warehouseID, factoryID, "production", id, output, currentDate, "WIP", "BORMA", true, pID, pOrigin, "", "")
				stockGrading[warehouseID] += output
			}

			// E. GRAD
			hasGrad := hasProc("GRAD") || hasProc("GRADING")
			if hasGrad && stockGrading[warehouseID] > 0 {
				input := stockGrading[warehouseID]
				stockGrading[warehouseID] = 0
				pID := latestPurchaseByWh[warehouseID]
				pOrigin := purchaseOriginMap[pID]
				if pOrigin == "" {
					pOrigin = defaultOriginID
				}
				id := uuid.New().String()

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

				// Get equipment from equipments collection
				equipmentName := "GRADING MACHINE"
				equipmentID := ""
				eqID, eqName := getEquipmentFromDB(db, "GRAD", factoryID)
				if eqID != "" {
					equipmentID = eqID
					equipmentName = eqName
				}

				prodDoc := bson.M{
					"_id":                     id,
					"factory_id":              factoryID,
					"warehouse_id":            warehouseID,
					"process_type":            "GRAD",
					"input_weight":            input,
					"GRADING":                 input,
					"P_ALL_WHOLES":            input,
					"WW":                      input * (rand.Float64() * 0.4),
					"SW":                      input * (rand.Float64() * 0.5),
					"PKW":                     input * (rand.Float64() * 0.15),
					"LWP":                     input * (rand.Float64() * 0.08),
					"DW":                      input * (rand.Float64() * 0.05),
					"UPP":                     input * (rand.Float64() * 0.03),
					"UPW":                     input * (rand.Float64() * 0.3),
					"OW":                      input * (rand.Float64() * 0.02),
					"BUDS":                    input * (rand.Float64() * 0.1),
					"S":                       input * (rand.Float64() * 0.05),
					"HUSK":                    input * (rand.Float64() * 0.02),
					"purchase_id":             pID,
					"status":                  "Start",
					"origin":                  pOrigin,
					"created_by":              "SYSTEM",
					"created_on":              currentDate,
					"org_id":                  req.OrgID,
					"template_id":             templatesByProcess["GRAD"],
					"process_start_date_time": currentDate,
					"price_per_kg":            12,
					"worker_id":               workerID,
					"equipment_name":          equipmentName,
					"equipment_id":            equipmentID,
					"machine_name":            equipmentName,
					"equipmentName":           equipmentName,
				}
				output := input // Add this to define output

				db.Collection("productions").InsertOne(ctx, prodDoc)
				// Input BORMA
				updateStock(db, req.OrgID, productMap["BORMA"], warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "BORMA", false, pID, pOrigin, "", "")
				// Output GRADING
				updateStock(db, req.OrgID, productMap["GRADING"], warehouseID, factoryID, "production", id, output, currentDate, "WIP", "GRAD", true, pID, pOrigin, "", "")
				stockPeeled[warehouseID] += output
			}

			// F. PACK (Only pack when we have significant accumulated stock)
			// Pack only if we have at least 50% of daily batch to avoid negative values
			minPackingStock := dailyBatchSize * 0.5
			hasPack := hasProc("PACK")
			if hasPack && stockPeeled[warehouseID] > minPackingStock {
				fmt.Printf("[TrialData]   -> Executing PACKING\n")
				input := stockPeeled[warehouseID]
				stockPeeled[warehouseID] = 0
				pID := latestPurchaseByWh[warehouseID]
				pOrigin := purchaseOriginMap[pID]
				if pOrigin == "" {
					pOrigin = defaultOriginID
				}
				id := uuid.New().String()

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

				// Get equipment from equipments collection
				equipmentName := "PACKING MACHINE"
				equipmentID := ""
				eqID, eqName := getEquipmentFromDB(db, "PACK", factoryID)
				if eqID != "" {
					equipmentID = eqID
					equipmentName = eqName
				}

				prodDoc := bson.M{
					"_id":                     id,
					"factory_id":              factoryID,
					"warehouse_id":            warehouseID,
					"process_type":            "PACK",
					"input_weight":            input,
					"PEELED":                  input,
					"purchase_id":             pID,
					"status":                  "Start",
					"origin":                  pOrigin,
					"created_by":              "SYSTEM",
					"created_on":              currentDate,
					"org_id":                  req.OrgID,
					"template_id":             templatesByProcess["PACK"],
					"process_start_date_time": currentDate,
					"price_per_kg":            12,
					"worker_id":               workerID,
					"equipment_name":          equipmentName,
					"equipment_id":            equipmentID,
					"machine_name":            equipmentName,
					"equipmentName":           equipmentName,
				}
				output := input // Add this to define output
				db.Collection("productions").InsertOne(ctx, prodDoc)
				// Input GRADING
				updateStock(db, req.OrgID, productMap["GRADING"], warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "GRAD", false, pID, pOrigin, "", "")
				// Output PEELED (changed from FG to WIP)
				updateStock(db, req.OrgID, productMap["PEELED"], warehouseID, factoryID, "production", id, output, currentDate, "WIP", "PACK", true, pID, pOrigin, "", "")
				stockPacked[warehouseID] += output
				fmt.Printf("[TrialData]   -> Executing PACKING: Input: %.2f kg, Output: %.2f kg\n", input, output)
			} else if hasPack {
				fmt.Printf("[TrialData]   -> PACK SKIPPED: Stock %.2f kg < Minimum %.2f kg (Accumulating for batch)\n", stockPeeled[warehouseID], minPackingStock)
			}

			// G. SALE - DISABLED
			/*
			saleQty := 50.0
			minSaleStock := 100.0 // Need at least 100 kg to trigger sale
			saleID := fmt.Sprintf("DS-FY%s-%03d", currentDate.Format("06"), i+1)
			sold := false

			buildSaleDoc := func(prodID string, qty float64) bson.M {
				return bson.M{
					"_id":                saleID,
					"sold_on":            currentDate,
					"available_qty":      qty,
					"created_by":         "SYSTEM",
					"customer_id":        "ST-CUSTOMER",
					"warehouse":          warehouseID,
					"warehouse_id":       warehouseID,
					"type_of_sale":       "Domestic",
					"org_id":             req.OrgID,
					"created_on":         currentDate,
					"type":               "Sale",
					"bill_to":            uuid.New().String(),
					"gst_number":         "33AIGPN1867L2ZX",
					"gst":                7.5,
					"price":              200,
					"total_price":        qty * 200,
					"status":             "Active",
					"delivery_to":        uuid.New().String(),
					"purchase_id":        latestPurchaseByWh[warehouseID],
					"product_id":         prodID,
					"quantity":           qty,
					"description":        "Trial Sale",
					"transport_distance": 100,
					"vehicle_number":     "TN01-SYS-9999",
				}
			}

			pID := latestPurchaseByWh[warehouseID]
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}

			// Only sell if we have enough stock
			// Use EXACT product IDs that match stock tracking
			if stockPacked[warehouseID] > minSaleStock {
				stockPacked[warehouseID] -= saleQty
				db.Collection("sale").InsertOne(ctx, buildSaleDoc(productMap["PEELED"], saleQty))
				updateStock(db, req.OrgID, productMap["PEELED"], warehouseID, "", "sale", saleID, -saleQty, currentDate, "FG", warehouseID, false, pID, pOrigin)
				fmt.Printf("[TrialData]   -> Executing SALE (%s): %.2f kg (Remaining: %.2f kg)\n", productMap["PEELED"], saleQty, stockPacked[warehouseID])
				sold = true
			} else if stockPeeled[warehouseID] > minSaleStock {
				stockPeeled[warehouseID] -= saleQty
				db.Collection("sale").InsertOne(ctx, buildSaleDoc(productMap["GRADING"], saleQty))
				updateStock(db, req.OrgID, productMap["GRADING"], warehouseID, "", "sale", saleID, -saleQty, currentDate, "WIP", "GRAD", false, pID, pOrigin)
				fmt.Printf("[TrialData]   -> Executing SALE (%s): %.2f kg (Remaining: %.2f kg)\n", productMap["GRADING"], saleQty, stockPeeled[warehouseID])
				sold = true
			} else if stockGrading[warehouseID] > minSaleStock*2 {
				stockGrading[warehouseID] -= saleQty
				db.Collection("sale").InsertOne(ctx, buildSaleDoc(productMap["BORMA"], saleQty))
				updateStock(db, req.OrgID, productMap["BORMA"], warehouseID, "", "sale", saleID, -saleQty, currentDate, "WIP", "BORMA", false, pID, pOrigin)
				fmt.Printf("[TrialData]   -> Executing SALE (%s): %.2f kg (Remaining: %.2f kg)\n", productMap["BORMA"], saleQty, stockGrading[warehouseID])
				sold = true
			} else if stockBorma[warehouseID] > minSaleStock*3 {
				stockBorma[warehouseID] -= saleQty
				db.Collection("sale").InsertOne(ctx, buildSaleDoc(productMap["SH_WHOLES"], saleQty))
				updateStock(db, req.OrgID, productMap["SH_WHOLES"], warehouseID, "", "sale", saleID, -saleQty, currentDate, "WIP", "SHELL", false, pID, pOrigin)
				fmt.Printf("[TrialData]   -> Executing SALE (%s): %.2f kg (Remaining: %.2f kg)\n", productMap["SH_WHOLES"], saleQty, stockBorma[warehouseID])
				sold = true
			} else {
				fmt.Printf("[TrialData]   -> SALE SKIPPED: Insufficient stock (Packed: %.2f, Peeled: %.2f, Min: %.2f kg)\n", stockPacked[warehouseID], stockPeeled[warehouseID], minSaleStock)
			}

			if sold && i%3 == 0 {
				// handled
			}
			*/
		}
	}

	updateStatus("Onboarding data generated successfully", "Completed")
	return nil
}

func updateStock(db *mongo.Database, orgID, productID, warehouseID, factoryID, txType, docID string, qty float64, date time.Time, stockType, location string, isOutput bool, purchaseID string, originID string, customerID string, customerName string) {
	ctx := context.Background()

	// 1. Get current balance for Opening/Closing
	var stockInHand bson.M
	filter := bson.M{
		"org_id":       orgID,
		"product_id":   productID,
		"warehouse_id": warehouseID,
		"stock_type":   stockType,
	}

	if factoryID != "" {
		filter["factory_id"] = factoryID
	}

	if stockType == "WIP" && location != "" {
		filter["process_type"] = location
	}

	// If purchaseID is provided, use it in filter
	if purchaseID != "" {
		filter["purchase_id"] = purchaseID
		filter["origin"] = originID
	}

	db.Collection("stock_in_hand").FindOne(ctx, filter).Decode(&stockInHand)

	openingBalance := 0.0
	existingID := ""
	originalPurchaseID := purchaseID
	
	if stockInHand != nil {
		openingBalance = helper.ToFloat64(stockInHand["available_qty"])
		if openingBalance == 0 {
			openingBalance = helper.ToFloat64(stockInHand["quantity"])
		}
		existingID, _ = stockInHand["_id"].(string)
		
		// Get purchase_id from stock_in_hand if not provided
		if purchaseID == "" {
			if pid, ok := stockInHand["purchase_id"].(string); ok && pid != "" {
				purchaseID = pid
				originalPurchaseID = pid
			}
		}
	}
	
	// If still no purchase_id and this is an output, try to find it from input stock
	if purchaseID == "" && isOutput {
		// For output transactions, look for the input product's stock to inherit purchase_id
		// Try multiple product types that might be inputs
		inputProducts := []string{"RCN", "STEAMEDRCN", "SH_WHOLES", "BORMA", "GRADING", productID}
		
		for _, inputProd := range inputProducts {
			var inputStock bson.M
			err := db.Collection("stock_in_hand").FindOne(ctx, bson.M{
				"org_id":       orgID,
				"product_id":   inputProd,
				"warehouse_id": warehouseID,
				"status":       "Active",
			}).Decode(&inputStock)
			
			if err == nil && inputStock != nil {
				if pid, ok := inputStock["purchase_id"].(string); ok && pid != "" {
					purchaseID = pid
					originalPurchaseID = pid
					break
				}
			}
		}
	}
	
	closingBalance := openingBalance + qty

	// 2. Upsert stock_in_hand (Using $inc for safety just like production_stock_handler)
	updateFilter := filter
	update := bson.M{
		"$inc": bson.M{"available_qty": qty, "quantity": qty},
		"$set": bson.M{
			"last_updated_on": date,
			"status":          "Active",
			"origin":          originID,
			"factory_id":      factoryID,
			"location":        warehouseID,
			"purchase_id":     originalPurchaseID,
		},
	}
	if existingID == "" {
		update["$setOnInsert"] = bson.M{
			"_id":        uuid.New().String(),
			"created_on": date,
			"org_id":     orgID,
			"product_id": productID,
			"stock_type": stockType,
		}
		if stockType == "WIP" && location != "" {
			update["$setOnInsert"].(bson.M)["process_type"] = location
		}
	}
	db.Collection("stock_in_hand").UpdateOne(ctx, updateFilter, update, options.Update().SetUpsert(true))

	// 3. Insert into stock_ledger
	// Always pick a random customer from customer collection for trial data
	if customerID == "" {
		var customers []bson.M
		cursor, _ := db.Collection("customer").Find(ctx, bson.M{"status": "Active"})
		if cursor != nil {
			cursor.All(ctx, &customers)
			if len(customers) > 0 {
				randomCustomer := customers[rand.Intn(len(customers))]
				customerID = fmt.Sprintf("%v", randomCustomer["_id"])
			}
		}
	}
	
	// Fallback: if no customers in collection, try to get from purchase
	if customerID == "" && purchaseID != "" {
		var purchaseDoc bson.M
		if err := db.Collection("purchase").FindOne(ctx, bson.M{"_id": purchaseID}).Decode(&purchaseDoc); err == nil {
			if cid, ok := purchaseDoc["customer_id"].(string); ok {
				customerID = cid
			}
		}
	}
	
	ledgerDoc := bson.M{
		"_id":                 uuid.New().String(),
		"org_id":              orgID,
		"created_by":          "SYSTEM",
		"created_on":          time.Now(),
		"transaction_date":    date,
		"product_id":          productID,
		"stock_type":          stockType,
		"warehouse_id":        warehouseID,
		"location":            location,
		"factory_id":          factoryID,
		"purchase_id":         purchaseID,
		"origin":              originID,
		"transaction_type":    txType,
		"transaction_balance": qty,
		"opening_balance":     openingBalance,
		"closing_balance":     closingBalance,
		"customer_name":       customerID,
		"status":              "Active",
		"remarks":             "System generated trial data",
	}

	if isOutput {
		ledgerDoc["production_id"] = docID
	} else {
		ledgerDoc["ref_id"] = docID
	}

	_, err := db.Collection("stock_ledger").InsertOne(ctx, ledgerDoc)
	if err != nil {
		fmt.Printf("[TrialData Ledger Error] Failed to insert into stock_ledger for %s: %v\n", orgID, err)
	}
}

func generateFullEmployeeDocLocal(factoryID, unitID, orgID, designation string, namePool []string) bson.M {
	deductPF := rand.Intn(2) == 0
	gender := "Male"
	if rand.Intn(2) == 0 {
		gender = "Female"
	}

	pfConfig := 0
	pfUAN := ""
	if deductPF {
		pfConfig = 12
		pfUAN = fmt.Sprintf("%012d", rand.Intn(1000000000000))
	}

	name := "Default Employee"
	if len(namePool) > 0 {
		name = namePool[rand.Intn(len(namePool))]
	} else {
		names := []string{"Arun", "Balan", "Chitra", "Deepa", "Eswar", "Fathima", "Ganesh", "Hema", "Indira", "Jothi"}
		name = names[rand.Intn(len(names))] + " " + string(rune('A'+rand.Intn(26)))
	}

	payTypes := []string{"consolidated", "structured", "fixedWages", "outputBased"}
	payType := payTypes[rand.Intn(len(payTypes))]

	return bson.M{
		"_id":                      uuid.New().String(),
		"gender":                   gender,
		"employee_name":            name,
		"contact_mobile_number":    fmt.Sprintf("9%09d", rand.Intn(1000000000)),
		"aadhaar_card":             fmt.Sprintf("%012d", rand.Intn(1000000000000)),
		"pan_card":                 fmt.Sprintf("ABCDE%04dF", rand.Intn(10000)),
		"employee_street":          "Street " + fmt.Sprintf("%d", rand.Intn(100)),
		"employee_area_name":       "Area " + fmt.Sprintf("%d", rand.Intn(100)),
		"employee_city":            "Chennai",
		"employee_state":           "Tamil Nadu",
		"employee_country":         "INDIA",
		"employee_pincode":         fmt.Sprintf("%06d", rand.Intn(1000000)),
		"factory":                  factoryID,
		"unit":                     unitID,
		"designation":              designation,
		"joining_date":             time.Now(),
		"pay_type":                 payType,
		"overtime_salary_per_hour": 80,
		"food_allowance":           20,
		"bus_fare":                 10,
		"deduct_pf_esi":            deductPF,
		"pf_config":                pfConfig,
		"pf_uan_no":                pfUAN,
		"deduct_bonus":             false,
		"bonus_per_day":            15,
		"status":                   "Active",
		"created_on":               time.Now(),
		"org_id":                   orgID,
	}
}
