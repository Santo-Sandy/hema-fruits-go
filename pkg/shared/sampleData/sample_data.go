package sampleData

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

// ------------------- FACTORY -------------------

func buildFactoryDoc(i int, data map[string]interface{}, demoOrgID string) bson.M {
	return bson.M{
		"_id":                   fmt.Sprintf("041FAC--%03d", i+1),
		"factory_name":          data["factory_name"],
		"registered_area_name":  data["factory_address"],
		"registered_country":    "India",
		"primary_contactnumber": data["factory_contact"],
		"status":                "Active",
		"org_id":                demoOrgID,
		"created_on":            time.Now(),
	}
}

// ------------------- FACTORY PROCESS -------------------

func buildFactoryProcessDocs(ctx context.Context, adminProcessCol *mongo.Collection, factoryID string, processes []string) ([]interface{}, error) {
	pipeline := mongo.Pipeline{
		{{"$match", bson.M{
			"$or": []bson.M{
				{"process_id": bson.M{"$in": processes}},
				{"parent_process_id": bson.M{"$in": processes}},
			},
		}}},
		{{"$group", bson.M{
			"_id":                      "$process_id",
			"process_name":             bson.M{"$first": "$process_name"},
			"process_id":               bson.M{"$first": "$process_id"},
			"day_target":               bson.M{"$first": "$day_target"},
			"working_duration_per_day": bson.M{"$first": "$working_duration_per_day"},
		}}},
	}

	cursor, err := adminProcessCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}

	var docs []interface{}
	for cursor.Next(ctx) {
		var doc bson.M
		cursor.Decode(&doc)
		processID := fmt.Sprintf("%v", doc["process_id"])
		doc["_id"] = fmt.Sprintf("%s-%s", factoryID, processID)
		doc["factory_id"] = factoryID
		doc["created_on"] = time.Now()
		docs = append(docs, doc)
	}
	return docs, nil
}

// ------------------- UNIT -------------------

func buildUnitDocs(ctx context.Context, processCol *mongo.Collection, factoryID string, processes []string, demoOrgID string) ([]interface{}, map[string]string, error) {
	pipeline := bson.A{
		bson.D{{"$match", bson.D{
			{"parent_process_id", bson.D{{"$exists", false}}},
			{"_id", bson.D{{"$in", processes}}},
		}}},
		bson.D{{"$project", bson.D{
			{"_id", 1},
			{"process_name", 1},
		}}},
	}

	cursor, err := processCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}

	var docs []interface{}
	processToUnit := make(map[string]string)
	unitIndex := 1

	for cursor.Next(ctx) {
		var doc bson.M
		cursor.Decode(&doc)

		processID := fmt.Sprintf("%v", doc["_id"])
		processName := fmt.Sprintf("%v", doc["process_name"])
		unitID := fmt.Sprintf("%sUNI--%03d", factoryID, unitIndex)

		docs = append(docs, bson.M{
			"_id":        unitID,
			"factory_id": factoryID,
			"unit_name":  strings.ToLower(processName) + " section",
			"status":     "Active",
			"org_id":     demoOrgID,
			"created_on": time.Now(),
		})

		processToUnit[processID] = unitID
		unitIndex++
	}
	return docs, processToUnit, nil
}

// ------------------- EMPLOYEE -------------------

func buildEmployeeDocs(factoryID string, orgID string, processEmployees map[string]interface{}, processToUnit map[string]string) []interface{} {
	var docs []interface{}

	for processID, countVal := range processEmployees {
		count := toInt(countVal)
		unitID := processToUnit[processID]
		if unitID == "" {
			continue
		}

		for j := 0; j < count; j++ {
			deductPF := randBool()
			gender := randomGender()

			pfConfig := 0
			pfUAN := ""
			if deductPF {
				pfConfig = rand.Intn(12)
				pfUAN = randomNumber(12)
			}

			docs = append(docs, bson.M{
				"_id":                      uuid.New().String(),
				"gender":                   gender,
				"employee_name":            randomTamilName(gender),
				"contact_mobile_number":    randomNumber(10),
				"aadhaar_card":             randomNumber(12),
				"pan_card":                 randomPAN(),
				"employee_street":          randomString("street"),
				"employee_area_name":       randomString("area"),
				"employee_city":            "Chennai",
				"employee_state":           "Tamil Nadu",
				"employee_country":         "INDIA",
				"employee_pincode":         randomNumber(6),
				"factory":                  factoryID,
				"unit":                     unitID,
				"designation":              getDesignation(processID),
				"joining_date":             time.Now(),
				"pay_type":                 "outputBased",
				"overtime_salary_per_hour": rand.Intn(100),
				"food_allowance":           rand.Intn(50),
				"bus_fare":                 rand.Intn(30),
				"deduct_pf_esi":            deductPF,
				"pf_config":                pfConfig,
				"pf_uan_no":                pfUAN,
				"deduct_bonus":             randBool(),
				"bonus_per_day":            rand.Intn(20),
				"status":                   "Active",
				"created_on":               time.Now(),
				"org_id":                   orgID,
			})
		}
	}
	return docs
}

// ------------------- EQUIPMENT + MAINTENANCE -------------------

func buildEquipmentAndMaintenanceDocs(factoryID string, demoOrgID string, equipmentMap map[string]interface{}, processToUnit map[string]string) ([]interface{}, []interface{}) {
	machineNames := map[string]string{
		"COOK":  "COOKER",
		"SHELL": "SHELL MACHINE",
		"BORM":  "ROASTER",
		"PEEL":  "PEELING MACHINE",
	}

	var equipmentDocs []interface{}
	var maintenanceDocs []interface{}

	for processID, countVal := range equipmentMap {
		count := toInt(countVal)
		unitID := processToUnit[processID]
		if unitID == "" {
			continue
		}

		baseName := machineNames[processID]
		if baseName == "" {
			baseName = "MACHINE"
		}

		for j := 0; j < count; j++ {
			equipmentID := uuid.New().String()
			maintenance := randomMaintenance(equipmentID, demoOrgID)

			equipmentDocs = append(equipmentDocs, map[string]interface{}{
				"_id":                   equipmentID,
				"machine_name":          fmt.Sprintf("%s %d", baseName, j+1),
				"factory":               factoryID,
				"unit":                  unitID,
				"process_id":            processID,
				"equipment_type":        "Machine",
				"status":                "Active",
				"is_production":         true,
				"org_id":                demoOrgID,
				"target_per_day":        rand.Intn(100) + 20,
				"capacity_per_hour":     rand.Intn(50) + 10,
				"machine_supplier_name": "abcautomations",
				"purchase_date":         time.Now(),
				"warranty":              rand.Intn(5) + 1,
			})

			maintenanceDocs = append(maintenanceDocs, maintenance...)
		}
	}
	return equipmentDocs, maintenanceDocs
}

// ------------------- INSERT HELPER -------------------

func insertDocs(ctx context.Context, col *mongo.Collection, docs []interface{}) error {
	if len(docs) == 0 {
		return nil
	}
	_, err := col.InsertMany(ctx, docs)
	if col.Name() == "invoice_details" {
		// your logic here
		fmt.Println("Inserted into invoice_details collection")
	}

	return err
}

// ------------------- ORCHESTRATOR -------------------

func GenerateSampleFactory(input []map[string]interface{}, warehouseData []map[string]interface{}, demoOrgID string, orgID string) ([]map[string]interface{}, error) {
	ctx := context.Background()

	db := database.GetConnection(demoOrgID)
	adminDB := database.GetConnection("604162a4ce67408c8b22870191199ad4")

	factoryCol := db.Collection("factory")
	processCol := db.Collection("process")
	unitCol := db.Collection("unit")
	employeeCol := db.Collection("employee")
	factoryProcessCol := db.Collection("factory_process")
	adminProcessCol := adminDB.Collection("factory_process")
	equipmentCol := db.Collection("equipments")
	maintenanceCol := db.Collection("maintance_details")
	bankCol := db.Collection("bank_details")

	var factoryDocs, factoryProcessDocs, unitDocs, employeeDocs, equipmentDocs, maintenanceDocs, bankDocs []interface{}

	for i, data := range input {
		factoryID := fmt.Sprintf("041FAC--%03d", i+1)

		factoryDocs = append(factoryDocs, buildFactoryDoc(i, data, demoOrgID))

		processArr, _ := data["factory_processes"].([]string)
		var processes []string
		for _, p := range processArr {
			processes = append(processes, strings.ToUpper(p))
		}
		bankDocs = append(bankDocs, GenerateSampleBanks(1, demoOrgID)...)

		fpDocs, err := buildFactoryProcessDocs(ctx, adminProcessCol, factoryID, processes)
		if err != nil {
			return nil, err
		}
		factoryProcessDocs = append(factoryProcessDocs, fpDocs...)

		uDocs, processToUnit, err := buildUnitDocs(ctx, processCol, factoryID, processes, demoOrgID)
		if err != nil {
			return nil, err
		}
		unitDocs = append(unitDocs, uDocs...)

		processEmployees, _ := data["no_of_Employee"].(map[string]interface{})
		employeeDocs = append(employeeDocs, buildEmployeeDocs(factoryID, orgID, processEmployees, processToUnit)...)

		equipmentMap, _ := data["equipment_count"].(map[string]interface{})
		// Ensure at least 1 equipment per process if not specified
		if equipmentMap == nil || len(equipmentMap) == 0 {
			equipmentMap = make(map[string]interface{})
			for _, proc := range processes {
				equipmentMap[proc] = 1 // Default 1 equipment per process
			}
		}
		eqDocs, mDocs := buildEquipmentAndMaintenanceDocs(factoryID, demoOrgID, equipmentMap, processToUnit)
		equipmentDocs = append(equipmentDocs, eqDocs...)
		maintenanceDocs = append(maintenanceDocs, mDocs...)
	}

	if err := insertDocs(ctx, factoryCol, factoryDocs); err != nil {
		return nil, err
	}
	GenerateSampleCustomers(5, demoOrgID)

	if len(factoryProcessDocs) > 0 {
		fmt.Println("factoryProcessDocs length:", len(factoryProcessDocs))
		b, _ := json.MarshalIndent(factoryProcessDocs, "", "  ")
		fmt.Println(string(b))
		if err := insertDocs(ctx, factoryProcessCol, factoryProcessDocs); err != nil {
			return nil, err
		}
	}
	if err := GenerateSampleWarehouses(demoOrgID, warehouseData); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, bankCol, bankDocs); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, unitCol, unitDocs); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, employeeCol, employeeDocs); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, equipmentCol, equipmentDocs); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, maintenanceCol, maintenanceDocs); err != nil {
		return nil, err
	}
	for _, m := range maintenanceDocs {
		mMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}

		parentId, _ := mMap["_id"].(string)

		err := helper.GenerateMultipleMaintenanceData(mMap, demoOrgID, parentId, nil, true)
		if err != nil {
			return nil, err
		}
	}
	// GenerateSampleDomesticPurchase(5, demoOrgID)
	return input, nil
}
func GenerateSampleBanks(count int, orgID string) []interface{} {

	var bankDocs []interface{}

	bankNames := []string{
		"HDFC Bank",
		"State Bank of India",
		"ICICI Bank",
		"Axis Bank",
		"Kotak Mahindra Bank",
	}

	branches := []string{
		"T Nagar",
		"Anna Nagar",
		"Velachery",
		"Adyar",
		"Nungambakkam",
	}

	accountTypes := []string{"savings", "current"}

	for i := 0; i < count; i++ {

		bankName := bankNames[rand.Intn(len(bankNames))]
		branch := branches[rand.Intn(len(branches))]
		accountType := accountTypes[rand.Intn(len(accountTypes))]

		doc := bson.M{
			"_id":             uuid.New().String(),
			"swift_code":      randomSWIFT(), // you can create helper
			"org_id":          orgID,
			"account_type":    accountType,
			"status":          "Active",
			"bank_name":       bankName,
			"account_number":  randomNumber(12),
			"branch_name":     branch,
			"ifsc_routing_no": randomIFSC(),
			"created_on":      time.Now(),
		}

		bankDocs = append(bankDocs, doc)
	}

	return bankDocs
}

// ------------------- DOMESTIC PURCHASE -------------------

type purchaseRef struct {
	OriginID   string
	CustomerID string
	BillToID   string
	GSTNumber  string
}

// fetchPurchaseRefs fetches a random origin, domestic supplier customer, and their billing address from DB
func fetchPurchaseRefs(ctx context.Context, db *mongo.Database) (purchaseRef, error) {
	var ref purchaseRef

	// fetch one origin
	originCursor, err := db.Collection("origin").Find(ctx, bson.M{})
	if err != nil {
		return ref, err
	}
	defer originCursor.Close(ctx)
	var origins []bson.M
	if err := originCursor.All(ctx, &origins); err != nil || len(origins) == 0 {
		return ref, fmt.Errorf("no origins found")
	}
	origin := origins[rand.Intn(len(origins))]
	ref.OriginID = fmt.Sprintf("%v", origin["_id"])

	// fetch one domestic supplier customer
	customerCursor, err := db.Collection("customer").Find(ctx, bson.M{
		"type_of_customer": "Domestic",
	})
	if err != nil {
		return ref, err
	}
	defer customerCursor.Close(ctx)
	var customers []bson.M
	if err := customerCursor.All(ctx, &customers); err != nil || len(customers) == 0 {
		return ref, fmt.Errorf("no domestic customers found")
	}
	customer := customers[rand.Intn(len(customers))]
	ref.CustomerID = fmt.Sprintf("%v", customer["_id"])

	// fetch billing address for that customer
	billingCursor, err := db.Collection("customer_billing_address").Find(ctx, bson.M{
		"customer_id": ref.CustomerID,
	})
	if err != nil {
		return ref, err
	}
	defer billingCursor.Close(ctx)
	var billings []bson.M
	if err := billingCursor.All(ctx, &billings); err != nil || len(billings) == 0 {
		return ref, fmt.Errorf("no billing address found for customer %s", ref.CustomerID)
	}
	billing := billings[rand.Intn(len(billings))]
	ref.BillToID = fmt.Sprintf("%v", billing["_id"])

	// extract gst_number from customer registered_location
	if loc, ok := customer["registered_location"].(bson.M); ok {
		ref.GSTNumber = fmt.Sprintf("%v", loc["gst_number"])
	}

	return ref, nil
}

// buildInvoiceDoc builds a single invoice_details doc linked to a purchase
func buildInvoiceDoc(purchaseID string, purchaseWeight float64, orgID string, warehouseID string) map[string]interface{} {
	qty := purchaseWeight
	price := float64(rand.Intn(100) + 150)
	invoiceAmount := qty * price
	gstRate := 0.05
	totalGST := invoiceAmount * gstRate
	sgst := totalGST / 2
	cgst := totalGST / 2

	return map[string]interface{}{
		"_id":                    uuid.New().String(),
		"purchase_id":            purchaseID,
		"invoice_number":         fmt.Sprintf("INV-%s", randomNumber(6)),
		"e_way_bill_number":      fmt.Sprintf("EWB-%s", randomNumber(8)),
		"invoice_date":           time.Now(),
		"quantity":               qty,
		"no_of_bags":             int(qty / 80),
		"invoice_amount":         invoiceAmount,
		"sgst":                   sgst,
		"cgst":                   cgst,
		"total_gst":              totalGST,
		"invoice_total":          invoiceAmount + totalGST,
		"transportation_charges": float64(rand.Intn(50000) + 10000),
		"cno_to":                 "Warehouse",
		"warehouse_id":           warehouseID,
		"org_id":                 orgID,
		"created_on":             time.Now(),
	}

}

// buildPaymentDoc builds a single domesticpayment doc linked to a purchase
func buildPaymentDoc(purchaseID string, orgID string, invoiceTotal float64, bankID string) map[string]interface{} {
	return map[string]interface{}{
		"_id":           uuid.New().String(),
		"purchase_id":   purchaseID,
		"date":          time.Now(),
		"amount":        invoiceTotal,
		"percentage":    100,
		"amountdebited": bankID,
		"org_id":        orgID,
		"created_on":    time.Now(),
	}
}

// buildPurchaseDoc builds the main purchase doc
func buildPurchaseDoc(index int, ref purchaseRef, orgID string) map[string]interface{} {
	purchaseID := fmt.Sprintf("DP-%s-%02d-%03d", strings.ToUpper(ref.OriginID), time.Now().Year()%100, index+1)
	purchaseWeight := float64(rand.Intn(1000) + 1000)

	return map[string]interface{}{
		"_id":                purchaseID,
		"purchasetype":       "domestic",
		"dop":                time.Now(),
		"country_origin":     ref.OriginID,
		"reference":          randomReference(),
		"customer_id":        ref.CustomerID,
		"bill_to":            ref.BillToID,
		"gst_number":         ref.GSTNumber,
		"purchase_weight":    purchaseWeight,
		"purchase_price":     float64(rand.Intn(100) + 150),
		"purchase_out_turn":  47,
		"purchase_nut_count": rand.Intn(110) + 140,
		"quality_reports": map[string]interface{}{
			"agency":      "RBS",
			"report_date": time.Now(),
			"out_turn":    47,
			"nut_count":   rand.Intn(110) + 140,
			"moisture":    float64(rand.Intn(5) + 1),
			"net_weight":  purchaseWeight,
		},
		"loading_date":      time.Now(),
		"loading_port":      randomLoadingPort(),
		"loading_charges":   float64(rand.Intn(20000) + 5000),
		"unloading_charges": float64(rand.Intn(20000) + 5000),
		"status":            "Active",
		"status_type":       "Cargo Arrived",
		"is_available_for_production_and_warehouse_transfer": false,
		"org_id":     orgID,
		"created_on": time.Now(),
	}
}

// GenerateSampleDomesticPurchase generates `count` domestic purchase records
// with real origin, customer, billing address IDs fetched from DB
func GenerateSampleDomesticPurchase(count int, orgID string) ([]interface{}, error) {
	ctx := context.Background()
	db := database.GetConnection(orgID)

	purchaseCol := db.Collection("purchase")
	invoiceCol := db.Collection("invoice_details")
	paymentCol := db.Collection("domesticpayment")

	// fetch one warehouse to use across all purchases
	var warehouse bson.M
	if err := db.Collection("company").FindOne(ctx, bson.M{}).Decode(&warehouse); err != nil {
		return nil, fmt.Errorf("no warehouse found: %w", err)
	}
	warehouseID := fmt.Sprintf("%v", warehouse["_id"])

	// fetch one bank to use across all payments
	var bank bson.M
	if err := db.Collection("bank_details").FindOne(ctx, bson.M{}).Decode(&bank); err != nil {
		return nil, fmt.Errorf("no bank found: %w", err)
	}
	bankID := fmt.Sprintf("%v", bank["_id"])

	var purchaseDocs, invoiceDocs, paymentDocs []interface{}

	for i := 0; i < count; i++ {
		ref, err := fetchPurchaseRefs(ctx, db)
		if err != nil {
			return nil, err
		}

		purchase := buildPurchaseDoc(i, ref, orgID)
		purchaseID := purchase["_id"].(string)
		purchaseweight := purchase["purchase_weight"].(float64)

		invoice := buildInvoiceDoc(purchaseID, purchaseweight, orgID, warehouseID) // purchase_id set below
		invoiceTotal, _ := invoice["invoice_total"].(float64)
		// link invoice and payment to purchase
		invoice["purchase_id"] = purchaseID
		payment := buildPaymentDoc(purchaseID, orgID, invoiceTotal, bankID)

		purchaseDocs = append(purchaseDocs, purchase)
		invoiceDocs = append(invoiceDocs, invoice)
		paymentDocs = append(paymentDocs, payment)
	}
	invoiceMap := make(map[string][]map[string]interface{})

	for _, item := range invoiceDocs {
		invoice, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		purchaseID := fmt.Sprintf("%v", invoice["purchase_id"])
		invoiceMap[purchaseID] = append(invoiceMap[purchaseID], invoice)
	}
	for _, item := range purchaseDocs {

		purchase, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		purchaseID := fmt.Sprintf("%v", purchase["_id"])

		if invoices, exists := invoiceMap[purchaseID]; exists {
			purchase["invoice_details"] = invoices
		} else {
			purchase["invoice_details"] = []interface{}{}
		}
	}
	if err := insertDocs(ctx, purchaseCol, purchaseDocs); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, invoiceCol, invoiceDocs); err != nil {
		return nil, err
	}
	if err := insertDocs(ctx, paymentCol, paymentDocs); err != nil {
		return nil, err
	}
	return invoiceDocs, nil
}

// ------------------- WAREHOUSE -------------------

func GenerateSampleWarehouses(orgID string, warehouseDetails []map[string]interface{}) error {
	ctx := context.Background()
	db := database.GetConnection(orgID)

	// fetch all factory IDs from DB
	cursor, err := db.Collection("factory").Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to fetch factories: %w", err)
	}
	defer cursor.Close(ctx)

	var factories []bson.M
	if err := cursor.All(ctx, &factories); err != nil || len(factories) == 0 {
		return fmt.Errorf("no factories found")
	}

	var docs []interface{}

	for i := 0; i < len(warehouseDetails); i++ {
		factory := factories[rand.Intn(len(factories))]
		factoryID := fmt.Sprintf("%v", factory["_id"])

		// Safe extraction with correct keys from transformWarehouse
		location, _ := warehouseDetails[i]["address"].(string)
		wType, _ := warehouseDetails[i]["warehouse_type"].(string)
		warehouseName, _ := warehouseDetails[i]["name"].(string)

		// Stable _id preservation
		wID, _ := warehouseDetails[i]["_id"].(string)
		if wID == "" {
			wID = uuid.New().String()
		}

		docs = append(docs, bson.M{
			"_id":                 wID,
			"name":                warehouseName,
			"warehouse_type":      wType,
			"inside_factory":      wType == "Factory Warehouse",
			"factory_id":          factoryID,
			"contact_person_name": "Warehouse Manager",
			"mobile_number":       randomNumber(10),
			"address":             location,
			"gst_number":          randomGST(),
			"status":              "Active",
			"org_id":              orgID,
			"created_on":          time.Now(),
		})
	}

	return insertDocs(ctx, db.Collection("company"), docs)
}

func randomReference() string {
	options := []string{"Reference", "Via Broker", "Not Applicable"}
	return options[rand.Intn(len(options))]
}

func randomLoadingPort() string {
	ports := []string{"Mangalore", "Tuticorin", "Panruti"}
	return ports[rand.Intn(len(ports))]
}

// ------------------- CUSTOMER -------------------

func GenerateSampleCustomers(count int, orgID string) error {

	ctx := context.Background()
	db := database.GetConnection(orgID)

	customerCol := db.Collection("customer")
	billingCol := db.Collection("customer_billing_address")

	var customerDocs []interface{}
	var billingDocs []interface{}

	for i := 0; i < count; i++ {

		customerID := fmt.Sprintf("CUST-%03d", i+1)
		customerType := randomCustomerType()
		isInternational := customerType == "International"

		customer := bson.M{
			"_id":                customerID,
			"customer_name":      randomCompanyName(),
			"customer_type":      randomCategoryMap(),
			"is_login_available": randBool(),
			"products_dealing":   randomProduct(),
			"type_of_customer":   customerType,
			"pan_card":           randomPAN(),
			"status":             "Active",
			"org_id":             orgID,
			"created_on":         time.Now(),
		}

		// ---------------- GST ----------------
		if !isInternational {
			customer["registered_location"] = bson.M{
				"gst_number": randomGST(),
			}
		}

		// ---------------- REGISTERED ADDRESS ----------------
		customer["registered_street"] = randomString("street")
		customer["registered_area_name"] = randomString("area")
		customer["registered_city"] = "Chennai"
		customer["registered_state"] = "Tamil Nadu"
		customer["registered_country"] = "India"
		customer["registered_pincode"] = randomNumber(6)
		customer["reg_type"] = randomRegType()

		// ---------------- BILLING ----------------
		useSame := randBool()

		var billing bson.M

		if useSame {
			billing = bson.M{
				"_id":         uuid.New().String(),
				"customer_id": customerID,
				"street":      customer["registered_street"],
				"area_name":   customer["registered_area_name"],
				"city":        customer["registered_city"],
				"state":       customer["registered_state"],
				"country":     customer["registered_country"],
				"pincode":     customer["registered_pincode"],
				"created_on":  time.Now(),
				"org_id":      orgID,
			}
		} else {
			billing = bson.M{
				"_id":         uuid.New().String(),
				"customer_id": customerID,
				"street":      randomString("billing_street"),
				"area_name":   randomString("billing_area"),
				"city":        "Madurai",
				"state":       "Tamil Nadu",
				"country":     "India",
				"pincode":     randomNumber(6),
				"created_on":  time.Now(),
				"org_id":      orgID,
			}
		}

		// add billing to list
		billingDocs = append(billingDocs, billing)

		// embed (optional)
		customer["billing_address"] = []interface{}{billing}
		customer["same_as"] = useSame

		// ---------------- CONTACT ----------------
		customer["primary_contact_name"] = randomTamilName("Male")
		customer["primary_contact_number"] = randomNumber(10)
		customer["primary_contact_email"] = randomEmail()

		customer["secondary_contact_name"] = randomTamilName("Female")
		customer["secondary_contact_number"] = randomNumber(10)
		customer["secondary_contact_email"] = randomEmail()

		// ---------------- BANK ----------------
		customer["bank_details"] = []interface{}{
			bson.M{
				"_id":                 uuid.New().String(),
				"bank_name":           "SBI",
				"account_holder_name": randomTamilName("Male"),
				"account_number":      randomNumber(12),
				"ifsc_routing_no":     "SBIN0001234",
				"branch_name":         "Chennai",
				"country":             "India",
			},
		}

		customerDocs = append(customerDocs, customer)
	}

	// ---------------- INSERT ----------------
	if err := insertDocs(ctx, customerCol, customerDocs); err != nil {
		return fmt.Errorf("customer insert failed: %w", err)
	}

	if err := insertDocs(ctx, billingCol, billingDocs); err != nil {
		return fmt.Errorf("billing insert failed: %w", err)
	}

	return nil
}

// ------------------- RANDOM HELPERS -------------------

func randomString(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, rand.Intn(10000))
}

func randomMaintenance(equipmentID string, orgID string) []interface{} {
	types := []string{"Pressure Test", "Drive Belt Check", "Oil Check", "Motor Inspection"}
	frequency := []string{"Daily", "Weekly", "Monthly"}
	serviceBy := []string{"Supplier", "Third Party", "In-house"}
	photo := []string{"Before Maintenance", "After Maintenance", "Both"}
	warning := []string{"To Mobile App", "To Both", "None"}

	count := rand.Intn(2) + 1
	var list []interface{}

	for i := 0; i < count; i++ {
		list = append(list, map[string]interface{}{
			"_id":              uuid.New().String(),
			"name":             types[rand.Intn(len(types))],
			"frequency":        frequency[rand.Intn(len(frequency))],
			"duration":         fmt.Sprintf("%d", rand.Intn(30)+10),
			"start_time":       fmt.Sprintf("%02d:00 AM", rand.Intn(12)+1),
			"photo":            photo[rand.Intn(len(photo))],
			"service_by":       serviceBy[rand.Intn(len(serviceBy))],
			"warning":          warning[rand.Intn(len(warning))],
			"part_replacement": "None",
			"cost_included":    fmt.Sprintf("%d", rand.Intn(2000)+500),
			"remarks":          "Auto generated",
			"org_id":           orgID,
			"equipment_id":     equipmentID,
		})
	}
	return list
}

func randomNumber(length int) string {
	num := ""
	for i := 0; i < length; i++ {
		num += fmt.Sprintf("%d", rand.Intn(10))
	}
	return num
}

func randomGender() string {
	genders := []string{"Male", "Female", "Others"}
	return genders[rand.Intn(len(genders))]
}

func randomPAN() string {
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	pan := ""
	for i := 0; i < 5; i++ {
		pan += string(letters[rand.Intn(len(letters))])
	}
	for i := 0; i < 4; i++ {
		pan += fmt.Sprintf("%d", rand.Intn(10))
	}
	pan += string(letters[rand.Intn(len(letters))])
	return pan
}

var maleNames = []string{
	"Arun", "Karthik", "Vijay", "Suresh", "Murugan", "Prakash", "Ramesh", "Dinesh", "Ganesh", "Mahesh",
	"Rajesh", "Senthil", "Saravanan", "Baskar", "Elango", "Kumar", "Manikandan", "Selvam", "Ravi", "Kannan",
	"Harish", "Lokesh", "Naveen", "Prabhu", "Sathish", "Thiru", "Vignesh", "Yuvaraj", "Aravind", "Balaji",
	"Chandran", "Deva", "Ezhil", "Gopi", "Hari", "Jagan", "Karthi", "Madhan", "Naren", "Praveen",
	"Raghu", "Sasi", "Tamil", "Uday", "Varun", "Yogesh", "Ashok", "Bala", "Deepak", "Eswar",
}
var femaleNames = []string{
	"Priya", "Divya", "Lakshmi", "Meena", "Kavya", "Anitha", "Revathi", "Deepa", "Nithya", "Janani",
	"Swathi", "Gayathri", "Keerthana", "Aarthi", "Bhavani", "Chitra", "Devi", "Fathima", "Geetha", "Hema",
	"Indhu", "Jaya", "Kalpana", "Latha", "Mahalakshmi", "Nandhini", "Pavithra", "Raji", "Sangeetha", "Thilaga",
	"Uma", "Valli", "Yamini", "Anu", "Banu", "Charu", "Dharani", "Easwari", "Gowri", "Harini",
	"Ishwarya", "Jothi", "Kirthika", "Lavanya", "Monika", "Nivetha", "Oviya", "Preethi", "Ramya", "Shalini",
}
var initials = []string{"M", "S", "R", "K", "P", "V", "T", "N", "A", "D"}

func randomTamilName(gender string) string {
	var first string
	if gender == "Male" {
		first = maleNames[rand.Intn(len(maleNames))]
	} else {
		first = femaleNames[rand.Intn(len(femaleNames))]
	}
	return first + " " + initials[rand.Intn(len(initials))]
}

func toInt(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return 0
}

func getDesignation(process string) string {
	switch strings.ToUpper(process) {
	case "COOK":
		return "Loadman"
	case "SHELL":
		return "sheller"
	case "BORM":
		return "Loadman"
	case "GRAD":
		return "Grader"
	case "COOL":
		return "Loadman"
	default:
		return "Yard Worker"
	}
}

func randBool() bool {
	return rand.Intn(2) == 0
}

func randomCompanyName() string {
	names := []string{
		"ABC Traders", "Sri Lakshmi Exports", "Golden Cashew Pvt Ltd",
		"South India Traders", "Ocean Foods Ltd",
	}
	return names[rand.Intn(len(names))]
}

func randomCategoryMap() map[string]bool {
	categories := []string{"Supplier", "Buyer", "serviceprovider"}

	result := make(map[string]bool)

	// randomly set true/false
	for _, cat := range categories {
		result[cat] = rand.Intn(2) == 1
	}

	return result
}
func randomProduct() string {
	arr := []string{"RCN", "Kernal", "Both"}
	return arr[rand.Intn(len(arr))]
}

func randomCustomerType() string {
	arr := []string{"Domestic"}
	return arr[rand.Intn(len(arr))]
}

func randomRegType() string {
	arr := []string{"Private", "Public", "Proprietor", "Partner"}
	return arr[rand.Intn(len(arr))]
}

func randomEmail() string {
	return fmt.Sprintf("user%d@gmail.com", rand.Intn(10000))
}

func randomGST() string {
	return fmt.Sprintf("%02dABCDE1234F1Z5", rand.Intn(99))
}
func randomSWIFT() string {
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return fmt.Sprintf("%c%c%c%cINBB%03d",
		letters[rand.Intn(len(letters))],
		letters[rand.Intn(len(letters))],
		letters[rand.Intn(len(letters))],
		letters[rand.Intn(len(letters))],
		rand.Intn(999),
	)
}
func randomIFSC() string {
	return fmt.Sprintf("BANK0%06d", rand.Intn(999999))
}

func SampleCookingData(factoryID string, invoiceData []interface{}, orgID string) ([]interface{}, error) {

	ctx := context.Background()
	var result []interface{}

	filter := bson.M{
		"process_id": "COOK",
		"factory":    factoryID,
	}

	cursor, err := database.GetConnection(orgID).Collection("equipments").Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var equipments []bson.M
	for cursor.Next(ctx) {
		var eq bson.M
		cursor.Decode(&eq)
		equipments = append(equipments, eq)
	}

	if len(equipments) == 0 {
		return nil, fmt.Errorf("no cooking equipment found")
	}

	// ---------------- COOKING DATA ----------------
	for _, data := range invoiceData {

		doc, ok := data.(map[string]interface{})
		if !ok {
			continue
		}

		totalQty := toInt(doc["quantity"])
		remaining := totalQty
		count := 1

		baseTime := time.Now()
		for remaining > 0 {

			chunk := rand.Intn(1500) + 2000

			if remaining < chunk {
				chunk = remaining
			}

			remaining -= chunk
			startTime := baseTime.AddDate(0, 0, count-1)

			eq := equipments[rand.Intn(len(equipments))]

			item := map[string]interface{}{
				"_id":                     uuid.New().String(),
				"factory_id":              eq["factory"],
				"unit":                    eq["unit"],
				"equipment_id":            eq["_id"],
				"warehouse_id":            doc["warehouse_id"],
				"process_id":              "1",
				"invoice_no":              doc["invoice_number"],
				"purchase_id":             doc["purchase_id"],
				"process_start_date_time": startTime,
				"input_weight":            chunk, "status": "Start",
				"stones_weight": 0,

				"template_id": "Cooking-Form",

				"duration":     8,
				"process_type": "COOK",
				"STEAMEDRCN":   chunk,
				"created_on":   time.Now(),
			}

			result = append(result, item)
			count++
		}
	}

	return result, nil
}
func SampleShellingData(factoryID string, invoiceData []interface{}, orgID string) ([]interface{}, error) {

	ctx := context.Background()
	var result []interface{}

	// 🔹 Fetch shelling equipments
	filter := bson.M{
		"process_id": "SHELL",
		"factory":    factoryID,
	}

	cursor, err := database.GetConnection(orgID).Collection("equipments").Find(ctx, filter)
	if err != nil {
		return nil, err
	}

	defer cursor.Close(ctx)

	var equipments []bson.M
	for cursor.Next(ctx) {
		var eq bson.M
		cursor.Decode(&eq)
		equipments = append(equipments, eq)
	}

	if len(equipments) == 0 {
		return nil, fmt.Errorf("no shelling equipment found")
	}

	for _, data := range invoiceData {

		doc, ok := data.(map[string]interface{})
		if !ok {
			continue
		}

		totalQty := toFloat(doc["available_qty"])
		remaining := totalQty
		count := 1

		baseTime := time.Now()

		for remaining > 0 {

			var inputWeight float64

			if remaining < 100 {
				inputWeight = remaining
			} else {
				inputWeight = float64(rand.Intn(200) + 100)

				if inputWeight > remaining {
					inputWeight = remaining
				}
			}

			yield := 47
			output := (inputWeight * float64(yield)) / 178.6
			// input := (output * 178.6) / 47
			sh_whoels := output * 0.8
			sh_pieces := output * 0.2
			workerID, err := GetRandomEmployeeID(factoryID, "sheller", orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to get random employee ID: %w", err)
			}
			remaining -= inputWeight

			startTime := baseTime.AddDate(0, 0, count-1)

			eq := equipments[rand.Intn(len(equipments))]

			item := map[string]interface{}{
				"_id":                     uuid.New().String(),
				"factory_id":              eq["factory"],
				"unit":                    eq["unit"],
				"process_type":            "SHELL",
				"template_name":           "MACHINE SHELLING",
				"template_id":             "12",
				"equipment_id":            eq["_id"],
				"warehouse_id":            doc["warehouse_id"],
				"process_id":              2,
				"worker_id":               workerID,
				"purchase_id":             doc["purchase_id"],
				"process_start_date_time": startTime,

				"input_weight":  inputWeight,
				"output_weight": output,
				"SH_WHOLES":     sh_whoels,
				"SH_PIECES":     sh_pieces,

				"stones_weight": 0,
				"status":        "Start",
				"created_on":    startTime,
			}

			result = append(result, item)
			count++
		}
	}

	return result, nil
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
func SampleBormaData(factoryID string, shellData []interface{}, orgID string) ([]interface{}, error) {

	ctx := context.Background()
	var result []interface{}

	filter := bson.M{
		"process_id": "BORM",
		"factory":    factoryID,
	}

	cursor, err := database.GetConnection(orgID).Collection("equipments").Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var equipments []bson.M
	for cursor.Next(ctx) {
		var eq bson.M
		cursor.Decode(&eq)
		equipments = append(equipments, eq)
	}

	if len(equipments) == 0 {
		return nil, fmt.Errorf("no borma equipment found")
	}

	for _, data := range shellData {

		doc, ok := data.(map[string]interface{})
		if !ok {
			continue
		}

		shWholes := toFloat(doc["SH_WHOLES"])
		shPieces := toFloat(doc["SH_PIECES"])

		trolleyWeight := 250.0

		netWeight := shWholes + shPieces
		if netWeight <= 0 {
			continue
		}

		inputWeight := netWeight + trolleyWeight
		lossPercent := 0.001 + rand.Float64()*0.002

		brWholes := shWholes * (1 - lossPercent)
		brPieces := shPieces * (1 - lossPercent)

		outputWeight := brWholes + brPieces + trolleyWeight

		diff := inputWeight - outputWeight
		diffPercent := (diff / inputWeight) * 100

		eq := equipments[rand.Intn(len(equipments))]
		startTime := time.Now()

		item := map[string]interface{}{
			"_id":           uuid.New().String(),
			"factory_id":    factoryID,
			"unit_id":       eq["unit"],
			"process_type":  "BORM",
			"process_id":    3,
			"template_id":   "Borma-NW-fields",
			"equipment_id":  eq["_id"],
			"warehouse_id":  doc["warehouse_id"],
			"purchase_id":   doc["purchase_id"],
			"borma_product": "NW WHOLES & NW PIECES",

			"process_start_date_time": startTime,
			"process_end_date_time":   startTime.Add(time.Minute * time.Duration(rand.Intn(20)+5)),

			"input_weight":   inputWeight,
			"trolley_weight": trolleyWeight,
			"output_weight":  outputWeight,

			"SH_WHOLES": shWholes,
			"SH_PIECES": shPieces,

			"BR_WHOLES": brWholes,
			"BR_PIECES": brPieces,

			"difference":         diff,
			"diff_in_percentage": diffPercent,

			"status":     "Completed",
			"created_by": "LV-111",
			"created_on": startTime,
		}

		result = append(result, item)
	}

	return result, nil
}
func SampleCoolingData(factoryID string, bormaData []interface{}, orgID string) ([]interface{}, error) {

	ctx := context.Background()
	var result []interface{}

	filter := bson.M{
		"process_id": "COOL",
		"factory":    factoryID,
	}

	cursor, err := database.GetConnection(orgID).Collection("equipments").Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var equipments []bson.M
	for cursor.Next(ctx) {
		var eq bson.M
		cursor.Decode(&eq)
		equipments = append(equipments, eq)
	}

	if len(equipments) == 0 {
		return nil, fmt.Errorf("no cooling equipment found")
	}

	for _, data := range bormaData {

		doc, ok := data.(map[string]interface{})
		if !ok {
			continue
		}

		brWholes := toFloat(doc["BR_WHOLES"])
		brPieces := toFloat(doc["BR_PIECES"])

		trolleyWeight := 250.0

		netWeight := brWholes + brPieces
		if netWeight <= 0 {
			continue
		}

		inputWeight := netWeight + trolleyWeight

		variation := (rand.Float64()*0.001 - 0.0005) // -0.05% to +0.05%

		clWholes := brWholes * (1 + variation)
		clPieces := brPieces * (1 + variation)

		outputWeight := clWholes + clPieces + trolleyWeight

		diff := inputWeight - outputWeight
		diffPercent := (diff / inputWeight) * 100

		eq := equipments[rand.Intn(len(equipments))]
		startTime := time.Now()

		item := map[string]interface{}{
			"_id": uuid.New().String(),

			"factory_id":   factoryID,
			"unit_id":      eq["unit"],
			"process_type": "COOL",
			"process_id":   4,
			"template_id":  "Cooling-NW-fields",
			"equipment_id": eq["_id"],

			"warehouse_id": doc["warehouse_id"],
			"purchase_id":  doc["purchase_id"],

			"cooling_product": "NW WHOLES & NW PIECES",

			"process_start_date_time": startTime,
			"process_end_date_time":   startTime.Add(time.Minute * time.Duration(rand.Intn(15)+5)),

			"input_weight":   inputWeight,
			"trolley_weight": trolleyWeight,
			"output_weight":  outputWeight,

			"BR_WHOLES": brWholes,
			"BR_PIECES": brPieces,

			"CL_WHOLES": clWholes,
			"CL_PIECES": clPieces,

			"difference":         diff,
			"diff_in_percentage": diffPercent,

			"prevous_batch_id": doc["_id"],

			"status":     "Completed",
			"created_by": "LV-111",
			"created_on": startTime,
		}

		result = append(result, item)
	}

	return result, nil
}
func SamplePeelingDataFinal(factoryID string, input []interface{}, orgID string) ([]interface{}, error) {

	var result []interface{}

	for _, data := range input {

		doc := data.(map[string]interface{})

		clWholes := toFloat(doc["CL_WHOLES"])
		clPieces := toFloat(doc["CL_PIECES"])

		startTime := time.Now()

		// =========================
		// 🔥 WHOLES PEELING (MCWP)
		// =========================
		if clWholes > 0 {

			plWholes := clWholes * 0.83

			plSWP := clWholes * 0.025
			plSSP := clWholes * 0.033
			plLWP := clWholes * 0.033
			plBB := clWholes * 0.041
			plSplits := clWholes * 0.016

			plAllPiece := plSWP + plSSP + plLWP + plBB + plSplits
			husk := clWholes - (plWholes + plAllPiece)

			item := map[string]interface{}{
				"_id": uuid.New().String(),

				"process_type": "PEEL",
				"process_id":   5,

				"template_name": "MACHINE WHOLES PEELING",
				"template_id":   "MCWP",

				"factory_id":   factoryID,
				"unit_id":      doc["unit_id"],
				"equipment_id": doc["equipment_id"],

				"warehouse_id": doc["warehouse_id"],
				"purchase_id":  doc["purchase_id"],

				"worker_id": "EMP-" + strconv.Itoa(rand.Intn(50)+1),

				"process_start_date_time": startTime,

				// INPUT
				"CL_WHOLES":    clWholes,
				"input_weight": clWholes,

				// OUTPUT
				"PL_WHOLES":    plWholes,
				"PL_SWP":       plSWP,
				"PL_SSP":       plSSP,
				"PL_LWP":       plLWP,
				"PL_BB":        plBB,
				"PL_SPLITS":    plSplits,
				"PL_ALL_PIECE": plAllPiece,
				"HUSK":         husk,

				"output_weight": clWholes,

				"status":     "Start",
				"created_by": "LV-111",
				"created_on": startTime,
			}

			result = append(result, item)
		}

		// =========================
		// 🔥 PIECES PEELING (MCPP)
		// =========================
		if clPieces > 0 {

			plSWP := clPieces * 0.42
			plSSP := clPieces * 0.08
			plLWP := clPieces * 0.08
			plBB := clPieces * 0.25
			plSplits := clPieces * 0.08

			plAllPiece := plSWP + plSSP + plLWP + plBB + plSplits
			husk := clPieces - plAllPiece

			item := map[string]interface{}{
				"_id": uuid.New().String(),

				"process_type": "PEEL",
				"process_id":   5,

				"template_name": "MACHINE PIECES PEELING",
				"template_id":   "MCPP",

				"factory_id":   factoryID,
				"unit_id":      doc["unit_id"],
				"equipment_id": doc["equipment_id"],

				"warehouse_id": doc["warehouse_id"],
				"purchase_id":  doc["purchase_id"],

				"worker_id": "PF" + strconv.Itoa(rand.Intn(10)+1),

				"process_start_date_time": startTime,

				// INPUT
				"CL_PIECES":    clPieces,
				"input_weight": clPieces,

				// OUTPUT
				"PL_SWP":       plSWP,
				"PL_SSP":       plSSP,
				"PL_LWP":       plLWP,
				"PL_BB":        plBB,
				"PL_SPLITS":    plSplits,
				"PL_ALL_PIECE": plAllPiece,
				"HUSK":         husk,

				"output_weight": clPieces,

				"status":     "Start",
				"created_by": "LV-111",
				"created_on": startTime,
			}

			result = append(result, item)
		}
	}

	return result, nil
}
func SampleGradingDataFinal(factoryID string, input []interface{}, orgID string) ([]interface{}, error) {

	var result []interface{}

	for _, data := range input {

		doc, ok := data.(map[string]interface{})
		if !ok {
			continue
		}

		plWholes := toFloat(doc["PL_WHOLES"])
		plAllPiece := toFloat(doc["PL_ALL_PIECE"])

		startTime := time.Now()

		if plWholes > 0 {

			whiteWholes := plWholes * 0.60
			swWholes := plWholes * 0.15
			pkw := plWholes * 0.10
			buds := plWholes * 0.05
			s := plWholes * 0.04
			pieces := plWholes * 0.03
			dw := plWholes * 0.015
			ow := plWholes * 0.01

			total := whiteWholes + swWholes + pkw + buds + s + pieces + dw + ow
			rejection := plWholes - total
			workerID, err := GetRandomEmployeeID(factoryID, "Grader", orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to get random employee ID: %w", err)
			}
			item := map[string]interface{}{
				"_id": uuid.New().String(),

				"process_type": "GRAD",
				"process_id":   6,

				"template_name": "WHOLES GRADING",
				"template_id":   "Manual-GWW",

				"factory_id":   factoryID,
				"unit_id":      doc["unit_id"],
				"equipment_id": doc["equipment_id"],

				"warehouse_id": doc["warehouse_id"],
				"purchase_id":  doc["purchase_id"],

				"worker_id": workerID,

				"process_start_date_time": startTime,

				// INPUT
				"PL_WHOLES":    plWholes,
				"input_weight": plWholes,

				// OUTPUT
				"W210":      whiteWholes,
				"W320":      swWholes,
				"W240":      pkw,
				"SW210":     buds,
				"SW320":     s,
				"SW240":     pieces,
				"DW":        dw,
				"OW":        ow,
				"REJECTION": rejection,

				"output_weight": total,

				"status":     "Start",
				"created_by": "LV-111",
				"created_on": startTime,
			}

			result = append(result, item)
		}

		if plAllPiece > 0 {

			jh := plAllPiece * 0.10
			s := plAllPiece * 0.15
			ss := plAllPiece * 0.10
			k := plAllPiece * 0.08
			lwp := plAllPiece * 0.07
			sp := plAllPiece * 0.10
			swp := plAllPiece * 0.08
			op := plAllPiece * 0.07
			pkp := plAllPiece * 0.08
			sps := plAllPiece * 0.05
			unpeeled := plAllPiece * 0.07
			buds := plAllPiece * 0.05

			total := jh + s + ss + k + lwp + sp + swp + op + pkp + sps + unpeeled + buds
			rejection := plAllPiece - total
			workerID, err := GetRandomEmployeeID(factoryID, "Grader", orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to get random employee ID: %w", err)
			}
			item := map[string]interface{}{
				"_id": uuid.New().String(),

				"process_type": "GRAD",
				"process_id":   6,

				"template_name": "PIECES GRADING",
				"template_id":   "Manual-GSP",

				"factory_id":   factoryID,
				"unit_id":      doc["unit_id"],
				"equipment_id": doc["equipment_id"],

				"warehouse_id": doc["warehouse_id"],
				"purchase_id":  doc["purchase_id"],

				"worker_id": workerID,

				"process_start_date_time": startTime,

				// INPUT
				"PL_ALL_PIECE": plAllPiece,
				"input_weight": plAllPiece,

				// OUTPUT
				"JH":              jh,
				"S":               s,
				"SS":              ss,
				"K":               k,
				"LWP":             lwp,
				"SP":              sp,
				"SWP":             swp,
				"OP":              op,
				"PKP":             pkp,
				"SPS":             sps,
				"UNPEELED_PIECES": unpeeled,
				"BUDS":            buds,
				"REJECTION":       rejection,

				"output_weight": plAllPiece,

				"status":     "Start",
				"created_by": "LV-111",
				"created_on": startTime,
			}

			result = append(result, item)
		}
	}

	return result, nil
}

func SamplePackingData(factoryID string, inputs []map[string]interface{}, orgID string) ([]interface{}, error) {

	var result []interface{}

	for _, input := range inputs {

		qty := toFloat(input["available_qty"])
		if qty <= 0 {
			continue
		}

		// 👉 Calculate tins (20kg per tin)
		noOfTins := int(qty / 20)
		if noOfTins == 0 {
			continue
		}

		startSerial := 1
		endSerial := noOfTins

		// 👉 Get unit_id from factory + unit name
		unitID, err := GetUnitByFactoryAndName(factoryID, "borma section", orgID)
		if err != nil {
			fmt.Println("Unit fetch error:", err)
			continue
		}

		startTime := time.Now()

		// 👉 Get worker
		workerID, err := GetRandomEmployeeID(factoryID, "Grader", orgID)
		if err != nil {
			return nil, fmt.Errorf("failed to get random employee ID: %w", err)
		}

		item := map[string]interface{}{
			"_id": uuid.New().String(),

			"process_type": "PACK",
			"process_id":   7,

			"template_id":   "Grade-pie",
			"template_name": "Packing for Graded ",

			"factory_id":   factoryID,
			"unit_id":      unitID,
			"warehouse_id": input["warehouse_id"],
			"purchase_id":  input["purchase_id"],

			"product_id": input["product_id"],
			"weight":     float64(noOfTins * 20),
			"worker_id":  workerID,

			"process_start_date_time": startTime,

			// QUALITY FIELDS
			"colour":          "good",
			"moisture":        3,
			"uniformity":      97,
			"insect_infested": "no",
			"testa":           2.2,
			"nlg":             2,

			// PACKING DETAILS ✅ FIXED
			"type_of_packing": "004",
			"start_serial_no": startSerial,
			"end_serial_no":   endSerial,
			"filled_tins":     noOfTins,

			// SALARY
			"is_fixed_salary": true,
			"fixed_salary":    0,

			// STATUS
			"status":     "packed",
			"created_by": "LV-004",
			"created_on": startTime,
		}

		// 👉 Debug (optional)
		fmt.Println("Generated Serial:", startSerial, "to", endSerial, "Tins:", noOfTins)

		result = append(result, item)
	}

	return result, nil
}
func GetRandomEmployeeID(factoryID string, role string, orgID string) (string, error) {

	ctx := context.Background()
	db := database.GetConnection(orgID)

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"factory":     factoryID,
			"designation": role,
			"status":      "Active",
		}}},
		{{Key: "$sample", Value: bson.M{
			"size": 1,
		}}},
	}

	cursor, err := db.Collection("employee").Aggregate(ctx, pipeline)
	if err != nil {
		return "", fmt.Errorf("failed to fetch employee: %w", err)
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return "", fmt.Errorf("failed to decode employee: %w", err)
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no active employee found")
	}

	empID, ok := results[0]["_id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid employee id format")
	}

	return empID, nil
}
func GetUnitByFactoryAndName(factoryID, unitName, dbName string) (string, error) {

	filter := bson.M{
		"factory_id": factoryID,
		"unit_name":  strings.ToLower(unitName),
	}

	var unit map[string]interface{}

	err := database.GetConnection(dbName).
		Collection("unit").
		FindOne(context.Background(), filter).
		Decode(&unit)

	if err != nil {
		return "", fmt.Errorf("unit not found")
	}

	return unit["_id"].(string), nil
}
func GetRandomCustomer(orgID string) (map[string]interface{}, error) {

	ctx := context.Background()
	col := database.GetConnection(orgID).Collection("customer")

	pipeline := mongo.Pipeline{
		{{"$match", bson.D{
			{"type_of_customer", "Domestic"},
		}}},
		{{"$sample", bson.D{
			{"size", 1},
		}}},
	}

	cursor, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []map[string]interface{}
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no customer found")
	}

	return result[0], nil
}
func InsertSaleDirect(orgID, userID string) (interface{}, error) {

	ctx := context.Background()
	collection := database.GetConnection(orgID).Collection("sale")
	customer, err := GetRandomCustomer(orgID)
	if err != nil {
		return nil, err
	}

	customerID := customer["_id"]

	var billTo interface{}
	var deliveryTo interface{}

	if billingArr, ok := customer["billing_address"].([]interface{}); ok && len(billingArr) > 0 {

		addr := billingArr[0].(map[string]interface{})
		billTo = addr["_id"]
		deliveryTo = addr["_id"]
	}

	inputData := map[string]interface{}{
		"_id":         uuid.New().String(),
		"dop":         time.Now(),
		"customer_id": customerID,
		"bill_to":     billTo,
		"delivery_to": deliveryTo,
		"org_id":      orgID,

		"type_of_sale": "kernel",
		"created_on":   time.Now(),
		"created_by":   userID,
	}

	res, err := collection.InsertOne(ctx, inputData)
	if err != nil {
		return nil, fmt.Errorf("sale insert failed: %v", err)
	}

	inputData["_id"] = res.InsertedID

	return inputData, nil
}
func InsertSoldProductDirect(
	stock map[string]interface{},
	saleID interface{},
	orgID, userID string,
) (map[string]interface{}, error) {

	ctx := context.Background()
	collection := database.GetConnection(orgID).Collection("sold_products_info")

	totalWeight := toFloat(stock["total_weight"])
	totalTins := toFloat(stock["total_filled_tins"])

	tinSize := totalWeight / totalTins
	qty := totalWeight
	if qty > 50 {
		qty = 50
	}
	saleMap := saleID.(map[string]interface{})
	tinCount := int(qty / tinSize)

	serialNos := []int{}
	for i := 1; i <= tinCount; i++ {
		serialNos = append(serialNos, i)
	}

	inputData := map[string]interface{}{
		"_id":              uuid.New().String(),
		"product_id":       stock["product_id"],
		"total_quantity":   qty,
		"purchase_id":      stock["purchase_id"],
		"production_id":    fmt.Sprintf("%v", stock["production_id"]),
		"type":             "packed",
		"template_id":      fmt.Sprintf("%v", saleMap["_id"]),
		"total_price":      qty * 450,
		"tin_name":         stock["tin_name"],
		"need_parent_post": true,
		"created_on":       time.Now(),
		"created_by":       userID,

		"tin_grid_data": []map[string]interface{}{
			{
				"production_id":        stock["production_id"],
				"purchase_id":          stock["purchase_id"],
				"serialNo":             fmt.Sprintf("1-%d", tinCount),
				"stock_quantity":       qty,
				"tin_count":            tinCount,
				"warehouse_id":         stock["warehouse_id"],
				"tin_kg":               tinSize,
				"product_price_per_kg": 450,
			},
		},
	}

	_, err := collection.InsertOne(ctx, inputData)
	if err != nil {
		return nil, fmt.Errorf("sold product insert failed: %v", err)
	}

	return inputData, nil
}
