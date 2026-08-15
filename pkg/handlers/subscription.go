package handlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/middleware"
	"hema-fruits-go/pkg/models"
)

// Helper to convert interface to int32 safely
func getInt32(v interface{}) int32 {
	switch val := v.(type) {
	case int:
		return int32(val)
	case int32:
		return val
	case int64:
		return int32(val)
	case float64:
		return int32(val)
	default:
		return 0
	}
}

// loadSettings loads Settings from database or returns defaults
func loadSettings(ctx context.Context, col *mongo.Collection) models.Settings {
	var settings models.Settings
	err := col.FindOne(ctx, bson.M{}).Decode(&settings)
	if err != nil {
		// Return default settings
		return models.Settings{
			ID:                     "default",
			PointRatio:             10,
			MoneyRatio:             1,
			PostDetectionPoint:     10,
			EnquiresDetectionPoint: 5,
			SetReward:              true,
			RewardPoint:            100,
		}
	}
	return settings
}

// UpdateCreditPoint handles manually crediting points to a user
func UpdateCreditPoint(c *fiber.Ctx) error {
	var payload struct {
		UserID string `json:"userId"`
		Amount int32  `json:"amount"` // Money amount
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payload"})
	}

	if payload.UserID == "" || payload.Amount <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "User ID and valid amount required"})
	}

	db := config.GetDB()
	ctx := context.Background()

	// Load user points
	var user models.User
	err := db.Collection("users").FindOne(ctx, bson.M{"_id": payload.UserID}).Decode(&user)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	settings := loadSettings(ctx, db.Collection("settings"))

	// Calculate earned points: points = amount * PointRatio / MoneyRatio
	points := payload.Amount * settings.PointRatio
	if settings.MoneyRatio > 0 {
		points = points / settings.MoneyRatio
	}

	openingBalance := user.Points
	closingBalance := user.Points + points

	// Update user points
	_, err = db.Collection("users").UpdateOne(
		ctx,
		bson.M{"_id": payload.UserID},
		bson.M{"$inc": bson.M{"points": points}},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Log transaction
	txn := models.WalletTxn{
		ID:             GenerateUniqueKey(),
		UserID:         payload.UserID,
		Description:    "Credit point purchase",
		RefID:          "",
		OpeningBalance: openingBalance,
		ClosingBalance: closingBalance,
		Type:           "CR",
		Amount:         points,
		CreatedOn:      time.Now().UTC(),
	}
	db.Collection("wallet_txn").InsertOne(ctx, txn)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  200,
		"message": "Points credited successfully",
		"userId":  payload.UserID,
		"amount":  points,
	})
}

// PostQuotaHandler checks/deducts points and inserts requirement/stock post
func PostQuotaHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	collectionName := c.Params("collection") // "requirements", "stocks", "quotes", "stock_quotes"
	quotaType := c.Params("type")            // "posts" or "enquiries"

	var inputData bson.M
	if err := c.BodyParser(&inputData); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if inputData == nil {
		inputData = bson.M{}
	}

	db := config.GetDB()
	ctx := context.Background()

	// 1. Get user and verify points balance
	var user models.User
	err := db.Collection("users").FindOne(ctx, bson.M{"_id": userToken.UserId}).Decode(&user)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "User not found"})
	}

	settings := loadSettings(ctx, db.Collection("settings"))

	var deductionPoints int32
	if quotaType == "posts" {
		deductionPoints = settings.PostDetectionPoint
	} else {
		deductionPoints = settings.EnquiresDetectionPoint
	}

	if user.Points < deductionPoints {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Insufficient points. Buy a package."})
	}

	// 2. Insert document
	docId := GenerateUniqueKey()
	inputData["_id"] = docId
	inputData["created_on"] = time.Now().UTC()
	inputData["created_by"] = userToken.UserId
	inputData["post_type"] = collectionName

	if _, ok := inputData["status"]; !ok {
		inputData["status"] = "Active"
	}

	targetCol := "post"
	if collectionName == "quotes" || collectionName == "stock_quotes" {
		targetCol = "response"
	}

	// Default edit limit for requirements or stocks
	if collectionName == "requirements" || collectionName == "stocks" {
		inputData["remainingeditCount"] = 5 // Default limit
		inputData["usereditCount"] = 0
	}

	_, err = db.Collection(targetCol).InsertOne(ctx, inputData)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Insert failed: " + err.Error()})
	}

	// 3. Deduct points and log txn
	openingBalance := user.Points
	closingBalance := user.Points - deductionPoints

	_, err = db.Collection("users").UpdateOne(
		ctx,
		bson.M{"_id": userToken.UserId},
		bson.M{"$inc": bson.M{"points": -deductionPoints}},
	)
	if err == nil {
		txn := models.WalletTxn{
			ID:             GenerateUniqueKey(),
			UserID:         userToken.UserId,
			Description:    "Posted to " + collectionName,
			RefID:          docId,
			OpeningBalance: openingBalance,
			ClosingBalance: closingBalance,
			Type:           "DR",
			Amount:         deductionPoints,
			CreatedOn:      time.Now().UTC(),
		}
		db.Collection("wallet_txn").InsertOne(ctx, txn)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data":   inputData,
	})
}

// UpdateViewFavoriteHandler toggles viewed or favorite status on post/response
func UpdateViewFavoriteHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	collectionName := c.Params("collection") // "requirements", "stocks", "quotes", "stock_quotes"
	actionType := c.Params("type")            // "viewed" or "favorite"
	documentId := c.Params("id")

	targetCol := "post"
	if collectionName == "quotes" || collectionName == "stock_quotes" {
		targetCol = "response"
	}

	db := config.GetDB()
	ctx := context.Background()

	var doc bson.M
	err := db.Collection(targetCol).FindOne(ctx, bson.M{"_id": documentId}).Decode(&doc)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Document not found"})
	}

	var update bson.M
	if actionType == "viewed" {
		// Simply add user ID to viewed set
		update = bson.M{
			"$addToSet": bson.M{"viewed": userToken.UserId},
		}
	} else {
		// Parse favorite payload (status: true/false)
		var body struct {
			Status bool `json:"status"`
		}
		c.BodyParser(&body)

		if body.Status {
			update = bson.M{
				"$addToSet": bson.M{"favorite": userToken.UserId},
			}
		} else {
			update = bson.M{
				"$pull": bson.M{"favorite": userToken.UserId},
			}
		}
	}

	_, err = db.Collection(targetCol).UpdateOne(ctx, bson.M{"_id": documentId}, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  200,
		"message": actionType + " updated successfully",
	})
}

// GiveRewardHandler awards welcome points to referral registrations
func GiveRewardHandler(c *fiber.Ctx) error {
	userID := c.Params("userid")
	db := config.GetDB()
	ctx := context.Background()

	var user models.User
	err := db.Collection("users").FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	settings := loadSettings(ctx, db.Collection("settings"))

	if !settings.SetReward {
		db.Collection("users").UpdateOne(ctx, bson.M{"_id": userID}, bson.M{"$set": bson.M{"isrewardgiven": true}})
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "No reward configured"})
	}

	if user.IsRewardGiven {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Reward already given"})
	}

	openingBalance := user.Points
	closingBalance := user.Points + settings.RewardPoint

	// Apply reward
	_, err = db.Collection("users").UpdateOne(
		ctx,
		bson.M{"_id": userID},
		bson.M{
			"$inc": bson.M{"points": settings.RewardPoint},
			"$set": bson.M{"isrewardgiven": true},
		},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to award points"})
	}

	// Log transaction
	txn := models.WalletTxn{
		ID:             GenerateUniqueKey(),
		UserID:         userID,
		Description:    "Free credit point",
		RefID:          "",
		OpeningBalance: openingBalance,
		ClosingBalance: closingBalance,
		Type:           "CR",
		Amount:         settings.RewardPoint,
		CreatedOn:      time.Now().UTC(),
	}
	db.Collection("wallet_txn").InsertOne(ctx, txn)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Reward awarded successfully"})
}

// UpdateConfirmStatusHandler handles confirming quotes or stock_quotes
func UpdateConfirmStatusHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	collectionName := c.Params("collection") // "quotes" or "stock_quotes"
	id := c.Params("id")

	if collectionName != "quotes" && collectionName != "stock_quotes" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid collection"})
	}

	var payload bson.M
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if payload == nil {
		payload = bson.M{}
	}

	statusRaw, _ := payload["status"].(string)
	status := strings.ToLower(statusRaw)

	db := config.GetDB()
	ctx := context.Background()

	// 1. Fetch response
	var response bson.M
	err := db.Collection("response").FindOne(ctx, bson.M{"_id": id}).Decode(&response)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Response record not found"})
	}

	allowedStatuses := []string{"new", "viewed", "processing"}

	payload["updated_on"] = time.Now().UTC()
	payload["updated_by"] = userToken.UserId
	payload["status"] = status
	payload["post_type"] = collectionName

	// 2. Perform atomic update
	result, err := db.Collection("response").UpdateOne(
		ctx,
		bson.M{
			"_id":    id,
			"status": bson.M{"$in": allowedStatuses},
		},
		bson.M{"$set": payload},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Update failed"})
	}

	if result.ModifiedCount == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Action not allowed. Already processed."})
	}

	// 3. Run quote confirmation logic if status is "confirmed"
	if status == "confirmed" {
		if collectionName == "quotes" {
			err = confirmQuote(ctx, db, response)
		} else {
			err = confirmStockQuote(ctx, db, response)
		}
		if err != nil {
			// Rollback response status
			db.Collection("response").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"status": "new"}})
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Status updated successfully",
	})
}

// confirmQuote increments confirmedKg on the requirement post
func confirmQuote(ctx context.Context, db *mongo.Database, quote bson.M) error {
	reqId, _ := quote["requirementId"].(string)
	quoteId, _ := quote["_id"].(string)
	if reqId == "" || quoteId == "" {
		return errors.New("Invalid requirementId or quoteId")
	}

	supplyQty := getInt32(quote["supplyQtyKg"])

	var requirement bson.M
	err := db.Collection("post").FindOne(ctx, bson.M{"_id": reqId, "post_type": "requirements"}).Decode(&requirement)
	if err != nil {
		return errors.New("Requirement not found")
	}

	// Check if already responded
	if arr, ok := requirement["confirmedUserId"].(primitive.A); ok {
		for _, id := range arr {
			if id == quoteId {
				return errors.New("You have already responded. Please apply another quote.")
			}
		}
	}

	// Increment and close if full
	_, err = db.Collection("post").UpdateOne(
		ctx,
		bson.M{"_id": reqId},
		bson.M{
			"$inc":       bson.M{"confirmedKg": supplyQty},
			"$addToSet":  bson.M{"confirmedUserId": quoteId},
		},
	)
	if err != nil {
		return err
	}

	// Fetch updated requirement
	db.Collection("post").FindOne(ctx, bson.M{"_id": reqId}).Decode(&requirement)
	confirmed := getInt32(requirement["confirmedKg"])
	required := getInt32(requirement["requiredqty"])

	if confirmed >= required {
		db.Collection("post").UpdateOne(ctx, bson.M{"_id": reqId}, bson.M{"$set": bson.M{"status": "closed"}})
	}

	return nil
}

// confirmStockQuote increments confirmedKg on the stock post
func confirmStockQuote(ctx context.Context, db *mongo.Database, quote bson.M) error {
	stockId, _ := quote["stockId"].(string)
	quoteId, _ := quote["_id"].(string)
	if stockId == "" || quoteId == "" {
		return errors.New("Invalid stockId or quoteId")
	}

	qty := getInt32(quote["quantity"])

	var stock bson.M
	err := db.Collection("post").FindOne(ctx, bson.M{"_id": stockId, "post_type": "stocks"}).Decode(&stock)
	if err != nil {
		return errors.New("Stock not found")
	}

	if arr, ok := stock["confirmedUserId"].(primitive.A); ok {
		for _, id := range arr {
			if id == quoteId {
				return errors.New("You have already responded. Please apply another quote.")
			}
		}
	}

	_, err = db.Collection("post").UpdateOne(
		ctx,
		bson.M{"_id": stockId},
		bson.M{
			"$inc":      bson.M{"confirmedKg": qty},
			"$addToSet": bson.M{"confirmedUserId": quoteId},
		},
	)
	if err != nil {
		return err
	}

	db.Collection("post").FindOne(ctx, bson.M{"_id": stockId}).Decode(&stock)
	confirmed := getInt32(stock["confirmedKg"])
	available := getInt32(stock["availableqty"])

	if confirmed >= available {
		db.Collection("post").UpdateOne(ctx, bson.M{"_id": stockId}, bson.M{"$set": bson.M{"status": "closed"}})
	}

	return nil
}

// ThemeGetHandler fetches the single theme configurations
func ThemeGetHandler(c *fiber.Ctx) error {
	collectionName := c.Params("collection")
	if collectionName != "theme" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid collection"})
	}

	db := config.GetDB()
	ctx := context.Background()

	// Theme matches specific static ID
	filter := bson.M{"_id": "69fad8b51405a743ac1ae26a"}

	var result bson.M
	err := db.Collection("theme").FindOne(ctx, filter).Decode(&result)
	if err != nil {
		// Default theme object if not found in db
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"_id": "69fad8b51405a743ac1ae26a",
			"primaryColor": "#FF9800",
			"accentColor": "#FF5722",
			"status": "Active",
		})
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// SettingsHandler handles editing post with remainingeditCount decrement check
func SettingsHandler(c *fiber.Ctx) error {
	collectionName := c.Params("collection")
	id := c.Params("id")
	userToken := middleware.GetUserTokenValue(c)

	db := config.GetDB()
	ctx := context.Background()

	if collectionName == "settings" {
		var inputData bson.M
		if err := c.BodyParser(&inputData); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if inputData == nil {
			inputData = bson.M{}
		}

		inputData["updatedOn"] = time.Now().UTC()
		inputData["updatedBy"] = userToken.UserId

		opts := options.Update().SetUpsert(true)
		_, err := db.Collection("settings").UpdateOne(ctx, bson.M{}, bson.M{"$set": inputData}, opts)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Settings updated"})
	}

	allowedCollections := map[string]bool{
		"requirements": true,
		"stocks":       true,
	}

	if !allowedCollections[collectionName] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid collection name"})
	}

	var updateData bson.M
	if err := c.BodyParser(&updateData); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if updateData == nil {
		updateData = bson.M{}
	}

	// Update document matching edit limits
	filter := bson.M{
		"_id":                id,
		"remainingeditCount": bson.M{"$gt": 0},
	}

	update := bson.M{
		"$set": updateData,
		"$inc": bson.M{
			"usereditCount":      1,
			"remainingeditCount": -1,
		},
	}

	_, err := db.Collection("post").UpdateOne(ctx, filter, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Update failed or limit reached: " + err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Edit count updated and post saved"})
}

// DeleteHandler handles soft deletion of posts/responses
func DeleteHandler(c *fiber.Ctx) error {
	collectionName := c.Params("collection")
	id := c.Params("id")

	db := config.GetDB()
	ctx := context.Background()

	targetCol := "post"
	if collectionName == "quotes" || collectionName == "stock_quotes" {
		targetCol = "response"
	}

	_, err := db.Collection(targetCol).UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"isDelete": true, "deletedAt": time.Now().UTC(), "status": "Inactive"}},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Record soft-deleted successfully",
	})
}
