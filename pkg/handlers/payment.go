package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/models"
)

// CashfreePG base URLs
const (
	CashfreeSandboxUrl    = "https://sandbox.cashfree.com/pg/orders"
	CashfreeProductionUrl = "https://api.cashfree.com/pg/orders"
)

type CashfreeOrderRequest struct {
	OrderAmount     float64         `json:"order_amount"`
	OrderCurrency   string          `json:"order_currency"`
	CustomerID      string          `json:"order_id"` // generated Order ID
	CustomerDetails CustomerDetails `json:"customer_details"`
}

type CustomerDetails struct {
	CustomerID    string `json:"customer_id"`
	CustomerPhone string `json:"customer_phone"`
	CustomerEmail string `json:"customer_email"`
}

// CreatePaymentOrder creates a new Cashfree PG order
func CreatePaymentOrder(c *fiber.Ctx) error {
	var req struct {
		Amount   float64 `json:"amount"`
		UserID   string  `json:"userId"`
		Phone    string  `json:"phone"`
		Email    string  `json:"email"`
		Currency string  `json:"currency"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if req.Currency == "" {
		req.Currency = "INR"
	}

	orderId := fmt.Sprintf("order_%s_%d", req.UserID, time.Now().Unix())

	appId := os.Getenv("CASHFREE_APP_ID")
	secretKey := os.Getenv("CASHFREE_SECRET_KEY")
	cashfreeEnv := os.Getenv("CASHFREE_ENV")

	// If credentials are not set, return a mock response for sandbox testing
	if appId == "" || secretKey == "" {
		mockSessionId := "session_" + GenerateUniqueKey()
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status": 200,
			"response": fiber.Map{
				"orderId":          orderId,
				"paymentSessionId": mockSessionId,
			},
		})
	}

	// Make request to Cashfree
	url := CashfreeSandboxUrl
	if strings.ToLower(cashfreeEnv) == "production" {
		url = CashfreeProductionUrl
	}

	bodyData := CashfreeOrderRequest{
		OrderAmount:   req.Amount,
		OrderCurrency: req.Currency,
		CustomerID:    orderId,
		CustomerDetails: CustomerDetails{
			CustomerID:    req.UserID,
			CustomerPhone: req.Phone,
			CustomerEmail: req.Email,
		},
	}

	jsonBytes, _ := json.Marshal(bodyData)
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-version", "2023-08-01")
	httpReq.Header.Set("x-client-id", appId)
	httpReq.Header.Set("x-client-secret", secretKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to connect to Cashfree: " + err.Error()})
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return c.Status(resp.StatusCode).JSON(fiber.Map{
			"error":   "Cashfree error response",
			"details": string(respBytes),
		})
	}

	var cashfreeResp map[string]interface{}
	if err := json.Unmarshal(respBytes, &cashfreeResp); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to parse Cashfree response"})
	}

	sessionId, _ := cashfreeResp["payment_session_id"].(string)
	retOrderId, _ := cashfreeResp["order_id"].(string)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"response": fiber.Map{
			"orderId":          retOrderId,
			"paymentSessionId": sessionId,
		},
	})
}

// VerifyPaymentOrder verifies Cashfree order status and credits points
func VerifyPaymentOrder(c *fiber.Ctx) error {
	orderId := c.Params("orderId")
	if orderId == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "orderId parameter is required"})
	}

	appId := os.Getenv("CASHFREE_APP_ID")
	secretKey := os.Getenv("CASHFREE_SECRET_KEY")
	cashfreeEnv := os.Getenv("CASHFREE_ENV")

	db := config.GetDB()
	ctx := context.Background()

	// Prevent double crediting
	count, _ := db.Collection("wallet_txn").CountDocuments(ctx, bson.M{"ref_id": orderId})
	if count > 0 {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  200,
			"message": "Payment verified (already processed)",
		})
	}

	// Default to success if mock credentials
	if appId == "" || secretKey == "" {
		// Mock logic: Extract userID from orderId format (order_userID_timestamp)
		parts := strings.Split(orderId, "_")
		userId := ""
		if len(parts) >= 2 {
			userId = parts[1]
		}

		if userId != "" {
			var user models.User
			err := db.Collection("users").FindOne(ctx, bson.M{"_id": userId}).Decode(&user)
			if err == nil {
				// Credit default mock points (e.g. 500 points for mock orders)
				points := int32(500)
				opening := user.Points
				closing := user.Points + points

				db.Collection("users").UpdateOne(ctx, bson.M{"_id": userId}, bson.M{"$inc": bson.M{"points": points}})
				db.Collection("wallet_txn").InsertOne(ctx, models.WalletTxn{
					ID:             GenerateUniqueKey(),
					UserID:         userId,
					Description:    "Mock Cashfree Payment",
					RefID:          orderId,
					OpeningBalance: opening,
					ClosingBalance: closing,
					Type:           "CR",
					Amount:         points,
					CreatedOn:      time.Now().UTC(),
				})
			}
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  200,
			"message": "Mock payment verification successful",
		})
	}

	url := fmt.Sprintf("%s/%s", CashfreeSandboxUrl, orderId)
	if strings.ToLower(cashfreeEnv) == "production" {
		url = fmt.Sprintf("%s/%s", CashfreeProductionUrl, orderId)
	}

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	httpReq.Header.Set("x-api-version", "2023-08-01")
	httpReq.Header.Set("x-client-id", appId)
	httpReq.Header.Set("x-client-secret", secretKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to connect to Cashfree: " + err.Error()})
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	var cashfreeResp map[string]interface{}
	if err := json.Unmarshal(respBytes, &cashfreeResp); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to parse Cashfree response"})
	}

	orderStatus, _ := cashfreeResp["order_status"].(string)
	if orderStatus != "PAID" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  400,
			"message": "Payment not completed, current status: " + orderStatus,
		})
	}

	// Calculate and credit points
	var amount float64
	if amtVal, ok := cashfreeResp["order_amount"]; ok {
		switch a := amtVal.(type) {
		case float64:
			amount = a
		case float32:
			amount = float64(a)
		}
	}

	customerDetails, _ := cashfreeResp["customer_details"].(map[string]interface{})
	userId, _ := customerDetails["customer_id"].(string)

	if userId != "" && amount > 0 {
		var user models.User
		err := db.Collection("users").FindOne(ctx, bson.M{"_id": userId}).Decode(&user)
		if err == nil {
			settings := loadSettings(ctx, db.Collection("settings"))
			points := int32(amount) * settings.PointRatio
			if settings.MoneyRatio > 0 {
				points = points / settings.MoneyRatio
			}

			opening := user.Points
			closing := user.Points + points

			db.Collection("users").UpdateOne(ctx, bson.M{"_id": userId}, bson.M{"$inc": bson.M{"points": points}})
			db.Collection("wallet_txn").InsertOne(ctx, models.WalletTxn{
				ID:             GenerateUniqueKey(),
				UserID:         userId,
				Description:    "Cashfree Payment PG",
				RefID:          orderId,
				OpeningBalance: opening,
				ClosingBalance: closing,
				Type:           "CR",
				Amount:         points,
				CreatedOn:      time.Now().UTC(),
			})
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  200,
		"message": "Payment verified and credited successfully",
	})
}
