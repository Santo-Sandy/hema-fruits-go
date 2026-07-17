package onboarding

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

// StartDailyTrialDataScheduler - Runs every day at midnight to generate sample data
func StartDailyTrialDataScheduler() {
	fmt.Println("[Scheduler] Daily trial data scheduler started")
	
	// Run immediately on startup (optional - comment out if you don't want immediate run)
	go runDailyGeneration()
	
	go func() {
		for {
			// Calculate time until next midnight
			now := time.Now()
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			timeUntilMidnight := nextMidnight.Sub(now)
			
			fmt.Printf("[Scheduler] Next run scheduled at: %s (in %v)\n", nextMidnight.Format("2006-01-02 15:04:05"), timeUntilMidnight)
			
			// Wait until midnight
			time.Sleep(timeUntilMidnight)
			
			// Run the generation
			runDailyGeneration()
		}
	}()
}

func runDailyGeneration() {
	fmt.Println("[Scheduler] Running daily trial data generation at", time.Now().Format("2006-01-02 15:04:05"))
	
	ctx := context.Background()
	commonDB := database.SharedDB.Client().Database(COMMON_CONFIG_DB)
	
	// Find all organizations with trial data enabled
	cursor, err := commonDB.Collection(ONBOARD_CONFIG_TABLE).Find(ctx, bson.M{
		"payload.trialData.wantTrialData": true,
	})
	
	if err != nil {
		fmt.Printf("[Scheduler] Error finding trial orgs: %v\n", err)
		return
	}
	
	var configs []struct {
		ID      string            `bson:"_id"`
		Payload OnboardingRequest `bson:"payload"`
	}
	
	if err := cursor.All(ctx, &configs); err != nil {
		fmt.Printf("[Scheduler] Error decoding configs: %v\n", err)
		return
	}
	
	fmt.Printf("[Scheduler] Found %d organizations with trial data enabled\n", len(configs))
	
	for _, config := range configs {
		fmt.Printf("[Scheduler] Checking org: %s\n", config.ID)

		var targetOrgID string
		if config.ID == "ORG-2026-80" || config.ID == "ORG-2026-81" {
			targetOrgID = config.ID
		} else {
			targetOrgID = config.ID + "_demo"
		}

		orgDB := database.GetConnection(targetOrgID)
		factoryCount, _ := orgDB.Collection("factory").CountDocuments(ctx, bson.M{})
		if factoryCount == 0 {
			fmt.Printf("[Scheduler] Skipping %s - No factories found\n", targetOrgID)
			continue
		}

		fmt.Printf("[Scheduler] Generating daily data for: %s\n", targetOrgID)

		req := TrialDataRequest{
			OrgID:    targetOrgID,
			NoOfDays: 1,
			Region:   config.Payload.TrialData.Language,
		}

		if req.Region == "" {
			req.Region = "Tamil"
		}

		if err := GenerateDailyTrialData(req, config.Payload); err != nil {
			fmt.Printf("[Scheduler] Error generating data for %s: %v\n", targetOrgID, err)
		} else {
			fmt.Printf("[Scheduler] Successfully generated data for %s\n", targetOrgID)
		}
	}
}

// GenerateDailyTrialData - Generates sample data for TODAY only
func GenerateDailyTrialData(req TrialDataRequest, payload OnboardingRequest) error {
	ctx := context.Background()
	db := database.GetConnection(req.OrgID)
	
	currentDate := time.Now()
	
	// Simulation Parameters
	noOfEmployee := 20
	perDayOpInBags := 60
	purchaseOutTurn := 45.0
	
	// Fetch Config and Overrides
	factoryConfigs := make(map[string]struct {
		Bags    int
		OutTurn float64
	})
	
	totalEmp := 0
	totalBags := 0
	pOutTurn := 0.0
	
	for _, f := range payload.Factories {
		fID := fmt.Sprintf("FAC--%03d", f.FactoryIndex)
		if f.FactoryIndex == 0 {
			fID = "FAC--001"
		}
		
		fBags := f.PerDayOpInBags
		fOutTurnLocal := f.PurchaseOutTurn
		
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
	
	fmt.Printf("[DailyData] Config: Emp=%d, Bags=%d, OutTurn=%.2f\n", noOfEmployee, perDayOpInBags, purchaseOutTurn)
	
	// Fetch factories
	var factories []bson.M
	fCursor, _ := db.Collection("factory").Find(ctx, bson.M{})
	fCursor.All(ctx, &factories)
	if len(factories) == 0 {
		return fmt.Errorf("no factories found")
	}
	
	// Fetch warehouses
	var warehouses []bson.M
	wCursor, _ := db.Collection("company").Find(ctx, bson.M{})
	wCursor.All(ctx, &warehouses)
	if len(warehouses) == 0 {
		return fmt.Errorf("no warehouses found")
	}
	
	// Get stock levels from database (refresh for each factory)
	var stockRCN, stockSteamed, stockBorma, stockGrading, stockPeeled, stockPacked map[string]float64
	
	refreshStock := func() {
		stockRCN = getStockFromDB(db, "RCN", req.OrgID)
		stockSteamed = getStockFromDB(db, "STEAMEDRCN", req.OrgID)
		stockBorma = getStockFromDB(db, "SH_WHOLES", req.OrgID)
		stockGrading = getStockFromDB(db, "BORMA", req.OrgID)
		stockPeeled = getStockFromDB(db, "GRADING", req.OrgID)
		stockPacked = getStockFromDB(db, "PEELED", req.OrgID)
	}
	
	// Initial stock fetch
	refreshStock()
	
	// Fetch origins
	var origins []bson.M
	oCursor, _ := db.Collection("origin").Find(ctx, bson.M{})
	if oCursor != nil {
		oCursor.All(ctx, &origins)
	}
	defaultOriginID := "INDIA"
	if len(origins) > 0 {
		defaultOriginID = fmt.Sprintf("%v", origins[0]["_id"])
	}
	
	latestPurchaseByWh := make(map[string]string)
	purchaseOriginMap := make(map[string]string)
	
	// Get latest purchase IDs
	for _, wh := range warehouses {
		whID := wh["_id"].(string)
		var purchase bson.M
		db.Collection("purchase").FindOne(ctx, bson.M{
			"warehouse_id": whID,
		}, nil).Decode(&purchase)
		if purchase != nil {
			if pid, ok := purchase["_id"].(string); ok {
				latestPurchaseByWh[whID] = pid
				if origin, ok := purchase["country_origin"].(string); ok {
					purchaseOriginMap[pid] = origin
				}
			}
		}
	}
	
	// Build process templates map
	templatesByProcess := make(map[string]string)
	tCursor, _ := db.Collection("process").Find(ctx, bson.M{})
	for tCursor.Next(ctx) {
		var t bson.M
		tCursor.Decode(&t)
		pID := fmt.Sprintf("%v", t["_id"])
		
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
	}
	
	bagWeight := 80.0
	
	// Process each factory
	for idx, factory := range factories {
		factoryID := factory["_id"].(string)
		warehouseID := warehouses[idx%len(warehouses)]["_id"].(string)
		
		// Refresh stock from database before processing each factory
		refreshStock()
		
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
		
		// Use primitive.A (not bson.A) — MongoDB returns primitive.A for arrays
		fProcesses, _ := factory["factory_processes"].(primitive.A)
		// Fallback: if factory_processes is empty, fetch from factory_process collection
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

		fmt.Printf("[DailyData] Factory processes for %s: %v\n", factoryID, fProcesses)

		normalizeProc := func(name string) string {
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
			if name == "PACKING" {
				return "PACK"
			}
			return name
		}

		hasProc := func(p string) bool {
			target := normalizeProc(p)
			for _, fp := range fProcesses {
				if normalizeProc(fmt.Sprintf("%v", fp)) == target {
					return true
				}
			}
			return false
		}
		
		fmt.Printf("[DailyData] Factory: %s | Warehouse: %s | Date: %s\n", factoryID, warehouseID, currentDate.Format("2006-01-02"))
		
		// A. Purchase if needed
		if stockRCN[warehouseID] < dailyBatchSize {
			originID := defaultOriginID
			if len(origins) > 0 {
				originID = fmt.Sprintf("%v", origins[rand.Intn(len(origins))]["_id"])
			}
			
			// Purchase enough for multiple days to avoid frequent purchases
			purchaseQty := dailyBatchSize * 10  // Changed from 5 to 10 days worth
			id := fmt.Sprintf("DP-%s-%s-%d", originID, currentDate.Format("060102"), time.Now().Unix()%10000)
			
			invoiceDoc := bson.M{
				"_id":               uuid.New().String(),
				"purchase_id":       id,
				"warehouse_id":      warehouseID,
				"quantity":          purchaseQty,
				"invoice_amount":    purchaseQty * 180,
				"invoice_total":     purchaseQty * 180,
				"invoice_date":      currentDate,
				"invoice_number":    fmt.Sprintf("INV-%d", time.Now().Unix()%10000),
				"no_of_bags":        int(purchaseQty / 80),
				"e_way_bill_number": fmt.Sprintf("EWB-%d", time.Now().Unix()%10000),
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
				"quantity":           purchaseQty,
				"product_id":         "RCN",
				"product_name":       "RAW CASHEW NUT",
				"dop":                currentDate,
				"customer_id":        "SYS-SUPPLIER",
				"gst_number":         "33AACCD8919J2ZG",
				"purchase_nut_count": 150 + rand.Intn(20),
				"status":             "Active",
				"is_available_for_production_and_warehouse_transfer": true,
				"country_origin":  originID,
				"reference":       "Automated Daily Refill",
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
			updateStock(db, req.OrgID, "RCN", warehouseID, factoryID, "purchase", id, purchaseQty, currentDate, "RCN", warehouseID, false, id, originID, "SYS-SUPPLIER", "")
			
			// Update in-memory stock immediately
			stockRCN[warehouseID] += purchaseQty
			latestPurchaseByWh[warehouseID] = id
			purchaseOriginMap[id] = originID
			
			// Refresh from DB to ensure accuracy
			refreshStock()
			
			fmt.Printf("[DailyData] -> Purchase: %.2f kg (New Stock: %.2f kg)\n", purchaseQty, stockRCN[warehouseID])
		}
		
		// B. COOK
		if hasProc("COOK") && stockRCN[warehouseID] >= dailyBatchSize {
			fmt.Printf("[DailyData] -> COOK: Stock before: %.2f kg\n", stockRCN[warehouseID])
			
			stockRCN[warehouseID] -= dailyBatchSize
			pID := latestPurchaseByWh[warehouseID]
			
			// If no purchase for this warehouse, fetch from purchase collection
			if pID == "" {
				var purchase bson.M
				err := db.Collection("purchase").FindOne(ctx, bson.M{
					"org_id": req.OrgID,
					"status": "Active",
				}).Decode(&purchase)
				if err == nil && purchase != nil {
					if pid, ok := purchase["_id"].(string); ok {
						pID = pid
						latestPurchaseByWh[warehouseID] = pID
						if origin, ok := purchase["country_origin"].(string); ok {
							purchaseOriginMap[pID] = origin
						}
					}
				}
			}
			
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}
			// Generate complete production document with all fields including equipment
			prodDoc := GenerateCompleteProductionDoc(
				"COOK",
				factoryID,
				warehouseID,
				pID,
				pOrigin,
				req.OrgID,
				templatesByProcess["COOK"],
				dailyBatchSize,
				dailyBatchSize,
				currentDate,
				"COOKERS",
			)
			id := prodDoc["_id"].(string)
			
			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "RCN", warehouseID, factoryID, "production", id, -dailyBatchSize, currentDate, "RCN", warehouseID, false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "STEAMEDRCN", warehouseID, factoryID, "production", id, dailyBatchSize, currentDate, "WIP", "COOK", true, pID, pOrigin, "", "")
			stockSteamed[warehouseID] += dailyBatchSize
			
			fmt.Printf("[DailyData] -> COOK: Input: %.2f kg, Output: %.2f kg, RCN Stock: %.2f kg\n", dailyBatchSize, dailyBatchSize, stockRCN[warehouseID])
		} else if hasProc("COOK") {
			fmt.Printf("[DailyData] -> COOK SKIPPED: Stock %.2f kg < Required %.2f kg\n", stockRCN[warehouseID], dailyBatchSize)
		}
		
		// C. SHELL
		if hasProc("SHELL") && stockSteamed[warehouseID] >= dailyBatchSize {
			fmt.Printf("[DailyData] -> SHELL: Stock before: %.2f kg\n", stockSteamed[warehouseID])
			
			stockSteamed[warehouseID] -= dailyBatchSize
			pID := latestPurchaseByWh[warehouseID]
			
			// If no purchase for this warehouse, fetch from purchase collection
			if pID == "" {
				var purchase bson.M
				err := db.Collection("purchase").FindOne(ctx, bson.M{
					"org_id": req.OrgID,
					"status": "Active",
				}).Decode(&purchase)
				if err == nil && purchase != nil {
					if pid, ok := purchase["_id"].(string); ok {
						pID = pid
						latestPurchaseByWh[warehouseID] = pID
						if origin, ok := purchase["country_origin"].(string); ok {
							purchaseOriginMap[pID] = origin
						}
					}
				}
			}
			
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
			
			// Get equipment name from equipments collection
			equipmentName := "SHELLING MACHINE"
			var equipment bson.M
			err := db.Collection("equipments").FindOne(ctx, bson.M{
				"process_id": "SHELL",
				"status":     "Active",
			}).Decode(&equipment)
			if err == nil && equipment != nil {
				if name, ok := equipment["machine_name"].(string); ok && name != "" {
					equipmentName = name
				}
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
				"template_id":             templatesByProcess["SHELL"],
				"process_start_date_time": currentDate,
				"price_per_kg":            12,
				"worker_id":               workerID,
				"equipment_name":          equipmentName,
			}
			
			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "STEAMEDRCN", warehouseID, factoryID, "production", id, -dailyBatchSize, currentDate, "WIP", "COOK", false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "SH_WHOLES", warehouseID, factoryID, "production", id, output, currentDate, "WIP", "SHELL", true, pID, pOrigin, "", "")
			stockBorma[warehouseID] += output
			
			fmt.Printf("[DailyData] -> SHELL: Input: %.2f kg, Output: %.2f kg, Steamed Stock: %.2f kg\n", dailyBatchSize, output, stockSteamed[warehouseID])
		} else if hasProc("SHELL") {
			fmt.Printf("[DailyData] -> SHELL SKIPPED: Stock %.2f kg < Required %.2f kg\n", stockSteamed[warehouseID], dailyBatchSize)
		}
		
		// D. BORMA
		if hasProc("BORMA") && stockBorma[warehouseID] > 0 {
			input := stockBorma[warehouseID]
			stockBorma[warehouseID] = 0
			pID := latestPurchaseByWh[warehouseID]
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}
			id := uuid.New().String()

			// Split output: ~80% wholes + ~20% pieces (matches real doc ratio)
			brWholes := input * 0.735  // ~73.5% of input
			shWholes := input * 0.245  // ~24.5% of input (SH_WHOLES going in)
			brPieces := brWholes * (279.62 / 1198.38) // realistic pieces ratio
			shPieces := shWholes * (280.0 / 1200.0)   // realistic pieces ratio
			outputWeight := brWholes + brPieces
			diff := input - outputWeight
			diffPct := 0.0
			if input > 0 {
				diffPct = diff / input * 100
			}
			trolleyWeight := input * 0.092 // ~9.2% trolley weight
			equipmentID, equipmentName := getEquipmentFromDB(db, "BORM", factoryID)
			coolingEquipID, _ := getEquipmentFromDB(db, "COOL", factoryID)
			unitID := getRandomUnitID()
			processEndTime := currentDate.Add(1 * time.Hour)

			prodDoc := bson.M{
				"_id":                     id,
				"factory_id":              factoryID,
				"warehouse_id":            warehouseID,
				"process_type":            "BORM",
				"input_weight":            input,
				"output_weight":           outputWeight,
				"SH_WHOLES":               shWholes,
				"BR_WHOLES":               brWholes,
				"SH_PIECES":               shPieces,
				"BR_PIECES":               brPieces,
				"borma_product":           "NW WHOLES & NW PIECES",
				"trolley_weight":          trolleyWeight,
				"difference":              diff,
				"diff_in_percentage":      diffPct,
				"purchase_id":             pID,
				"status":                  "Completed",
				"origin":                  pOrigin,
				"created_by":              "SYSTEM",
				"created_on":              currentDate,
				"update_by":               "SYSTEM",
				"update_on":               currentDate,
				"org_id":                  req.OrgID,
				"template_id":             templatesByProcess["BORMA"],
				"unit_id":                 unitID,
				"process_id":              3,
				"equipment_id":            equipmentID,
				"cooling_equipment_id":    coolingEquipID,
				"equipmentName":           equipmentName,
				"process_start_date_time": currentDate,
				"process_end_date_time":   processEndTime,
				"image_upload":            []interface{}{},
				"equipments": bson.M{
					"_id":                    equipmentID,
					"machine_name":           equipmentName,
					"equipment_process_type": "Batch",
					"max_capacity":           10000,
					"status":                 "Active",
					"equipment_type":         "Machine",
					"unit":                   unitID,
					"minimum_duration":       12,
					"machine_supplier_name":  getRandomSupplier(),
					"warranty":               12,
					"is_production":          true,
					"process_id":             "BORM",
					"factory":                factoryID,
					"target_per_day":         1000,
					"created_by":             "SYSTEM",
					"created_on":             currentDate,
					"update_by":              "SYSTEM",
					"update_on":              currentDate,
				},
			}

			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "SH_WHOLES", warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "SHELL", false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "BORMA", warehouseID, factoryID, "production", id, outputWeight, currentDate, "WIP", "BORMA", true, pID, pOrigin, "", "")
			stockGrading[warehouseID] += outputWeight
			fmt.Printf("[DailyData] -> BORMA: Input: %.2f kg, Wholes: %.2f kg, Pieces: %.2f kg\n", input, brWholes, brPieces)
		}
		
		// E. GRAD
		if hasProc("GRAD") && stockGrading[warehouseID] > 0 {
			input := stockGrading[warehouseID]
			stockGrading[warehouseID] = 0
			pID := latestPurchaseByWh[warehouseID]
			
			// If no purchase for this warehouse, fetch from purchase collection
			if pID == "" {
				var purchase bson.M
				err := db.Collection("purchase").FindOne(ctx, bson.M{
					"org_id": req.OrgID,
					"status": "Active",
				}).Decode(&purchase)
				if err == nil && purchase != nil {
					if pid, ok := purchase["_id"].(string); ok {
						pID = pid
						latestPurchaseByWh[warehouseID] = pID
						if origin, ok := purchase["country_origin"].(string); ok {
							purchaseOriginMap[pID] = origin
						}
					}
				}
			}
			
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}
			output := input
			
			// Generate complete production document with all fields
			prodDoc := GenerateCompleteProductionDoc(
				"GRAD",
				factoryID,
				warehouseID,
				pID,
				pOrigin,
				req.OrgID,
				templatesByProcess["GRAD"],
				input,
				output,
				currentDate,
				"GRADING MACHINE",
			)
			id := prodDoc["_id"].(string)
			
			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "BORMA", warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "BORMA", false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "GRADING", warehouseID, factoryID, "production", id, output, currentDate, "WIP", "GRAD", true, pID, pOrigin, "", "")
			stockPeeled[warehouseID] += output
			fmt.Printf("[DailyData] -> GRAD: %.2f kg\n", output)
		}
		
		// F. COOL
		if hasProc("COOL") && stockPeeled[warehouseID] > 0 {
			input := stockPeeled[warehouseID]
			pID := latestPurchaseByWh[warehouseID]
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}
			id := uuid.New().String()

			// Realistic ratios from real doc (CL_WHOLES=400, BR_WHOLES=398, input=548)
			clWholes := input * (400.0 / 548.0)
			brWholes := input * (398.0 / 548.0)
			outputWeight := clWholes
			diff := input - outputWeight
			diffPct := 0.0
			if input > 0 {
				diffPct = diff / input * 100
			}
			equipmentID, _ := getEquipmentFromDB(db, "COOL", factoryID)
			unitID := getRandomUnitID()
			processEndTime := currentDate.Add(1 * time.Hour)

			prodDoc := bson.M{
				"_id":                     id,
				"factory_id":              factoryID,
				"warehouse_id":            warehouseID,
				"process_type":            "COOL",
				"process_id":              4,
				"input_weight":            input,
				"output_weight":           outputWeight,
				"CL_WHOLES":               clWholes,
				"BR_WHOLES":               brWholes,
				"cooling_product":         "NW WHOLES",
				"trolley_weight":          150.0,
				"difference":              diff,
				"diff_in_percentage":      diffPct,
				"purchase_id":             pID,
				"prevous_batch_id":        id,
				"status":                  "Completed",
				"origin":                  pOrigin,
				"created_by":              "SYSTEM",
				"created_on":              currentDate,
				"update_by":               "SYSTEM",
				"update_on":               currentDate,
				"org_id":                  req.OrgID,
				"template_id":             "Cooling-NW-wholes-fields",
				"unit_id":                 unitID,
				"equipment_id":            equipmentID,
				"equipmentName":           "COOLING ROOM",
				"remarks":                 nil,
				"process_start_date_time": currentDate,
				"process_end_date_time":   processEndTime,
				"image_upload":            []interface{}{},
				"equipments": bson.M{
					"_id":                    equipmentID,
					"machine_name":           "COOLING ROOM",
					"equipment_process_type": "continuous",
					"max_capacity":           10000,
					"status":                 "Active",
					"equipment_type":         "CR",
					"unit":                   unitID,
					"minimum_duration":       10,
					"machine_supplier_name":  getRandomSupplier(),
					"warranty":               5,
					"is_production":          true,
					"process_id":             "COOL",
					"factory":                factoryID,
					"target_per_day":         800,
					"capacity_per_hour":      100,
					"created_by":             "SYSTEM",
					"created_on":             currentDate,
					"update_by":              "SYSTEM",
					"update_on":              currentDate,
				},
			}

			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "GRADING", warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "GRAD", false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "GRADING", warehouseID, factoryID, "production", id, clWholes, currentDate, "WIP", "COOL", true, pID, pOrigin, "", "")
			stockPeeled[warehouseID] = clWholes
			fmt.Printf("[DailyData] -> COOL: Input: %.2f kg, CL_WHOLES: %.2f kg\n", input, clWholes)
		}

		// G. PEEL
		if hasProc("PEEL") && stockPeeled[warehouseID] > 0 {
			input := stockPeeled[warehouseID]
			stockPeeled[warehouseID] = 0
			pID := latestPurchaseByWh[warehouseID]
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}
			id := uuid.New().String()

			// Output split ratios from real doc (input=440)
			plWholes := input * (40.0 / 440.0)
			plLwp := input * (25.0 / 440.0)
			plSwp := input * (200.0 / 440.0)
			plSplits := input * (25.0 / 440.0)
			plBb := input * (100.0 / 440.0)
			plSsp := input * (25.0 / 440.0)
			plAllPiece := input * (375.0 / 440.0)
			husk := input * (25.0 / 440.0)
			workerID := ""
			var peelEmployees []bson.M
			pEmpCursor, _ := db.Collection("employee").Find(ctx, bson.M{"status": "Active"})
			if pEmpCursor != nil {
				pEmpCursor.All(ctx, &peelEmployees)
				if len(peelEmployees) > 0 {
					workerID = fmt.Sprintf("%v", peelEmployees[rand.Intn(len(peelEmployees))]["_id"])
				}
			}
			equipmentID, _ := getEquipmentFromDB(db, "PEEL", factoryID)
			unitID := getRandomUnitID()

			prodDoc := bson.M{
				"_id":                     id,
				"factory_id":              factoryID,
				"warehouse_id":            warehouseID,
				"process_type":            "PEEL",
				"process_id":              5,
				"input_weight":            input,
				"output_weight":           input,
				"CL_WHOLES":               input,
				"PL_WHOLES":               plWholes,
				"PL_LWP":                  plLwp,
				"PL_SWP":                  plSwp,
				"PL_SPLITS":               plSplits,
				"PL_BB":                   plBb,
				"PL_SSP":                  plSsp,
				"PL_ALL_PIECE":            plAllPiece,
				"HUSK":                    husk,
				"price_per_kg":            5,
				"worker_id":               workerID,
				"purchase_id":             pID,
				"status":                  "Start",
				"origin":                  pOrigin,
				"created_by":              "SYSTEM",
				"created_on":              currentDate,
				"org_id":                  req.OrgID,
				"template_id":             "MCWP",
				"template_name":           "MACHINE WHOLES PEELING",
				"unit_id":                 unitID,
				"equipment_id":            equipmentID,
				"equipmentName":           "JP PEELING MACHINE",
				"process_start_date_time": currentDate,
				"equipments": bson.M{
					"_id":                    equipmentID,
					"machine_name":           "JP PEELING MACHINE",
					"equipment_process_type": "continuous",
					"max_capacity":           160,
					"status":                 "Active",
					"equipment_type":         "Machine",
					"unit":                   unitID,
					"minimum_duration":       20,
					"machine_supplier_name":  getRandomSupplier(),
					"warranty":               10,
					"is_production":          true,
					"process_id":             "PEEL",
					"factory":                factoryID,
					"target_per_day":         1000,
					"created_by":             "SYSTEM",
					"created_on":             currentDate,
					"update_by":              "SYSTEM",
					"update_on":              currentDate,
				},
			}

			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "GRADING", warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "COOL", false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "PEELED", warehouseID, factoryID, "production", id, plWholes, currentDate, "WIP", "PEEL", true, pID, pOrigin, "", "")
			stockPeeled[warehouseID] += plWholes
			fmt.Printf("[DailyData] -> PEEL: Input: %.2f kg, Wholes: %.2f kg, Pieces: %.2f kg\n", input, plWholes, plAllPiece)
		}

		// H. PACK (Only pack when we have significant accumulated stock)
		minPackingStock := dailyBatchSize * 0.5
		if hasProc("PACK") && stockPeeled[warehouseID] > minPackingStock {
			input := stockPeeled[warehouseID]
			stockPeeled[warehouseID] = 0
			pID := latestPurchaseByWh[warehouseID]
			pOrigin := purchaseOriginMap[pID]
			if pOrigin == "" {
				pOrigin = defaultOriginID
			}
			id := uuid.New().String()

			productID := getRandomPackedProduct()
			packingType := getRandomPackingType()
			tinWeight := getPackingTypeWeight(packingType)
			filledTins := int(input / tinWeight)
			if filledTins == 0 {
				filledTins = 1
			}
			startSerial := rand.Intn(1000) + 1
			workerID := fmt.Sprintf("Pack%03d", rand.Intn(100)+1)
			unitID := getRandomUnitID()

			prodDoc := bson.M{
				"_id":                     id,
				"factory_id":              factoryID,
				"warehouse_id":            warehouseID,
				"process_type":            "PACK",
				"process_id":              6,
				"input_weight":            input,
				"product_id":              productID,
				"type_of_packing":         packingType,
				"filled_tins":             filledTins,
				"start_serial_no":         startSerial,
				"end_serial_no":           startSerial + filledTins,
				"packed_by":               workerID,
				"worker_id":               workerID,
				"unit_id":                 unitID,
				"purchase_id":             pID,
				"status":                  "Active",
				"origin":                  pOrigin,
				"created_by":              "SYSTEM",
				"created_on":              currentDate,
				"org_id":                  req.OrgID,
				"template_id":             templatesByProcess["PACK"],
				"template_name":           "PIECES",
				"nlg":                     1 + rand.Intn(5),
				"colour":                  getRandomColour(),
				"moisture":                3 + rand.Intn(3),
				"nut_count":               10 + rand.Intn(30),
				"uniformity":              5 + rand.Intn(10),
				"testa":                   1 + rand.Intn(5),
				"insect_infested":         getRandomYesNo(),
				"process_start_date_time": currentDate,
			}

			db.Collection("productions").InsertOne(ctx, prodDoc)
			updateStock(db, req.OrgID, "PEELED", warehouseID, factoryID, "production", id, -input, currentDate, "WIP", "PEEL", false, pID, pOrigin, "", "")
			updateStock(db, req.OrgID, "PEELED", warehouseID, factoryID, "production", id, input, currentDate, "FG", warehouseID, true, pID, pOrigin, "", "")
			stockPacked[warehouseID] += input
			fmt.Printf("[DailyData] -> PACK: Input: %.2f kg, Tins: %d\n", input, filledTins)
		} else if hasProc("PACK") {
			fmt.Printf("[DailyData] -> PACK SKIPPED: Stock %.2f kg < Minimum %.2f kg (Accumulating for batch)\n", stockPeeled[warehouseID], minPackingStock)
		}
		
		// G. SALE (Only sell when we have enough packed goods)
		saleQty := 50.0
		minSaleStock := 100.0 // Need at least 100 kg to trigger sale
		saleID := fmt.Sprintf("DS-FY%s-%d", currentDate.Format("06"), time.Now().Unix()%1000)
		
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
				"description":        "Daily Trial Sale",
				"transport_distance": 100,
				"vehicle_number":     "TN01-SYS-9999",
			}
		}
		
		pID := latestPurchaseByWh[warehouseID]
		pOrigin := purchaseOriginMap[pID]
		if pOrigin == "" {
			pOrigin = defaultOriginID
		}
		
		// Only sell if we have enough stock (realistic scenario)
		if stockPacked[warehouseID] > minSaleStock {
			stockPacked[warehouseID] -= saleQty
			db.Collection("sale").InsertOne(ctx, buildSaleDoc("FG_WHOLES", saleQty))
			updateStock(db, req.OrgID, "FG_WHOLES", warehouseID, "", "sale", saleID, -saleQty, currentDate, "WIP", "PACK", false, pID, pOrigin, "", "")
			fmt.Printf("[DailyData] -> SALE: %.2f kg (Remaining: %.2f kg)\n", saleQty, stockPacked[warehouseID])
		} else if stockPeeled[warehouseID] > minSaleStock {
			stockPeeled[warehouseID] -= saleQty
			db.Collection("sale").InsertOne(ctx, buildSaleDoc("PE_WHOLES", saleQty))
			updateStock(db, req.OrgID, "PE_WHOLES", warehouseID, "", "sale", saleID, -saleQty, currentDate, "WIP", "GRAD", false, pID, pOrigin, "", "")
			fmt.Printf("[DailyData] -> SALE: %.2f kg from PEELED (Remaining: %.2f kg)\n", saleQty, stockPeeled[warehouseID])
		} else {
			fmt.Printf("[DailyData] -> SALE SKIPPED: Insufficient stock (Packed: %.2f kg, Peeled: %.2f kg, Min: %.2f kg)\n", stockPacked[warehouseID], stockPeeled[warehouseID], minSaleStock)
		}
	}
	
	fmt.Printf("[DailyData] Completed generation for %s\n", req.OrgID)
	return nil
}

// getStockFromDB - Fetches current stock levels from database
func getStockFromDB(db *mongo.Database, productID string, orgID string) map[string]float64 {
	ctx := context.Background()
	stock := make(map[string]float64)
	
	cursor, err := db.Collection("stock_in_hand").Find(ctx, bson.M{
		"product_id": productID,
		"status":     "Active",
		"org_id":     orgID,
	})
	
	if err != nil {
		return stock
	}
	
	var stocks []bson.M
	cursor.All(ctx, &stocks)
	
	for _, s := range stocks {
		whID := fmt.Sprintf("%v", s["warehouse_id"])
		qty := helper.ToFloat64(s["available_qty"])
		if qty == 0 {
			qty = helper.ToFloat64(s["quantity"])
		}
		stock[whID] += qty
	}
	
	return stock
}
