package einvoice

import (
	"context"
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"kriyatec.com/pms-api/pkg/shared/database"
)

// ValidateGSTRequest represents the request body for GST validation
type ValidateGSTRequest struct {
	GstNo string `json:"gst_no"`
}

// ValidateCustomerRequest represents the request body for customer validation
type ValidateCustomerRequest struct {
	GSTNo     string `json:"gst_no"`
	FactoryID string `json:"factory_id"`
	Email     string `json:"email"`
}

// ValidateGSTNHandler validates a GST number and returns business details
func ValidateGSTNHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	if orgId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Organization ID missing",
		})
	}

	// Parse request body
	var req ValidateGSTRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	gstin := req.GstNo
	if gstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "GSTIN is required",
		})
	}

	// Validate GSTIN format (15 characters)
	if len(gstin) != 15 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Invalid GSTIN format. GSTIN must be 15 characters",
		})
	}

	// Get factory_id from query (optional, for getting seller GSTIN)
	factoryId := c.Query("factory_id")

	var sellerGstin string
	if factoryId != "" {
		// Get factory details to extract seller GSTIN
		db := database.GetConnection(orgId)
		var factory bson.M
		err := db.Collection("config").FindOne(context.Background(), bson.M{"factory_id": factoryId}).Decode(&factory)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"message": "Factory not found",
			})
		}
		sellerGstin, _ = factory["gst_no"].(string)
	}

	// If no seller GSTIN from factory, use a default or the same GSTIN
	if sellerGstin == "" {
		sellerGstin = gstin
	}

	// Read e-invoice credentials from environment variables
	email := os.Getenv("EINVOICE_EMAIL")
	username := os.Getenv("EINVOICE_USERNAME")
	password := os.Getenv("EINVOICE_PASSWORD")
	clientID := os.Getenv("EINVOICE_CLIENT_ID")
	clientSecret := os.Getenv("EINVOICE_CLIENT_SECRET")
	ipAddress := os.Getenv("EINVOICE_IP_ADDRESS")

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

	// Build config
	config := Config{
		AuthEndpoint:        os.Getenv("EINVOICE_AUTH_ENDPOINT"),
		GSTNDetailsEndpoint: os.Getenv("EINVOICE_GSTN_DETAILS_ENDPOINT"),
		Email:               email,
		Username:            username,
		Password:            password,
		IPAddress:           ipAddress,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		GSTIN:               sellerGstin,
	}

	client := NewClient(config)

	// Authenticate
	if err := client.Authenticate(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Authentication failed",
			"error":   err.Error(),
		})
	}

	// Get GSTN details
	gstnDetails, err := client.GetGSTNDetails(gstin)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to fetch GSTN details",
			"error":   err.Error(),
		})
	}

	// Check if the API returned an error
	if gstnDetails.StatusCode != "1" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": gstnDetails.StatusDesc,
			"details": gstnDetails.ErrorDetails,
		})
	}

	// Map GSTN details to factory form fields
	// Helper function to safely convert interface{} to string
	toString := func(v interface{}) string {
		if v == nil {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return fmt.Sprintf("%.0f", val)
		case int:
			return fmt.Sprintf("%d", val)
		default:
			return fmt.Sprintf("%v", val)
		}
	}

	// Build street address from available components
	streetParts := []string{}
	if bnm := toString(gstnDetails.Data.AddrBnm); bnm != "" {
		streetParts = append(streetParts, bnm)
	}
	if st := toString(gstnDetails.Data.AddrSt); st != "" {
		streetParts = append(streetParts, st)
	}
	registeredStreet := ""
	if len(streetParts) > 0 {
		registeredStreet = fmt.Sprintf("%s", streetParts[0])
		if len(streetParts) > 1 {
			registeredStreet = fmt.Sprintf("%s %s", streetParts[0], streetParts[1])
		}
	}

	response := fiber.Map{
		"success": true,
		"message": "GSTN validated successfully",
		"data": fiber.Map{
			"gst_no":               gstnDetails.Data.Gstin,
			"factory_name":         gstnDetails.Data.LegalName,
			"trade_name":           toString(gstnDetails.Data.TradeName),
			"registered_street":    registeredStreet,
			"registered_building":  toString(gstnDetails.Data.AddrBno),
			"registered_floor":     toString(gstnDetails.Data.AddrFlno),
			"registered_area_name": toString(gstnDetails.Data.AddrLoc),
			"registered_pincode":   toString(gstnDetails.Data.AddrPncd),
			"state_code":           fmt.Sprintf("%d", gstnDetails.Data.StateCode),
			"taxpayer_type":        gstnDetails.Data.TxpType,
			"status":               gstnDetails.Data.Status,
			"registration_date":    toString(gstnDetails.Data.DtReg),
			"cancellation_date":    gstnDetails.Data.CancelDt,
		},
	}

	return c.JSON(response)
}

// ValidateCustomerHandler validates a customer's GSTIN using the SYNC_GSTIN_FROMCP API
func ValidateCustomerHandler(c *fiber.Ctx) error {
	orgId := c.Get("OrgId")
	if orgId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Organization ID missing",
		})
	}

	// Parse request body
	var req ValidateCustomerRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"error":   err.Error(),
		})
	}

	gstin := req.GSTNo
	if gstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "gst_no is required",
		})
	}

	// Validate GSTIN format (15 characters)
	if len(gstin) != 15 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Invalid GSTIN format. GSTIN must be 15 characters",
		})
	}

	factoryId := req.FactoryID
	if factoryId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "factory_id is required",
		})
	}

	// Get factory details to extract seller GSTIN
	db := database.GetConnection(orgId)
	var factory bson.M
	err := db.Collection("factory").FindOne(context.Background(), bson.M{"_id": factoryId}).Decode(&factory)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Factory not found",
		})
	}

	factoryGstin, _ := factory["gst_no"].(string)
	if factoryGstin == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": "Factory GSTIN not found",
		})
	}

	email := req.Email
	if email == "" {
		email = os.Getenv("EINVOICE_EMAIL")
	}

	// Read e-invoice credentials from environment variables
	username := os.Getenv("EINVOICE_USERNAME")
	password := os.Getenv("EINVOICE_PASSWORD")
	clientID := os.Getenv("EINVOICE_CLIENT_ID")
	clientSecret := os.Getenv("EINVOICE_CLIENT_SECRET")
	ipAddress := os.Getenv("EINVOICE_IP_ADDRESS")

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

	// Build config with factory GSTIN
	config := Config{
		AuthEndpoint:        os.Getenv("EINVOICE_AUTH_ENDPOINT"),
		GSTNDetailsEndpoint: "https://apisandbox.whitebooks.in/einvoice/type/SYNC_GSTIN_FROMCP/version/V1_03",
		Email:               email,
		Username:            username,
		Password:            password,
		IPAddress:           ipAddress,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		GSTIN:               factoryGstin,
	}

	client := NewClient(config)

	// Authenticate
	if err := client.Authenticate(); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Authentication failed",
			"error":   err.Error(),
		})
	}

	// Get GSTN details using SYNC_GSTIN_FROMCP endpoint
	gstnDetails, err := client.GetGSTNDetails(gstin)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to fetch GSTN details",
			"error":   err.Error(),
		})
	}

	// Check if the API returned an error
	if gstnDetails.StatusCode != "1" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"message": gstnDetails.StatusDesc,
			"details": gstnDetails.ErrorDetails,
		})
	}

	// Helper function to safely convert interface{} to string
	toString := func(v interface{}) string {
		if v == nil {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return fmt.Sprintf("%.0f", val)
		case int:
			return fmt.Sprintf("%d", val)
		default:
			return fmt.Sprintf("%v", val)
		}
	}

	// Build street address from available components
	streetParts := []string{}
	if bnm := toString(gstnDetails.Data.AddrBnm); bnm != "" {
		streetParts = append(streetParts, bnm)
	}
	if st := toString(gstnDetails.Data.AddrSt); st != "" {
		streetParts = append(streetParts, st)
	}
	registeredStreet := ""
	if len(streetParts) > 0 {
		registeredStreet = fmt.Sprintf("%s", streetParts[0])
		if len(streetParts) > 1 {
			registeredStreet = fmt.Sprintf("%s %s", streetParts[0], streetParts[1])
		}
	}

	response := fiber.Map{
		"success": true,
		"message": "Customer GSTIN validated successfully",
		"data": fiber.Map{
			"gstin":                gstnDetails.Data.Gstin,
			"legal_name":           gstnDetails.Data.LegalName,
			"trade_name":           toString(gstnDetails.Data.TradeName),
			"registered_street":    registeredStreet,
			"registered_building":  toString(gstnDetails.Data.AddrBno),
			"registered_floor":     toString(gstnDetails.Data.AddrFlno),
			"registered_area_name": toString(gstnDetails.Data.AddrLoc),
			"registered_pincode":   toString(gstnDetails.Data.AddrPncd),
			"state_code":           fmt.Sprintf("%d", gstnDetails.Data.StateCode),
			"taxpayer_type":        gstnDetails.Data.TxpType,
			"status":               gstnDetails.Data.Status,
			"registration_date":    toString(gstnDetails.Data.DtReg),
			"cancellation_date":    gstnDetails.Data.CancelDt,
		},
	}

	return c.JSON(response)
}
