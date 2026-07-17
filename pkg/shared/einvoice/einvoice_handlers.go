package einvoice

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

// getEnvInt retrieves an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	if intVal, err := strconv.Atoi(val); err == nil {
		return intVal
	}
	return defaultValue
}

// parseErrorCodes extracts error codes from error message and returns detailed error information
func parseErrorCodes(errorMessage string) []map[string]interface{} {
	var errors []map[string]interface{}

	// Extract error codes from message like "{\"errorCodes\":\"219,702,\"}"
	// Simple parsing - look for numbers between quotes
	var currentCode string
	inNumber := false

	for _, char := range errorMessage {
		if char >= '0' && char <= '9' {
			currentCode += string(char)
			inNumber = true
		} else if inNumber && (char == ',' || char == '"' || char == '}') {
			if currentCode != "" {
				if code, err := strconv.Atoi(currentCode); err == nil {
					errorInfo := map[string]interface{}{
						"code": code,
					}

					// Look up error details from error codes
					if errorDetail, found := FindError(code); found {
						errorInfo["message"] = errorDetail.Message
						errorInfo["reason"] = errorDetail.Reason
						errorInfo["resolution"] = errorDetail.Resolution
					} else {
						errorInfo["message"] = fmt.Sprintf("Error code %d", code)
					}

					errors = append(errors, errorInfo)
				}
				currentCode = ""
			}
			inNumber = false
		}
	}

	return errors
}

// buildDetailedErrorResponse creates a detailed error response with parsed error codes
func buildDetailedErrorResponse(resp interface{}) fiber.Map {
	errorResponse := fiber.Map{
		"success": false,
	}

	var errorMessage string
	var statusDesc string

	// Try to extract error information based on response type
	switch r := resp.(type) {
	case *StandaloneEwayBillResponse:
		statusDesc = r.StatusDesc
		if r.Error != nil && r.Error.Message != "" {
			errorMessage = r.Error.Message
		}
	case *GenerateEwayBillResponse:
		statusDesc = r.StatusDesc
		if r.Error != nil && r.Error.Message != "" {
			errorMessage = r.Error.Message
		}
	default:
		errorResponse["message"] = "Unknown error occurred"
		return errorResponse
	}

	// Parse error codes if available
	if errorMessage != "" {
		errorCodes := parseErrorCodes(errorMessage)
		if len(errorCodes) > 0 {
			// Build detailed message from error codes (only messages, no codes)
			var messages []string
			for _, ec := range errorCodes {
				if msg, ok := ec["message"].(string); ok {
					messages = append(messages, msg)
				}
			}

			errorResponse["message"] = "E-way bill generation failed"
			errorResponse["details"] = messages
			return errorResponse
		}
	}

	// Fallback to status description
	errorResponse["message"] = statusDesc
	return errorResponse
}

// fetchSaleAndCustomerData retrieves sale and customer details using aggregation
func fetchSaleAndCustomerData(db *mongo.Database, saleId string) (bson.M, bson.M, error) {
	saleCollection := db.Collection("sale")
	pipeline := []bson.M{
		{"$match": bson.M{"_id": saleId}},
		{
			"$lookup": bson.M{
				"from":         "customer",
				"localField":   "customer_id",
				"foreignField": "_id",
				"as":           "customer",
			},
		},
		{"$unwind": "$customer"},
	}

	cursor, err := saleCollection.Aggregate(context.Background(), pipeline)
	if err != nil {
		return nil, nil, err
	}
	defer cursor.Close(context.Background())

	var result bson.M
	if !cursor.Next(context.Background()) {
		return nil, nil, fmt.Errorf("Sale record not found")
	}

	if err = cursor.Decode(&result); err != nil {
		return nil, nil, err
	}

	saleData := result
	customerData, ok := result["customer"].(bson.M)
	if !ok {
		return nil, nil, fmt.Errorf("Customer data not found in result")
	}

	return saleData, customerData, nil
}

// GenerateEInvoiceHandler branches between E-Invoice and Standard Invoice
func GenerateEInvoiceHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	if orgId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "OrgId header is required"})
	}

	saleId := c.Query("sale_id")
	if saleId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sale_id query parameter is required"})
	}

	genType := c.Query("type", "invoice")    // 'einvoice' or 'invoice'
	templateId := c.Query("pdf_template_id") // Optional: template ID for PDF generation
	factoryId := c.Query("factory_id")       // Required for einvoice: factory ID for credentials

	db := database.GetConnection(orgId)
	saleData, customerData, err := fetchSaleAndCustomerData(db, saleId)
	if err != nil {
		log.Printf("fetchSaleAndCustomerData error: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	customerId := saleData["customer_id"].(string)
	gstin, _ := customerData["gst_number"].(string)

	invoiceData := map[string]interface{}{
		"sale_id":       saleId,
		"customer_id":   customerId,
		"customer_name": customerData["customer_name"],
		"gstin":         gstin,
		"pan":           customerData["pan_number"],
		"address":       customerData["registered_area_name"],
		"city":          customerData["registered_city"],
		"state":         customerData["registered_state"],
		"pincode":       customerData["registered_pincode"],
		"bank_details":  customerData["bank_details"],
		"sale_details": fiber.Map{
			"_id":         saleData["_id"],
			"total_price": saleData["total_price"],
			"quantity":    saleData["quantity"],
			"sold_on":     saleData["sold_on"],
			"status":      saleData["status"],
		},
	}

	// Prefer billing address
	addrCollection := db.Collection("customer_billing_address")
	var billing bson.M
	if err := addrCollection.FindOne(context.Background(), bson.M{"customer_id": customerId, "gst_address": true}).Decode(&billing); err == nil {
		if v, ok := billing["street"].(string); ok && v != "" {
			invoiceData["address"] = v
		}
		if v, ok := billing["city"].(string); ok && v != "" {
			invoiceData["city"] = v
		}
		if v, ok := billing["pincode"].(string); ok && v != "" {
			invoiceData["pincode"] = v
		}
		if v, ok := billing["state"].(string); ok && v != "" {
			invoiceData["state"] = v
		}
	}

	if genType == "invoice" {
		return processStandardInvoice(c, orgId, saleId, customerId, gstin, saleData, invoiceData, templateId)
	}

	return processEInvoice(c, orgId, saleId, customerId, gstin, saleData, invoiceData, templateId, factoryId)
}

func processStandardInvoice(c *fiber.Ctx, orgId, saleId, customerId, gstin string, saleData, invoiceData map[string]interface{}, templateId string) error {
	db := database.GetConnection(orgId)
	einvoiceCollection := db.Collection("invoice")
	userToken := utils.GetUserTokenValue(c)

	// Check if invoice already exists for this sale_id
	var existingInvoice bson.M
	err := einvoiceCollection.FindOne(context.Background(), bson.M{"sale_id": saleId, "type": "invoice"}).Decode(&existingInvoice)
	if err == nil {
		// Invoice record exists - check if it's successfully generated
		status, _ := existingInvoice["status"].(string)
		if status == "generated" {
			// Successfully generated - don't allow regeneration
			existingInvoiceNo, _ := existingInvoice["einvoice_number"].(string)
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"success":         false,
				"message":         "Invoice already generated for this sale",
				"einvoice_number": existingInvoiceNo,
			})
		}
		// If status is "failed", delete the old record and allow retry
		if status == "failed" {
			log.Printf("Deleting failed invoice record for sale_id: %s to allow retry", saleId)
			_, _ = einvoiceCollection.DeleteOne(context.Background(), bson.M{"_id": existingInvoice["_id"]})
		}
	}

	var generatedInvoiceNo string
	generateNum := true
	if val, exists := saleData["einvoice_number"]; exists {
		if s, ok := val.(string); ok && s != "" {
			generatedInvoiceNo = s
			generateNum = false
		}
	}

	if generateNum {
		var factoryId string

		if val, ok := saleData["factory"]; ok {
			if str, ok := val.(string); ok {
				factoryId = str
			}
		} else {
			factoryId = userToken.FactoryId
		}

		if factoryId == "" {
			// Fallback to a generic invoice number if factoryId is not available
			count, _ := helper.HandlerCollectionCount(orgId, "invoice", bson.M{
				"sale_id":     saleId,
				"customer_id": customerId,
				"type":        "invoice",
			})
			generatedInvoiceNo = fmt.Sprintf("INV-%s-%s-%d", saleId, customerId, count+1)
		} else {
			factoryconfig := make(map[string]interface{})
			// Load from DB only if not present in cache
			if _, ok := factoryconfig[factoryId]; !ok {
				factoryDoc, err := GetDataById(orgId, factoryId, "config")
				if err == nil {
					if einvoice, ok := factoryDoc["sale_einvoice"]; ok {
						factoryconfig[factoryId] = einvoice
					}
				}
			}

			year := time.Now().Format("06")  // FY year (e.g., 25,26)
			month := time.Now().Format("01") // 01–12

			unique := "SEQ|" + factoryId + "-FY" + year + "-" + month + "-"

			sno, _ := helper.HandleSequenceOrder(unique, orgId, "invoice")
			generatedInvoiceNo = sno
		}
	}

	// Take description from sale table if available
	prdDesc := "Product"
	if desc, ok := saleData["product_description"].(string); ok && desc != "" {
		prdDesc = desc
	}

	// Use existing HSN lookup or fallback
	hsnCode := "08013100"
	unit := "KGS"
	typeOfSale, _ := saleData["type_of_sale"].(string)

	var lookupProductId string
	if typeOfSale != "kernel" {
		lookupProductId = "RCN"
	} else {
		var sp_info bson.M
		if err := db.Collection("sold_products_info").FindOne(context.Background(), bson.M{"pdf_template_id": saleId}).Decode(&sp_info); err == nil {
			if pid, ok := sp_info["product_id"].(string); ok {
				lookupProductId = pid
			}
		}
	}

	if lookupProductId != "" {
		var prod bson.M
		if err := db.Collection("product").FindOne(context.Background(), bson.M{"_id": lookupProductId}).Decode(&prod); err == nil {
			if h, ok := prod["hsn_code"].(string); ok && h != "" {
				hsnCode = h
			}
			if unitVal, ok := prod["unit"].(string); ok && unitVal != "" {
				unit = unitVal
			}
			// If name from product table is preferred over generic fallback but after sale record desc
			if prdDesc == "Product" {
				if name, ok := prod["product_name"].(string); ok && name != "" {
					prdDesc = name
				}
			}
		}
	}

	invoiceDoc := bson.M{
		"_id":             uuid.New().String(),
		"org_id":          orgId,
		"sale_id":         saleId,
		"customer_id":     customerId,
		"gstin":           gstin,
		"created_at":      time.Now(),
		"invoice_data":    invoiceData,
		"type":            "invoice",
		"einvoice_number": generatedInvoiceNo,
		"description":     prdDesc,
		"hsn_code":        hsnCode,
		"unit":            unit,
		"status":          "generated",
	}

	// Insert new invoice record (not upsert to prevent duplicates)
	_, err = einvoiceCollection.InsertOne(context.Background(), invoiceDoc)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "Failed to save invoice record", "error": err.Error()})
	}

	// Check if we need to generate PDF/PNG report
	invoiceFormat := os.Getenv("INVOICE_FORMAT")

	if invoiceFormat == "report" && templateId != "" {
		// Add delay to ensure invoice record is fully committed to database
		time.Sleep(500 * time.Millisecond)

		log.Printf("Generating standard invoice report for sale_id: %s using template: %s", saleId, templateId)
		reportURL, err := generateInvoiceReport(orgId, saleId, templateId, userToken.UserId, "invoice")
		if err != nil {
			log.Printf("Warning: Failed to generate invoice report: %v", err)
		} else {
			// Update invoice record with report URL
			_, _ = einvoiceCollection.UpdateOne(
				context.Background(),
				bson.M{"_id": invoiceDoc["_id"]},
				bson.M{"$set": bson.M{"report_url": reportURL}},
			)
			// Also update sale table with report URL
			_, _ = db.Collection("sale").UpdateOne(
				context.Background(),
				bson.M{"_id": saleId},
				bson.M{"$set": bson.M{"report_url": reportURL}},
			)
		}
	}

	// Update sale table only after successful invoice creation
	saleUpdate := bson.M{
		"$set": bson.M{
			"invoice_generated":    true,
			"einvoice_number":      generatedInvoiceNo,
			"invoice_type":         "invoice",
			"invoice_id":           invoiceDoc["_id"],
			"invoice_generated_at": time.Now(),
			"updated_by":           userToken.UserId,
			"updated_on":           time.Now(),
		},
	}
	_, err = db.Collection("sale").UpdateOne(context.Background(), bson.M{"_id": saleId}, saleUpdate)
	if err != nil {
		log.Printf("Warning: Failed to update sale table: %v", err)
	}

	return c.JSON(fiber.Map{
		"success":         true,
		"message":         "Invoice generated successfully",
		"einvoice_number": generatedInvoiceNo,
	})
}

func processEInvoice(c *fiber.Ctx, orgId, saleId, customerId, gstin string, saleData, invoiceData map[string]interface{}, templateId string, factoryId string) error {
	db := database.GetConnection(orgId)
	einvoiceCollection := db.Collection("invoice")

	// Check if e-invoice already exists for this sale_id
	var existingEInvoice bson.M
	err := einvoiceCollection.FindOne(context.Background(), bson.M{"sale_id": saleId, "type": "einvoice"}).Decode(&existingEInvoice)
	if err == nil {
		// E-invoice record exists - check if it's successfully generated
		status, _ := existingEInvoice["status"].(string)
		if status == "generated" {
			// Successfully generated - don't allow regeneration
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"success": false,
				"message": "E-Invoice already generated for this sale",
			})
		}
		// If status is "failed", delete the old record and allow retry
		if status == "failed" {
			log.Printf("Deleting failed e-invoice record for sale_id: %s to allow retry", saleId)
			_, _ = einvoiceCollection.DeleteOne(context.Background(), bson.M{"_id": existingEInvoice["_id"]})
		}
	}

	// Get purchase_id from sale data
	var purchaseId string
	if val, ok := saleData["purchase_id"]; ok {
		if str, ok := val.(string); ok {
			purchaseId = str
		}
	}

	if purchaseId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "purchase_id not found in sale data",
		})
	}

	// Fetch purchase record to get factory_id
	var purchase bson.M
	err = db.Collection("purchase").FindOne(context.Background(), bson.M{"_id": purchaseId}).Decode(&purchase)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Purchase record not found",
			"error":   err.Error(),
		})
	}

	if factoryId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "factory_id not found in purchase record",
		})
	}

	// Fetch factory details from factory table
	var factory bson.M
	err = db.Collection("factory").FindOne(context.Background(), bson.M{"_id": factoryId}).Decode(&factory)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Factory not found",
			"error":   err.Error(),
		})
	}

	// Extract GSTIN from factory (still needed for seller GSTIN)
	sellerGstin, _ := factory["gst_no"].(string)
	if sellerGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "GST number not found in factory record",
		})
	}

	// Read e-invoice credentials from environment variables
	email := os.Getenv("EINVOICE_EMAIL")
	username := os.Getenv("EINVOICE_USERNAME")
	password := os.Getenv("EINVOICE_PASSWORD")
	clientID := os.Getenv("EINVOICE_CLIENT_ID")
	clientSecret := os.Getenv("EINVOICE_CLIENT_SECRET")
	ipAddress := os.Getenv("EINVOICE_IP_ADDRESS")

	// Use default IP if not provided
	if ipAddress == "" {
		ipAddress = "127.0.0.1"
	}

	// Validate required credentials
	if email == "" || username == "" || password == "" || clientID == "" || clientSecret == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Missing required e-invoice credentials in environment variables",
		})
	}

	// Build config using environment credentials
	config := Config{
		AuthEndpoint:        os.Getenv("EINVOICE_AUTH_ENDPOINT"),
		GenerateIRNEndpoint: os.Getenv("EINVOICE_GENERATE_IRN_ENDPOINT"),
		CancelEndpoint:      os.Getenv("EINVOICE_CANCEL_ENDPOINT"),
		EwayBillEndpoint:    os.Getenv("EINVOICE_EWAYBILL_ENDPOINT"),
		Email:               email,
		Username:            username,
		Password:            password,
		IPAddress:           ipAddress,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		GSTIN:               sellerGstin,
	}

	client := NewClient(config)

	einvoiceDoc := bson.M{
		"_id":          uuid.New().String(),
		"org_id":       orgId,
		"sale_id":      saleId,
		"customer_id":  customerId,
		"gstin":        gstin,
		"factory_id":   factoryId,
		"created_at":   time.Now(),
		"invoice_data": invoiceData,
		"type":         "einvoice",
	}

	// Initialize API logs with new structure
	apiLogs := bson.M{
		"requests": []bson.M{
			{
				"method": "invoice",
				"api":    []bson.M{},
			},
		},
	}

	// Step 1: Authenticate
	log.Printf("Step 1: Authenticating with e-invoice API")
	authLog := bson.M{
		"endpoint":  "authenticate",
		"timestamp": time.Now(),
		"request": bson.M{
			"email":    config.Email,
			"username": config.Username,
			"gstin":    config.GSTIN,
		},
	}

	err = client.Authenticate()
	if err != nil {
		authLog["success"] = false
		authLog["error"] = err.Error()

		// Add to invoice method API array
		apiLogs["requests"].([]bson.M)[0]["api"] = []bson.M{authLog}

		einvoiceDoc["status"] = "failed"
		einvoiceDoc["failed_at"] = time.Now()
		einvoiceDoc["error"] = err.Error()
		einvoiceDoc["api_logs"] = apiLogs
		_, _ = einvoiceCollection.InsertOne(context.Background(), einvoiceDoc)

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Authentication failed",
			"error":   err.Error(),
		})
	}

	session := client.GetSession()
	authLog["success"] = true
	authLog["response"] = bson.M{
		"auth_token":   session.AuthToken,
		"client_id":    session.ClientID,
		"token_expiry": session.TokenExpiry.Unix(),
	}

	// Step 2: Generate IRN
	log.Printf("Step 2: Generating IRN")
	irnLog := bson.M{
		"endpoint":  "generate_irn",
		"timestamp": time.Now(),
	}

	irnResponse, irnRequest, err := generateIRNForSaleWithLog(client, config, saleData, invoiceData, gstin, db, factory)
	irnLog["request"] = irnRequest

	if err != nil {
		irnLog["success"] = false
		irnLog["error"] = err.Error()

		// Add both logs to invoice method API array
		apiLogs["requests"].([]bson.M)[0]["api"] = []bson.M{authLog, irnLog}

		einvoiceDoc["status"] = "failed"
		einvoiceDoc["failed_at"] = time.Now()
		einvoiceDoc["error"] = err.Error()
		einvoiceDoc["api_logs"] = apiLogs
		_, _ = einvoiceCollection.InsertOne(context.Background(), einvoiceDoc)

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "IRN generation failed",
			"error":   err.Error(),
		})
	}

	irnLog["success"] = true
	irnLog["response"] = irnResponse

	// Add both logs to invoice method API array
	apiLogs["requests"].([]bson.M)[0]["api"] = []bson.M{authLog, irnLog}

	// All steps successful - save complete document
	einvoiceDoc["completed_at"] = time.Now()
	einvoiceDoc["status"] = "generated"
	einvoiceDoc["api_logs"] = apiLogs
	_, err = einvoiceCollection.InsertOne(context.Background(), einvoiceDoc)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to save e-invoice record",
			"error":   err.Error(),
		})
	}

	// Update sale table and upload QR code
	err = finalizeEInvoice(c, db, orgId, saleId, einvoiceDoc, irnResponse, templateId)
	if err != nil {
		log.Printf("Warning: Failed to finalize e-invoice: %v", err)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "E-Invoice (IRN) generated successfully",
	})
}

// GenerateEwayBillHandler handles E-way bill generation as a separate API
func GenerateEwayBillHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	saleId := c.Query("sale_id")
	factoryId := c.Query("factory_id")
	onlyEwaybill := c.Query("only_ewaybill", "false") // "true" for standalone, "false" for IRN-based

	if orgId == "" || saleId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "OrgId and sale_id required"})
	}

	if factoryId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "factory_id query parameter is required"})
	}

	db := database.GetConnection(orgId)

	// Check if e-way bill already exists for this sale_id
	invoiceCollection := db.Collection("invoice")
	var existingInvoice bson.M
	err := invoiceCollection.FindOne(context.Background(), bson.M{"sale_id": saleId}).Decode(&existingInvoice)
	if err != nil && onlyEwaybill == "false" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "Invoice record not found. Generate invoice/e-invoice first."})
	}

	// Check if e-way bill already generated
	if err == nil {
		if ewaybillData, ok := existingInvoice["ewaybill"].(bson.M); ok {
			if ewbNo, ok := ewaybillData["no"].(int64); ok && ewbNo > 0 {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"success":         false,
					"message":         "E-Way Bill already generated for this sale",
					"ewaybill_number": ewbNo,
				})
			}
		}
	}

	saleCollection := db.Collection("sale")
	var saleDoc bson.M
	if err := saleCollection.FindOne(context.Background(), bson.M{"_id": saleId}).Decode(&saleDoc); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "Sale record not found"})
	}

	// Fetch factory details from factory table
	var factory bson.M
	err = db.Collection("factory").FindOne(context.Background(), bson.M{"_id": factoryId}).Decode(&factory)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Factory not found",
			"error":   err.Error(),
		})
	}

	// Extract GSTIN from factory (still needed for seller GSTIN)
	sellerGstin, _ := factory["gst_no"].(string)
	if sellerGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "GST number not found in factory record",
		})
	}

	// Read e-invoice credentials from environment variables
	email := os.Getenv("EINVOICE_EMAIL")
	username := os.Getenv("EINVOICE_USERNAME")
	password := os.Getenv("EINVOICE_PASSWORD")
	clientID := os.Getenv("EINVOICE_CLIENT_ID")
	clientSecret := os.Getenv("EINVOICE_CLIENT_SECRET")
	ipAddress := os.Getenv("EINVOICE_IP_ADDRESS")

	// Use default IP if not provided
	if ipAddress == "" {
		ipAddress = "127.0.0.1"
	}

	// Validate required credentials
	if email == "" || username == "" || password == "" || clientID == "" || clientSecret == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Missing required e-invoice credentials in environment variables",
		})
	}

	if onlyEwaybill == "true" {
		// Standalone e-way bill generation (without IRN)
		return generateStandaloneEwayBill(c, db, orgId, saleId, saleDoc, factory)
	}

	// IRN-based e-way bill generation (existing flow)
	irnValue, _ := saleDoc["einvoice_number"].(string)
	if irnValue == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "IRN required for E-way bill. Generate IRN first or use only_ewaybill=true for standalone e-way bill."})
	}

	config := Config{
		AuthEndpoint:     os.Getenv("EINVOICE_AUTH_ENDPOINT"),
		EwayBillEndpoint: os.Getenv("EINVOICE_EWAYBILL_ENDPOINT"),
		Email:            email,
		Username:         username,
		Password:         password,
		IPAddress:        ipAddress,
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		GSTIN:            sellerGstin,
	}

	client := NewClient(config)
	if err := client.Authenticate(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "Authentication failed", "error": err.Error()})
	}

	// Build e-way bill request - get distance from sale table
	distance := 100 // Default fallback
	if d, ok := saleDoc["transport_distance"].(float64); ok && d > 0 {
		distance = int(d)
		log.Printf("Using transport_distance from sale table: %d KM", distance)
	} else {
		log.Printf("transport_distance not found in sale table, using default: %d KM", distance)
	}

	vehicleNumber := "KA12ER1234"
	if v, ok := saleDoc["vehicle_number"].(string); ok && v != "" {
		vehicleNumber = v
	}

	ewayReq := GenerateEwayBillRequest{
		Irn:        irnValue,
		Distance:   distance,
		TransMode:  "1",
		TransDocDt: formatDateForAPI(time.Now()),
		TransDocNo: fmt.Sprintf("TR%s", saleId[:min(13, len(saleId))]),
		VehNo:      vehicleNumber,
		VehType:    "R",
	}

	// Store API log with new structure
	apiLogs := bson.M{
		"requests": []bson.M{
			{
				"method": "ewaybill",
				"api": []bson.M{
					{
						"endpoint":  "generate_ewaybill",
						"timestamp": time.Now(),
						"request":   ewayReq,
					},
				},
			},
		},
	}

	ewayResp, err := client.GenerateEwayBill(ewayReq)
	ewayLog := apiLogs["requests"].([]bson.M)[0]["api"].([]bson.M)[0]

	if err != nil {
		ewayLog["success"] = false
		ewayLog["error"] = err.Error()

		// Append e-way bill log to existing api_logs
		_, _ = invoiceCollection.UpdateOne(
			context.Background(),
			bson.M{"sale_id": saleId},
			bson.M{"$push": bson.M{"api_logs.requests": apiLogs["requests"].([]bson.M)[0]}},
		)

		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	if !isSuccessStatus(ewayResp.StatusCode) {
		ewayLog["success"] = false
		ewayLog["response"] = ewayResp

		// Append e-way bill log to existing api_logs
		_, _ = invoiceCollection.UpdateOne(
			context.Background(),
			bson.M{"sale_id": saleId},
			bson.M{"$push": bson.M{"api_logs.requests": apiLogs["requests"].([]bson.M)[0]}},
		)

		// Build detailed error response with parsed error codes
		errorResponse := buildDetailedErrorResponse(ewayResp)
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse)
	}

	ewayLog["success"] = true
	ewayLog["response"] = ewayResp

	// Update sale table with nested ewaybill object only after successful generation
	userToken := utils.GetUserTokenValue(c)
	ewaybillData := bson.M{
		"no":           ewayResp.Data.EwbNo,
		"date":         ewayResp.Data.EwbDt,
		"valid_till":   ewayResp.Data.EwbValidTill,
		"generated_at": time.Now(),
	}

	saleUpdate := bson.M{
		"$set": bson.M{
			"ewaybill_generated": true,
			"ewaybill_number":    ewayResp.Data.EwbNo,
			"updated_by":         userToken.UserId,
			"updated_on":         time.Now(),
		},
	}
	_, err = db.Collection("sale").UpdateOne(context.Background(), bson.M{"_id": saleId}, saleUpdate)
	if err != nil {
		log.Printf("Warning: Failed to update sale table: %v", err)
	}

	// Update invoice record with ewaybill details and append to api_logs
	auditUpdate := bson.M{
		"$set": bson.M{
			"ewaybill": ewaybillData,
		},
		"$push": bson.M{
			"api_logs.requests": apiLogs["requests"].([]bson.M)[0],
		},
	}
	_, err = db.Collection("invoice").UpdateOne(context.Background(), bson.M{"sale_id": saleId}, auditUpdate)
	if err != nil {
		log.Printf("Warning: Failed to update invoice record: %v", err)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "E-Way Bill generated successfully",
	})
}

// generateStandaloneEwayBill generates e-way bill without IRN using standalone API
func generateStandaloneEwayBill(c *fiber.Ctx, db *mongo.Database, orgId, saleId string, saleDoc, factory bson.M) error {
	// Extract GSTIN from factory (still needed for seller GSTIN)
	sellerGstin, _ := factory["gst_no"].(string)
	if sellerGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "GST number not found in factory record",
		})
	}

	// Read e-way bill credentials from environment variables
	email := os.Getenv("EWAYBILL_EMAIL")
	username := os.Getenv("EWAYBILL_USERNAME")
	password := os.Getenv("EWAYBILL_PASSWORD")
	clientID := os.Getenv("EWAYBILL_CLIENT_ID")
	clientSecret := os.Getenv("EWAYBILL_CLIENT_SECRET")
	ipAddress := os.Getenv("EWAYBILL_IP_ADDRESS")

	// Use default IP if not provided
	if ipAddress == "" {
		ipAddress = "127.0.0.1"
	}

	// Validate required credentials
	if email == "" || username == "" || password == "" || clientID == "" || clientSecret == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Missing required e-way bill credentials in environment variables",
		})
	}

	// Fetch customer data
	customerId, _ := saleDoc["customer_id"].(string)
	var customerData bson.M
	if err := db.Collection("customer").FindOne(context.Background(), bson.M{"_id": customerId}).Decode(&customerData); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "Customer not found"})
	}

	buyerGstin, _ := customerData["gst_number"].(string)
	if buyerGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "Customer GSTIN required for e-way bill"})
	}

	// Extract seller details from factory
	sellerName, _ := factory["factory_name"].(string)
	sellerAddr1, _ := factory["registered_street"].(string)
	sellerPlace, _ := factory["registered_city"].(string)
	sellerPincode, _ := factory["registered_pincode"].(string)
	sellerStateCode := sellerGstin[:2]

	// Extract buyer details
	buyerName, _ := customerData["customer_name"].(string)
	buyerAddr1, _ := customerData["registered_area_name"].(string)
	buyerPlace, _ := customerData["registered_city"].(string)
	buyerPincode, _ := customerData["registered_pincode"].(string)
	buyerStateCode := buyerGstin[:2]

	// Convert pincodes to int
	sellerPincodeInt, _ := strconv.Atoi(sellerPincode)
	buyerPincodeInt, _ := strconv.Atoi(buyerPincode)
	sellerStateCodeInt, _ := strconv.Atoi(sellerStateCode)
	buyerStateCodeInt, _ := strconv.Atoi(buyerStateCode)

	// Get transport distance from sale table
	var transportDistance int
	if existingDistance, ok := saleDoc["transport_distance"].(float64); ok && existingDistance > 0 {
		transportDistance = int(existingDistance)
		log.Printf("Using transport_distance from sale table: %d KM", transportDistance)
	} else {
		transportDistance = 100 // Default fallback
		if ok {
			log.Printf("transport_distance in sale table is %.2f (invalid), using default: %d KM", existingDistance, transportDistance)
		} else {
			log.Printf("transport_distance not found in sale table, using default: %d KM", transportDistance)
		}
	}

	// Validate distance is within acceptable range (1-4000 KM)
	if transportDistance < 1 {
		log.Printf("Warning: transport_distance %d is too low, setting to minimum 1 KM", transportDistance)
		transportDistance = 1
	} else if transportDistance > 4000 {
		log.Printf("Warning: transport_distance %d is too high, setting to maximum 4000 KM", transportDistance)
		transportDistance = 4000
	}

	// Get sale details
	totalPriceWithTax, _ := saleDoc["total_price"].(float64)
	gstRate, _ := saleDoc["gst"].(float64)
	quantity, _ := saleDoc["quantity"].(float64)

	// Extract base price from total (total_price includes tax)
	// Formula: basePrice = totalPrice / (1 + gstRate/100)
	basePrice := roundTo2Decimals(totalPriceWithTax / (1 + gstRate/100))

	// Calculate tax amounts based on base price
	isInterState := sellerStateCode != buyerStateCode
	var igstAmt, cgstAmt, sgstAmt float64
	if isInterState {
		// For inter-state: IGST = basePrice * gstRate / 100
		igstAmt = roundTo2Decimals(basePrice * gstRate / 100)
		cgstAmt = 0.0
		sgstAmt = 0.0
	} else {
		// For intra-state: CGST = SGST = basePrice * gstRate / 100 / 2
		igstAmt = 0.0
		cgstAmt = calculateTax(basePrice, gstRate)
		sgstAmt = calculateTax(basePrice, gstRate)
	}

	totInvValue := totalPriceWithTax

	// Get product details
	hsnCode := "08013100"
	prdDesc := "Product"
	unit := "KGS"
	typeOfSale, _ := saleDoc["type_of_sale"].(string)

	var lookupProductId string
	if typeOfSale != "kernel" {
		lookupProductId = "RCN"
	} else {
		var sp_info bson.M
		if err := db.Collection("sold_products_info").FindOne(context.Background(), bson.M{"pdf_template_id": saleId}).Decode(&sp_info); err == nil {
			if pid, ok := sp_info["product_id"].(string); ok {
				lookupProductId = pid
			}
		}
	}

	if lookupProductId != "" {
		var prod bson.M
		if err := db.Collection("product").FindOne(context.Background(), bson.M{"_id": lookupProductId}).Decode(&prod); err == nil {
			if h, ok := prod["hsn_code"].(string); ok && h != "" {
				hsnCode = h
			}
			if name, ok := prod["product_name"].(string); ok && name != "" {
				prdDesc = name
			}
			if u, ok := prod["unit"].(string); ok && u != "" {
				unit = u
			}
		}
	}

	// Get vehicle details
	vehicleNumber := ""
	if v, ok := saleDoc["vehicle_number"].(string); ok && v != "" {
		vehicleNumber = v
	}

	// Build standalone e-way bill request
	ewayReq := StandaloneEwayBillRequest{
		SupplyType:       "O", // Outward
		SubSupplyType:    "1", // Supply
		TransactionType:  1,   // Regular (as integer)
		DocType:          "INV",
		DocNo:            saleId,
		DocDate:          formatDateForAPI(saleDoc["sold_on"]),
		FromGstin:        sellerGstin,
		FromTrdName:      sellerName,
		FromAddr1:        sellerAddr1,
		FromPlace:        sellerPlace,
		FromPincode:      sellerPincodeInt,
		FromStateCode:    sellerStateCodeInt,
		ActFromStateCode: sellerStateCodeInt, // Same as FromStateCode for regular transactions
		ToGstin:          buyerGstin,
		ToTrdName:        buyerName,
		ToAddr1:          buyerAddr1,
		ToPlace:          buyerPlace,
		ToPincode:        buyerPincodeInt,
		ToStateCode:      buyerStateCodeInt,
		ActToStateCode:   buyerStateCodeInt, // Same as ToStateCode for regular transactions
		TotalValue:       basePrice,
		CgstValue:        cgstAmt,
		SgstValue:        sgstAmt,
		IgstValue:        igstAmt,
		CessValue:        0.0,
		TotInvValue:      totInvValue,
		TransMode:        "1",                                  // Road
		TransDistance:    fmt.Sprintf("%d", transportDistance), // Convert to string
		VehicleNo:        vehicleNumber,
		VehicleType:      "R", // Regular
		ItemList: []StandaloneEwayBillItem{
			{
				ProductName: prdDesc,
				HsnCode:     hsnCode,
				Quantity:    quantity,
				QtyUnit:     unit,
				CgstRate: func() float64 {
					if isInterState {
						return 0
					} else {
						return gstRate / 2
					}
				}(),
				SgstRate: func() float64 {
					if isInterState {
						return 0
					} else {
						return gstRate / 2
					}
				}(),
				IgstRate:      gstRate,
				TaxableAmount: basePrice,
			},
		},
	}

	// Configure standalone e-way bill client
	config := Config{
		EwayBillAuthEndpoint:     os.Getenv("EWAYBILL_AUTH_ENDPOINT"),
		EwayBillGenerateEndpoint: os.Getenv("EWAYBILL_GENERATE_ENDPOINT"),
		Email:                    email,
		Username:                 username,
		Password:                 password,
		IPAddress:                ipAddress,
		ClientID:                 clientID,
		ClientSecret:             clientSecret,
		GSTIN:                    sellerGstin,
	}

	client := NewClient(config)

	// Store authentication log
	authLog := bson.M{
		"endpoint":  "authenticate",
		"timestamp": time.Now(),
		"request": bson.M{
			"email":    config.Email,
			"username": config.Username,
			"gstin":    config.GSTIN,
		},
	}

	// Authenticate using standalone e-way bill endpoint
	if err := client.AuthenticateEwayBill(); err != nil {
		authLog["success"] = false
		authLog["error"] = err.Error()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "Authentication failed", "error": err.Error()})
	}

	authLog["success"] = true
	authLog["response"] = bson.M{
		"status":  "authenticated",
		"message": "E-way bill authentication successful",
	}

	// Store e-way bill generation log
	ewayLog := bson.M{
		"endpoint":  "generate_ewaybill",
		"timestamp": time.Now(),
		"request":   ewayReq,
	}

	// Generate standalone e-way bill
	ewayResp, err := client.GenerateStandaloneEwayBill(ewayReq)
	if err != nil {
		ewayLog["success"] = false
		ewayLog["error"] = err.Error()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	if !isSuccessStatus(ewayResp.StatusCode) {
		ewayLog["success"] = false
		ewayLog["response"] = ewayResp

		// Build detailed error response with parsed error codes
		errorResponse := buildDetailedErrorResponse(ewayResp)
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse)
	}

	ewayLog["success"] = true
	ewayLog["response"] = ewayResp

	// Combine API logs with new structure
	apiLogs := bson.M{
		"requests": []bson.M{
			{
				"method": "ewaybill",
				"api":    []bson.M{authLog, ewayLog},
			},
		},
	}

	// Update sale table with e-way bill details
	userToken := utils.GetUserTokenValue(c)
	ewaybillData := bson.M{
		"no":           ewayResp.Data.EwbNo,
		"date":         ewayResp.Data.EwbDt,
		"valid_till":   ewayResp.Data.EwbValidTill,
		"generated_at": time.Now(),
		"type":         "standalone", // Mark as standalone e-way bill
	}

	saleUpdate := bson.M{
		"$set": bson.M{
			"ewaybill_generated": true,
			"ewaybill_number":    ewayResp.Data.EwbNo,
			"transport_distance": transportDistance,
			"updated_by":         userToken.UserId,
			"updated_on":         time.Now(),
		},
	}
	_, err = db.Collection("sale").UpdateOne(context.Background(), bson.M{"_id": saleId}, saleUpdate)
	if err != nil {
		log.Printf("Warning: Failed to update sale table: %v", err)
	}

	// Create/update invoice record with e-way bill details
	invoiceCollection := db.Collection("invoice")
	var existingInvoice bson.M
	err = invoiceCollection.FindOne(context.Background(), bson.M{"sale_id": saleId}).Decode(&existingInvoice)

	if err != nil {
		// No invoice record exists, create one
		invoiceDoc := bson.M{
			"_id":        uuid.New().String(),
			"org_id":     orgId,
			"sale_id":    saleId,
			"type":       "standalone_ewaybill",
			"created_at": time.Now(),
			"ewaybill":   ewaybillData,
			"api_logs":   apiLogs,
		}
		_, _ = invoiceCollection.InsertOne(context.Background(), invoiceDoc)
	} else {
		// Update existing invoice record
		auditUpdate := bson.M{
			"$set": bson.M{
				"ewaybill": ewaybillData,
				"api_logs": apiLogs,
			},
		}
		_, _ = invoiceCollection.UpdateOne(context.Background(), bson.M{"sale_id": saleId}, auditUpdate)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Standalone E-Way Bill generated successfully",
		"data": fiber.Map{
			"ewaybill_number": ewayResp.Data.EwbNo,
			"ewaybill_date":   ewayResp.Data.EwbDt,
			"valid_till":      ewayResp.Data.EwbValidTill,
		},
	})
}

// })

// extractFirstInt finds the first integer in a string
func extractFirstInt(s string) (int, bool) {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		} else if len(digits) > 0 {
			break
		}
	}
	if len(digits) == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(string(digits))
	if err != nil {
		return 0, false
	}
	return v, true
}

// deleteS3FileFromURL extracts the file path from S3 URL and deletes it
func deleteS3FileFromURL(s3URL string) error {
	if s3URL == "" {
		return nil
	}

	// Extract the file path from URL
	// Expected format: https://cerp.sgp1.digitaloceanspaces.com/report/orgId/stock_report/userId/filename
	baseURL := os.Getenv("S3_APIENDPOINT")
	if !strings.HasPrefix(s3URL, baseURL) {
		return fmt.Errorf("invalid S3 URL format: %s", s3URL)
	}

	filePath := strings.TrimPrefix(s3URL, baseURL)
	bucket := os.Getenv("S3_BUCKET_CERP")
	if bucket == "" {
		return fmt.Errorf("S3_BUCKET_CERP environment variable not set")
	}

	// Use the helper package DeleteFile function
	success := helper.DeleteFile(bucket, filePath)
	if !success {
		return fmt.Errorf("failed to delete file from S3: %s", filePath)
	}

	log.Printf("Successfully deleted S3 file: %s", filePath)
	return nil
}

// reverseSaleAndInvoiceData reverses the sale and invoice data when cancellation is successful
func reverseSaleAndInvoiceData(db *mongo.Database, saleId string, cancelType string, userToken utils.UserToken) error {
	// Get the sale document
	var saleDoc bson.M
	err := db.Collection("sale").FindOne(context.Background(), bson.M{"_id": saleId}).Decode(&saleDoc)
	if err != nil {
		return fmt.Errorf("failed to fetch sale document: %w", err)
	}

	// Get the invoice document
	var invoiceDoc bson.M
	err = db.Collection("invoice").FindOne(context.Background(), bson.M{"sale_id": saleId}).Decode(&invoiceDoc)
	if err != nil {
		return fmt.Errorf("failed to fetch invoice document: %w", err)
	}

	// Delete S3 files if they exist
	if reportURL, ok := invoiceDoc["report_url"].(string); ok && reportURL != "" {
		if err := deleteS3FileFromURL(reportURL); err != nil {
			log.Printf("Warning: Failed to delete report_url from S3: %v", err)
		}
	}

	// Check for report_url in einvoice nested object
	if einvoiceData, ok := invoiceDoc["einvoice"].(bson.M); ok {
		if reportURL, ok := einvoiceData["report_url"].(string); ok && reportURL != "" {
			if err := deleteS3FileFromURL(reportURL); err != nil {
				log.Printf("Warning: Failed to delete einvoice report_url from S3: %v", err)
			}
		}
	}

	// Prepare reversal updates based on cancel type
	saleUpdate := bson.M{
		"updated_by": userToken.UserId,
		"updated_on": time.Now(),
	}

	if cancelType == "irn" || cancelType == "einvoice" || cancelType == "invoice" {
		// Reverse invoice related fields
		saleUpdate["invoice_generated"] = false
		saleUpdate["invoice_cancelled"] = true
		saleUpdate["einvoice_number"] = ""
		saleUpdate["irn_number"] = ""
		saleUpdate["ack_no"] = ""
		saleUpdate["ack_date"] = ""
		saleUpdate["report_url"] = ""
	}

	if cancelType == "ewaybill" {
		// Reverse e-way bill related fields
		saleUpdate["ewaybill_generated"] = false
		saleUpdate["ewaybill_cancelled"] = true
		saleUpdate["ewaybill_number"] = ""
	}

	// Update sale table
	_, err = db.Collection("sale").UpdateOne(
		context.Background(),
		bson.M{"_id": saleId},
		bson.M{"$set": saleUpdate},
	)
	if err != nil {
		return fmt.Errorf("failed to update sale table: %w", err)
	}

	// Update invoice table - mark as cancelled and remove sensitive data
	invoiceUpdate := bson.M{
		"status":     "cancelled",
		"updated_at": time.Now(),
		"updated_by": userToken.UserId,
		"report_url": "", // Clear report URL
	}

	if cancelType == "einvoice" || cancelType == "invoice" {
		// Clear einvoice data but keep cancel details
		invoiceUpdate["einvoice.report_url"] = ""
	}

	_, err = db.Collection("invoice").UpdateOne(
		context.Background(),
		bson.M{"sale_id": saleId},
		bson.M{"$set": invoiceUpdate},
	)
	if err != nil {
		return fmt.Errorf("failed to update invoice table: %w", err)
	}

	log.Printf("Successfully reversed sale and invoice data for sale_id: %s, cancel_type: %s", saleId, cancelType)
	return nil
}

// CancelIRNHandler cancels an IRN or standard invoice based on type
func CancelIRNHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	if orgId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "OrgId header required"})
	}

	var req CancelIRNRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid request body", "error": err.Error()})
	}

	// Validate required fields from request body
	if req.SaleId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sale_id is required in request body"})
	}

	if req.FactoryId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "factory_id is required in request body"})
	}

	// Get cancel time limit from environment variable (default 24 hours)
	cancelTimeLimitHours := getEnvInt("EINVOICE_CANCEL_TIME_LIMIT_HOURS", 24)

	// Check invoice record and get type
	db := database.GetConnection(orgId)
	einvoiceCollection := db.Collection("invoice")
	var genDoc bson.M
	if err := einvoiceCollection.FindOne(context.Background(), bson.M{"sale_id": req.SaleId}).Decode(&genDoc); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invoice record not found for sale_id"})
	}

	// Get invoice type
	invoiceType, _ := genDoc["type"].(string)
	if invoiceType == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invoice type not found in record"})
	}

	// If type is "invoice" (standard invoice), just remove invoice number without calling API
	if invoiceType == "invoice" {
		userToken := utils.GetUserTokenValue(c)

		// Check if invoice_number exists in sale table
		var saleDoc bson.M
		err := db.Collection("sale").FindOne(context.Background(), bson.M{"_id": req.SaleId}).Decode(&saleDoc)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"success": false,
				"message": "Sale record not found",
			})
		}

		invoiceNumber, _ := saleDoc["einvoice_number"].(string)
		if invoiceNumber == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"message": "Invoice number not found in sale record. Invoice may not be generated yet.",
			})
		}

		// Update invoice record - mark as cancelled
		cancelDetails := bson.M{
			"cancelled_at":   time.Now(),
			"cancel_reason":  req.CnlRsn,
			"cancel_remarks": req.CnlRem,
		}

		_, _ = einvoiceCollection.UpdateOne(
			context.Background(),
			bson.M{"sale_id": req.SaleId},
			bson.M{"$set": bson.M{
				"cancelled":      true,
				"cancel_details": cancelDetails,
				"status":         "cancelled",
				"updated_at":     time.Now(),
				"updated_by":     userToken.UserId,
			}},
		)

		// Reverse sale and invoice data (includes S3 deletion)
		if err := reverseSaleAndInvoiceData(db, req.SaleId, "invoice", userToken); err != nil {
			log.Printf("Warning: Failed to reverse sale and invoice data: %v", err)
		}

		// Insert cancel record
		cancelDoc := bson.M{
			"_id":        uuid.New().String(),
			"org_id":     orgId,
			"sale_id":    req.SaleId,
			"type":       "cancel_invoice",
			"created_at": time.Now(),
			"request":    req,
			"created_by": userToken.UserId,
			"note":       "Standard invoice cancelled without API call",
		}
		_, _ = einvoiceCollection.InsertOne(context.Background(), cancelDoc)

		return c.JSON(fiber.Map{
			"success": true,
			"message": "Standard invoice cancelled successfully",
			"data": fiber.Map{
				"type":           "invoice",
				"sale_id":        req.SaleId,
				"invoice_number": invoiceNumber,
			},
		})
	}

	// If type is "einvoice", proceed with IRN cancellation API call
	if invoiceType == "einvoice" {
		// Set default values if not provided
		if req.CnlRsn == "" {
			req.CnlRsn = "1" // Default reason code (typically "Duplicate" or "Data Entry Mistake")
			log.Printf("CnlRsn not provided, using default: %s", req.CnlRsn)
		}

		if req.CnlRem == "" {
			req.CnlRem = "No remarks provided"
			log.Printf("CnlRem not provided, using default: %s", req.CnlRem)
		}
	} else if invoiceType != "invoice" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": fmt.Sprintf("Invalid invoice type for cancellation: %s", invoiceType),
		})
	}

	// If IRN is not provided in request, fetch it from database
	if req.Irn == "" {
		if einvoiceData, ok := genDoc["einvoice"].(bson.M); ok {
			if irn, ok := einvoiceData["irn"].(string); ok && irn != "" {
				req.Irn = irn
				log.Printf("Fetched IRN from database: %s", irn)
			}
		}

		// If still empty, return error
		if req.Irn == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"message": "IRN not found in request or database",
			})
		}
	}

	var createdAt time.Time
	switch v := genDoc["created_at"].(type) {
	case time.Time:
		createdAt = v
	case primitive.DateTime:
		createdAt = v.Time()
	default:
		createdAt = time.Now()
	}

	timeSinceCreation := time.Since(createdAt)
	if timeSinceCreation > time.Duration(cancelTimeLimitHours)*time.Hour {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success":                false,
			"message":                fmt.Sprintf("Cancel not allowed after %d hours of generation", cancelTimeLimitHours),
			"hours_since_generation": int(timeSinceCreation.Hours()),
		})
	}

	// Fetch factory details from factory table
	var factory bson.M
	err := db.Collection("factory").FindOne(context.Background(), bson.M{"_id": req.FactoryId}).Decode(&factory)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Factory not found",
			"error":   err.Error(),
		})
	}

	// Extract e-invoice credentials from factory.einvoice object
	// Extract GSTIN from factory (still needed for seller GSTIN)
	sellerGstin, _ := factory["gst_no"].(string)
	if sellerGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "GST number not found in factory record",
		})
	}

	// Read e-invoice credentials from environment variables
	email := os.Getenv("EINVOICE_EMAIL")
	username := os.Getenv("EINVOICE_USERNAME")
	password := os.Getenv("EINVOICE_PASSWORD")
	clientID := os.Getenv("EINVOICE_CLIENT_ID")
	clientSecret := os.Getenv("EINVOICE_CLIENT_SECRET")
	ipAddress := os.Getenv("EINVOICE_IP_ADDRESS")

	// Use default IP if not provided
	if ipAddress == "" {
		ipAddress = "127.0.0.1"
	}

	// Validate required credentials
	if email == "" || username == "" || password == "" || clientID == "" || clientSecret == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Missing required e-invoice credentials in environment variables",
		})
	}

	config := Config{
		AuthEndpoint:        os.Getenv("EINVOICE_AUTH_ENDPOINT"),
		GenerateIRNEndpoint: os.Getenv("EINVOICE_GENERATE_IRN_ENDPOINT"),
		CancelEndpoint:      os.Getenv("EINVOICE_CANCEL_ENDPOINT"),
		Email:               email,
		Username:            username,
		Password:            password,
		IPAddress:           ipAddress,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		GSTIN:               sellerGstin,
	}

	client := NewClient(config)
	if err := client.Authenticate(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "authentication failed", "error": err.Error()})
	}

	// Store API log
	apiLog := bson.M{
		"timestamp": time.Now(),
		"request":   req,
	}

	cancelResp, err := client.CancelIRN(req)
	if err != nil {
		apiLog["success"] = false
		apiLog["error"] = err.Error()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	apiLog["success"] = isSuccessStatus(cancelResp.StatusCode)
	apiLog["response"] = cancelResp

	// Check if cancellation failed
	if !isSuccessStatus(cancelResp.StatusCode) {
		// Try to map error code to friendly message
		if v, ok := extractFirstInt(cancelResp.StatusDesc); ok {
			if e, found := FindError(v); found {
				// Return only reason and message from mapped error
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"success": false,
					"reason":  e.Reason,
					"message": e.Message,
				})
			}
		}
		// Fallback to status description if no mapping found
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": cancelResp.StatusDesc,
		})
	}

	// Try to map any numeric code in status description to friendly message
	mapped := map[string]interface{}{"response": cancelResp}
	if v, ok := extractFirstInt(cancelResp.StatusDesc); ok {
		if e, found := FindError(v); found {
			mapped["mapped_error"] = e
		}
	}

	// Insert a separate cancel record linked to sale_id
	userToken := utils.GetUserTokenValue(c)
	cancelDoc := bson.M{
		"_id":        uuid.New().String(),
		"org_id":     orgId,
		"sale_id":    req.SaleId,
		"type":       "cancel_irn",
		"created_at": time.Now(),
		"request":    req,
		"response":   cancelResp,
		"api_log":    apiLog,
		"created_by": userToken.UserId,
	}
	_, _ = einvoiceCollection.InsertOne(context.Background(), cancelDoc)
	cancelRsn := ""
	if req.CnlRsn == "" {
		cancelRsn = "No reason provided"
	} else {
		cancelRsn = req.CnlRsn
	}
	// Update invoice record with cancellation details if successful
	if isSuccessStatus(cancelResp.StatusCode) {
		cancelDetails := bson.M{
			"cancelled_at":   time.Now(),
			"cancel_date":    cancelResp.Data.CancelDate,
			"cancel_reason":  cancelRsn,
			"cancel_remarks": req.CnlRem,
		}

		_, _ = einvoiceCollection.UpdateOne(
			context.Background(),
			bson.M{"sale_id": req.SaleId, "type": "einvoice"},
			bson.M{"$set": bson.M{
				"cancelled":      true,
				"cancel_details": cancelDetails,
				"status":         "cancelled",
				"updated_at":     time.Now(),
				"updated_by":     userToken.UserId,
			}},
		)

		// Reverse sale and invoice data (includes S3 deletion)
		if err := reverseSaleAndInvoiceData(db, req.SaleId, "invoice", userToken); err != nil {
			log.Printf("Warning: Failed to reverse sale and invoice data: %v", err)
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "IRN cancelled successfully",
	})
}

// CancelEwayBillHandler cancels an e-way bill
func CancelEwayBillHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	if orgId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "OrgId header required"})
	}

	var req CancelEwayBillRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid request body", "error": err.Error()})
	}

	// Validate required fields from request body
	if req.SaleId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sale_id is required in request body"})
	}

	if req.FactoryId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "factory_id is required in request body"})
	}

	// Get cancel time limit from environment variable (default 24 hours)
	cancelTimeLimitHours := getEnvInt("EINVOICE_CANCEL_EWAYBILL_TIME_LIMIT_HOURS", 24)

	// check invoice record and ewaybill details
	db := database.GetConnection(orgId)
	einvoiceCollection := db.Collection("invoice")
	var invoiceDoc bson.M
	if err := einvoiceCollection.FindOne(context.Background(), bson.M{"sale_id": req.SaleId}).Decode(&invoiceDoc); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invoice record not found for sale_id"})
	}

	// Check if e-way bill exists
	ewaybillData, ok := invoiceDoc["ewaybill"].(bson.M)
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "E-way bill not found for this sale"})
	}

	// Get e-way bill number
	var ewbNo int64
	switch v := ewaybillData["no"].(type) {
	case int64:
		ewbNo = v
	case int32:
		ewbNo = int64(v)
	case int:
		ewbNo = int64(v)
	case float64:
		ewbNo = int64(v)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Invalid e-way bill number format in database",
		})
	}

	if ewbNo == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "E-way bill number is 0 or not found",
		})
	}

	// Set e-way bill number in request
	req.EwbNo = ewbNo

	// Check if already cancelled
	if cancelled, ok := ewaybillData["cancelled"].(bool); ok && cancelled {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"success": false, "message": "E-way bill already cancelled"})
	}

	// Get e-way bill generation time
	var generatedAt time.Time
	switch v := ewaybillData["generated_at"].(type) {
	case time.Time:
		generatedAt = v
	case primitive.DateTime:
		generatedAt = v.Time()
	default:
		generatedAt = time.Now()
	}

	timeSinceGeneration := time.Since(generatedAt)
	if timeSinceGeneration > time.Duration(cancelTimeLimitHours)*time.Hour {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success":                false,
			"message":                fmt.Sprintf("Cancel not allowed after %d hours of e-way bill generation", cancelTimeLimitHours),
			"hours_since_generation": int(timeSinceGeneration.Hours()),
		})
	}

	// Fetch factory details from factory table
	var factory bson.M
	err := db.Collection("factory").FindOne(context.Background(), bson.M{"_id": req.FactoryId}).Decode(&factory)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Factory not found",
			"error":   err.Error(),
		})
	}

	// Extract GSTIN from factory
	sellerGstin, _ := factory["gst_no"].(string)
	if sellerGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "GST number not found in factory record",
		})
	}

	// Read e-way bill credentials from environment variables
	email := os.Getenv("EWAYBILL_EMAIL")
	username := os.Getenv("EWAYBILL_USERNAME")
	password := os.Getenv("EWAYBILL_PASSWORD")
	clientID := os.Getenv("EWAYBILL_CLIENT_ID")
	clientSecret := os.Getenv("EWAYBILL_CLIENT_SECRET")
	ipAddress := os.Getenv("EWAYBILL_IP_ADDRESS")

	// Use default IP if not provided
	if ipAddress == "" {
		ipAddress = "127.0.0.1"
	}

	// Validate required credentials
	if email == "" || username == "" || password == "" || clientID == "" || clientSecret == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Missing required e-way bill credentials in environment variables",
		})
	}

	config := Config{
		EwayBillAuthEndpoint:   os.Getenv("EWAYBILL_AUTH_ENDPOINT"),
		EwayBillCancelEndpoint: os.Getenv("EWAYBILL_CANCEL_ENDPOINT"),
		Email:                  email,
		Username:               username,
		Password:               password,
		IPAddress:              ipAddress,
		ClientID:               clientID,
		ClientSecret:           clientSecret,
		GSTIN:                  sellerGstin,
	}

	client := NewClient(config)
	if err := client.AuthenticateEwayBill(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "authentication failed", "error": err.Error()})
	}

	// Set default values if not provided
	if req.CancelRsn == 0 {
		req.CancelRsn = 2 // Default reason code: 2 = "Transhipment"
		log.Printf("CancelRsn not provided, using default: %d", req.CancelRsn)
	}

	if req.CancelRem == "" {
		req.CancelRem = "No remarks provided"
		log.Printf("CancelRem not provided, using default: %s", req.CancelRem)
	}

	// Store API log
	apiLog := bson.M{
		"timestamp": time.Now(),
		"request":   req,
	}

	cancelResp, err := client.CancelEwayBill(req)
	if err != nil {
		apiLog["success"] = false
		apiLog["error"] = err.Error()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	if !isSuccessStatus(cancelResp.StatusCode) {
		apiLog["success"] = false
		apiLog["response"] = cancelResp

		// Try to map error code to friendly message
		if v, ok := extractFirstInt(cancelResp.StatusDesc); ok {
			if e, found := FindError(v); found {
				// Return only reason and message from mapped error
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"success": false,
					"reason":  e.Reason,
					"message": e.Message,
				})
			}
		}
		// Fallback to status description if no mapping found
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": cancelResp.StatusDesc,
		})
	}

	apiLog["success"] = true
	apiLog["response"] = cancelResp

	// Insert a separate cancel record linked to sale_id
	userToken := utils.GetUserTokenValue(c)
	cancelDoc := bson.M{
		"_id":        uuid.New().String(),
		"org_id":     orgId,
		"sale_id":    req.SaleId,
		"type":       "cancel_ewaybill",
		"created_at": time.Now(),
		"request":    req,
		"response":   cancelResp,
		"api_log":    apiLog,
		"created_by": userToken.UserId,
	}
	_, _ = einvoiceCollection.InsertOne(context.Background(), cancelDoc)

	// Update invoice record with e-way bill cancellation details
	cancelDetails := bson.M{
		"cancelled_at":   time.Now(),
		"cancel_date":    cancelResp.Data.CancelDate,
		"cancel_reason":  req.CancelRsn,
		"cancel_remarks": req.CancelRem,
		"cancelled":      true,
	}

	_, _ = einvoiceCollection.UpdateOne(
		context.Background(),
		bson.M{"sale_id": req.SaleId},
		bson.M{"$set": bson.M{
			"ewaybill":   cancelDetails,
			"updated_at": time.Now(),
			"updated_by": userToken.UserId,
		}},
	)

	// Reverse sale and invoice data (includes S3 deletion if applicable)
	if err := reverseSaleAndInvoiceData(db, req.SaleId, "ewaybill", userToken); err != nil {
		log.Printf("Warning: Failed to reverse sale and invoice data: %v", err)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "E-way bill cancelled successfully",
	})
}

// formatDateForAPI converts MongoDB date to DD/MM/YYYY format (for API calls)
func formatDateForAPI(dateInterface interface{}) string {
	if dateInterface == nil {
		return time.Now().Format("02/01/2006")
	}

	switch v := dateInterface.(type) {
	case time.Time:
		return v.Format("02/01/2006")
	case primitive.DateTime:
		return v.Time().Format("02/01/2006")
	case string:
		// Try to parse and reformat if it's in a different format
		if t, err := time.Parse("02-01-2006", v); err == nil {
			return t.Format("02/01/2006")
		}
		return v
	default:
		return time.Now().Format("02/01/2006")
	}
}

// getFloat64 safely extracts float64 from interface{}
func getFloat64(val interface{}) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0.0
	}
}

// calculateTax calculates tax amount based on amount and tax rate
func calculateTax(amount, taxRate interface{}) float64 {
	var amt, rate float64

	switch v := amount.(type) {
	case float64:
		amt = v
	case int:
		amt = float64(v)
	default:
		return 0.0
	}

	switch v := taxRate.(type) {
	case float64:
		rate = v
	case int:
		rate = float64(v)
	default:
		return 0.0
	}

	result := amt * (rate / 100) / 2 // Divided by 2 because CGST and SGST are each half
	return roundTo2Decimals(result)
}

// addTaxToAmount adds tax to amount
func addTaxToAmount(amount, taxRate interface{}) float64 {
	var amt, rate float64

	switch v := amount.(type) {
	case float64:
		amt = v
	case int:
		amt = float64(v)
	default:
		return 0.0
	}

	switch v := taxRate.(type) {
	case float64:
		rate = v
	case int:
		rate = float64(v)
	default:
		return 0.0
	}

	result := amt * (1 + rate/100)
	return roundTo2Decimals(result)
}

// calculateUnitPrice calculates unit price from total price and quantity
func calculateUnitPrice(totalPrice, quantity interface{}) float64 {
	var total, qty float64

	switch v := totalPrice.(type) {
	case float64:
		total = v
	case int:
		total = float64(v)
	default:
		return 0.0
	}

	switch v := quantity.(type) {
	case float64:
		qty = v
	case int:
		qty = float64(v)
	default:
		return 0.0
	}

	if qty == 0 {
		return 0.0
	}
	result := total / qty
	return roundTo2Decimals(result)
}

// roundTo2Decimals rounds a float64 to 2 decimal places
func roundTo2Decimals(val float64) float64 {
	return math.Round(val*100) / 100
}

func DownloadEInvoiceHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	if orgId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "OrgId header is required",
		})
	}

	saleId := c.Query("sale_id")
	if saleId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "sale_id query parameter is required",
		})
	}

	// Get MongoDB connection
	db := database.GetConnection(orgId)
	saleCollection := db.Collection("invoice")

	// Fetch sale record to get the invoice_url
	var invoiceDoc bson.M
	err := saleCollection.FindOne(context.Background(), bson.M{"sale_id": saleId}).Decode(&invoiceDoc)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"success": false,
			"message": "Sale record not found",
			"error":   err.Error(),
		})
	}
	// Get the S3 URL from top level if exists
	qrURL := ""
	einvoiceData, _ := invoiceDoc["einvoice"]
	if ev, ok := invoiceDoc["einvoice"].(bson.M); ok {
		if val, ok := ev["url"].(string); ok && val != "" {
			qrURL = val
		}
	}

	if qrURL == "" {
		if val, ok := invoiceDoc["report_url"].(string); ok {
			qrURL = val
		}
	}

	// Return the download information
	return c.JSON(fiber.Map{
		"success": true,
		"message": "E-invoice details retrieved successfully",
		"data": fiber.Map{
			"sale_id":     saleId,
			"invoice_url": qrURL,
			"einvoice":    einvoiceData,
		},
	})
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GetDataById(orgID string, ID string, collectionName string) (map[string]interface{}, error) {
	var data map[string]interface{}
	err := database.GetConnection(orgID).Collection(collectionName).FindOne(context.Background(), bson.M{"_id": ID}).Decode(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// generateInvoiceReport generates a PDF/PNG report for an invoice using a template
func generateInvoiceReport(orgId, saleId, templateId, userId, invoiceType string) (string, error) {
	// Prepare params for the template
	params := map[string]interface{}{
		"sale_id":      saleId,
		"invoice_type": invoiceType,
	}

	// Call the PDF export handler internally
	nodePDFURL := os.Getenv("NODE_PDF_SERVICE_URL")
	if nodePDFURL == "" {
		nodePDFURL = "http://localhost:3002/api/pdf/export"
	}

	// Fetch template from MongoDB
	db := database.GetConnection(orgId)
	var template map[string]interface{}
	err := db.Collection("pdf-templates").FindOne(
		context.Background(),
		bson.M{"_id": templateId},
	).Decode(&template)
	if err != nil {
		return "", fmt.Errorf("template not found: %w", err)
	}

	// Update template with params (using exported function from helper package)
	helper.UpdateTemplateParams(template, params, orgId)

	// Clean template for export (using exported function from helper package)
	cleanedTemplate := helper.CleanTemplateForExport(template)

	// Call Node.js PDF service (using exported function from helper package)
	pdfBytes, err := helper.CallNodePDFService(nodePDFURL, cleanedTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to generate PDF: %w", err)
	}

	// Upload to S3
	fileName := fmt.Sprintf("invoice_report_%s_%s_%d.pdf", invoiceType, saleId, time.Now().Unix())
	s3URL, err := helper.UploadGeneratedFile(pdfBytes, fileName, orgId, userId)
	if err != nil {
		return "", fmt.Errorf("failed to upload report to S3: %w", err)
	}

	log.Printf("Successfully generated and uploaded invoice report: %s", s3URL)
	return s3URL, nil
}

// generateIRNForSaleWithLog handles IRN generation for a sale and returns request/response for logging
func generateIRNForSaleWithLog(client *Client, config Config, saleData, invoiceData map[string]interface{}, gstin string, db *mongo.Database, factory bson.M) (*GenerateIRNResponse, interface{}, error) {
	// Extract seller details from factory
	sellerGstin, _ := factory["gst_no"].(string)
	sellerName, _ := factory["factory_name"].(string)
	sellerAddr1, _ := factory["registered_street"].(string)
	sellerLoc, _ := factory["registered_city"].(string)
	sellerPincode, _ := factory["registered_pincode"].(string)

	// Validate required fields
	if sellerGstin == "" {
		return nil, nil, fmt.Errorf("GST number not found in factory record")
	}
	if sellerName == "" {
		sellerName = "Seller Name"
	}
	if sellerAddr1 == "" {
		sellerAddr1 = "Address Line 1"
	}
	if sellerLoc == "" {
		sellerLoc = "City"
	}

	// Get state code from GSTIN (first 2 digits)
	sellerStcd := sellerGstin[:2]

	// Convert pincode to interface{} for API
	var sellerPin interface{}
	if sellerPincode != "" {
		if pinInt, err := strconv.Atoi(sellerPincode); err == nil {
			sellerPin = pinInt
		} else {
			sellerPin = sellerPincode
		}
	} else {
		sellerPin = 560001 // Default
	}

	// Get transport distance from sale table
	var transportDistance float64
	if existingDistance, ok := saleData["transport_distance"].(float64); ok && existingDistance > 0 {
		transportDistance = existingDistance
		log.Printf("Using transport_distance from sale table: %.2f KM", transportDistance)
	} else {
		transportDistance = 100.0 // Default fallback
		log.Printf("transport_distance not found in sale table, using default: %.2f KM", transportDistance)
	}

	// Store distance in invoice_data for later retrieval
	if saleDetails, ok := invoiceData["sale_details"].(fiber.Map); ok {
		saleDetails["transport_distance"] = transportDistance
	} else if saleDetails, ok := invoiceData["sale_details"].(map[string]interface{}); ok {
		saleDetails["transport_distance"] = transportDistance
	}

	pincodeFloat := invoiceData["pincode"].(string)
	pincode, _ := strconv.ParseFloat(pincodeFloat, 64)

	// Extract buyer state code from GSTIN (first 2 digits)
	buyerStateCode := gstin[:2]
	isInterState := sellerStcd != buyerStateCode

	// Set Pos to buyer's state code (required)
	posCode := buyerStateCode

	// Extract base price from total_price (which includes tax)
	totalPriceWithTax := getFloat64(saleData["total_price"])
	gstRate := getFloat64(saleData["gst"])

	// Formula: basePrice = totalPrice / (1 + gstRate/100)
	basePrice := roundTo2Decimals(totalPriceWithTax / (1 + gstRate/100))

	// Calculate tax amounts based on base price
	var igstAmt, cgstAmt, sgstAmt float64
	if isInterState {
		// Inter-state: Use IGST only
		igstAmt = roundTo2Decimals(basePrice * gstRate / 100)
		cgstAmt = 0.0
		sgstAmt = 0.0
	} else {
		// Intra-state: Use CGST + SGST
		igstAmt = 0.0
		cgstAmt = calculateTax(basePrice, gstRate)
		sgstAmt = calculateTax(basePrice, gstRate)
	}

	// Dynamic HSN code and product details lookup
	hsnCode := "08013100"
	prdDesc := "Product"
	unit := "KGS"

	typeOfSale, _ := saleData["type_of_sale"].(string)

	var lookupProductId string
	if typeOfSale != "kernel" {
		lookupProductId = "RCN"
	} else {
		var sp_info bson.M
		if err := db.Collection("sold_products_info").FindOne(context.Background(), bson.M{"pdf_template_id": saleData["_id"]}).Decode(&sp_info); err == nil {
			if pid, ok := sp_info["product_id"].(string); ok {
				lookupProductId = pid
			}
			if totalPrice, ok := sp_info["total_price"].(float64); ok {
				saleData["total_price"] = totalPrice
			}
			if quantity, ok := sp_info["total_quantity"].(float64); ok {
				saleData["quantity"] = quantity
			}
			saleData["gst"] = 0
		}
	}

	if lookupProductId != "" {
		var prod bson.M
		if err := db.Collection("product").FindOne(context.Background(), bson.M{"_id": lookupProductId}).Decode(&prod); err == nil {
			if h, ok := prod["hsn_code"].(string); ok && h != "" {
				hsnCode = h
			}
			if name, ok := prod["product_name"].(string); ok && name != "" {
				prdDesc = name
			}
			if u, ok := prod["unit"].(string); ok && u != "" {
				unit = u
			}
		}
	}

	// Build GenerateIRNRequest using only mandatory fields
	generateIRN := GenerateIRNRequest{
		Version: "1.1",
		TranDtls: TranDtls{
			TaxSch: "GST",
			SupTyp: "B2B",
		},
		DocDtls: DocDtls{
			Typ: "INV",
			No:  fmt.Sprintf("%v", saleData["_id"]),
			Dt:  formatDateForAPI(saleData["sold_on"]),
		},
		SellerDtls: SellerDtls{
			Gstin: sellerGstin,
			LglNm: sellerName,
			Addr1: sellerAddr1,
			Loc:   sellerLoc,
			Pin:   sellerPin,
			Stcd:  sellerStcd,
		},
		BuyerDtls: BuyerDtls{
			Gstin: gstin,
			LglNm: fmt.Sprintf("%v", invoiceData["customer_name"]),
			Pos:   posCode,
			Addr1: fmt.Sprintf("%v", invoiceData["address"]),
			Loc:   fmt.Sprintf("%v", invoiceData["city"]),
			Pin:   pincode,
			Stcd:  buyerStateCode,
		},
		ItemList: []Item{
			{
				SlNo:       "1",
				PrdDesc:    prdDesc,
				IsServc:    "N",
				HsnCd:      hsnCode,
				Qty:        saleData["quantity"],
				Unit:       unit,
				UnitPrice:  calculateUnitPrice(basePrice, saleData["quantity"]),
				TotAmt:     basePrice,
				AssAmt:     basePrice,
				GstRt:      saleData["gst"],
				IgstAmt:    igstAmt,
				CgstAmt:    cgstAmt,
				SgstAmt:    sgstAmt,
				TotItemVal: totalPriceWithTax,
			},
		},
		ValDtls: ValDtls{
			AssVal:    basePrice,
			CgstVal:   cgstAmt,
			SgstVal:   sgstAmt,
			IgstVal:   igstAmt,
			TotInvVal: totalPriceWithTax,
		},
	}

	generateIRNResponse, err := client.GenerateIRN(generateIRN)
	if err != nil {
		return nil, generateIRN, err
	}

	if !isSuccessStatus(generateIRNResponse.StatusCode) {
		return nil, generateIRN, fmt.Errorf("Generate IRN API error: %s", generateIRNResponse.StatusDesc)
	}

	return generateIRNResponse, generateIRN, nil
}

// finalizeEInvoice updates sale table and uploads QR code after IRN generation succeeds
func finalizeEInvoice(c *fiber.Ctx, db *mongo.Database, orgId, saleId string, einvoiceDoc bson.M, irnResponse *GenerateIRNResponse, templateId string) error {
	userToken := utils.GetUserTokenValue(c)

	if irnResponse == nil {
		return fmt.Errorf("IRN response not available")
	}

	qrCodeData := irnResponse.Data.SignedQRCode
	if qrCodeData == "" {
		qrCodeData = irnResponse.Data.Irn
	}

	var s3URL string
	if qrCodeData != "" {
		qrImage, err := qrcode.Encode(qrCodeData, qrcode.Medium, 256)
		if err == nil {
			imageFileName := fmt.Sprintf("einvoice_qr_%s_%d.png", saleId, time.Now().Unix())
			s3URL, err = helper.UploadGeneratedFile(qrImage, imageFileName, orgId, userToken.UserId)
			if err != nil {
				log.Printf("Warning: Failed to upload QR code: %v", err)
			}
		}
	}

	irnForDB := irnResponse.Data.Irn
	ackNo := fmt.Sprintf("%d", irnResponse.Data.AckNo)

	irnDetails := bson.M{
		"irn":           irnForDB,
		"ack_no":        ackNo,
		"url":           s3URL,
		"generated_at":  time.Now(),
		"signedqrcode":  irnResponse.Data.SignedQRCode,
		"signedinvoice": irnResponse.Data.SignedInvoice,
	}

	// Update the invoice record with IRN details FIRST before generating report
	_, err := db.Collection("invoice").UpdateOne(
		context.Background(),
		bson.M{"sale_id": saleId, "type": "einvoice"},
		bson.M{"$set": bson.M{"einvoice": irnDetails}},
	)
	if err != nil {
		log.Printf("Warning: Failed to update invoice record with IRN details: %v", err)
	}

	// Check if we need to generate PDF/PNG report
	invoiceFormat := os.Getenv("INVOICE_FORMAT")

	if invoiceFormat == "report" && templateId != "" {
		// Add delay to ensure IRN details are fully committed to database
		time.Sleep(500 * time.Millisecond)

		log.Printf("Generating e-invoice report for sale_id: %s using template: %s", saleId, templateId)
		reportURL, err := generateInvoiceReport(orgId, saleId, templateId, userToken.UserId, "einvoice")
		if err != nil {
			log.Printf("Warning: Failed to generate e-invoice report: %v", err)
		} else {
			irnDetails["report_url"] = reportURL
			// Also update sale table with report URL
			_, _ = db.Collection("sale").UpdateOne(
				context.Background(),
				bson.M{"_id": saleId},
				bson.M{"$set": bson.M{"report_url": reportURL}},
			)
		}
	}

	// Get transport distance from invoice_data if available
	var transportDistance float64
	if invoiceData, ok := einvoiceDoc["invoice_data"].(map[string]interface{}); ok {
		if saleDetails, ok := invoiceData["sale_details"].(map[string]interface{}); ok {
			if dist, ok := saleDetails["transport_distance"].(float64); ok {
				transportDistance = dist
			}
		}
	}

	// Update sale table only after all steps are successful
	saleUpdate := bson.M{
		"$set": bson.M{
			"invoice_generated": true,
			"einvoice_number":   irnForDB,
			"invoice_type":      "einvoice",
			"invoice_id":        einvoiceDoc["_id"],
			"updated_by":        userToken.UserId,
			"updated_on":        time.Now(),
		},
	}

	// Add transport_distance if calculated
	if transportDistance > 0 {
		saleUpdate["$set"].(bson.M)["transport_distance"] = transportDistance
		log.Printf("Storing transport_distance: %.2f KM for sale_id: %s", transportDistance, saleId)
	}

	_, err = db.Collection("sale").UpdateOne(context.Background(), bson.M{"_id": saleId}, saleUpdate)
	if err != nil {
		return fmt.Errorf("failed to update sale table: %v", err)
	}

	// Update the invoice record with final IRN details (including report_url if generated)
	_, err = db.Collection("invoice").UpdateOne(
		context.Background(),
		bson.M{"sale_id": saleId, "type": "einvoice"},
		bson.M{"$set": bson.M{"einvoice": irnDetails}},
	)
	if err != nil {
		return fmt.Errorf("failed to update invoice record with final details: %v", err)
	}

	return nil
}
