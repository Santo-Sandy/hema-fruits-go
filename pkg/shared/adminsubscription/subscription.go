package adminsubscription

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
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

func UpdateCreditPoint(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// Parse payload
	var payload struct {
		UserID string `json:"userId"`
		Amount int32  `json:"amount"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return shared.BadRequest("Invalid payload: " + err.Error())
	}

	// Validate
	if payload.UserID == "" {
		return shared.BadRequest("User ID is required")
	}
	if payload.Amount <= 0 {
		return shared.BadRequest("Amount must be greater than 0")
	}

	db := database.GetConnection(org.Id)
	ctx := context.Background()

	// Call CreditUserPoints
	err := CreditUserPoints(
		ctx,
		db,
		payload.UserID,
		payload.Amount,
	)
	if err != nil {
		return shared.BadRequest("Failed to credit points: " + err.Error())
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Points credited successfully",
		"userId":  payload.UserID,
		"amount":  payload.Amount,
	})
}

func GiveRewardHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userID := c.Params("userid")
	db := database.GetConnection(org.Id)
	ctx := context.Background()

	usersCollection := db.Collection("users")
	settingsCollection := db.Collection("settings")

	// Get user
	var user struct {
		ID            string `bson:"_id"`
		Points        int    `bson:"points"`
		IsRewardGiven bool   `bson:"isrewardgiven"`
	}

	err := usersCollection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		return shared.BadRequest("User not found")
	}

	// Get settings
	var settings struct {
		RewardPoint int  `bson:"rewardpoint"`
		SetReward   bool `bson:"setreward"`
	}

	err = settingsCollection.FindOne(ctx, bson.M{}).Decode(&settings)
	if err != nil {
		return shared.BadRequest("Settings not found")
	}

	// If reward disabled → just mark as reward given
	if !settings.SetReward {
		update := bson.M{
			"$set": bson.M{"isrewardgiven": true},
		}

		_, err := usersCollection.UpdateOne(ctx, bson.M{"_id": userID}, update)
		if err != nil {
			return shared.BadRequest("Failed to update reward status")
		}

		return shared.SuccessResponse(c, fiber.Map{
			"message": "No reward",
		})
	}

	// If reward already given
	if user.IsRewardGiven {
		return shared.SuccessResponse(c, fiber.Map{
			"message": "Reward already given",
		})
	}

	// Give reward
	update := bson.M{
		"$inc": bson.M{"points": settings.RewardPoint},
		"$set": bson.M{"isrewardgiven": true},
	}

	_, err = usersCollection.UpdateOne(ctx, bson.M{"_id": userID}, update)
	if err != nil {
		return shared.BadRequest("Failed to update reward")
	}
	walletTxnCollection := db.Collection("wallet_txn")

	openingBalance := user.Points
	closingBalance := user.Points + settings.RewardPoint

	txn := bson.M{
		"_id":             primitive.NewObjectID().Hex(),
		"user_id":         userID,
		"description":     "Free credit point",
		"ref_id":          "",
		"opening_balance": openingBalance,
		"closing_balance": closingBalance,
		"type":            "CR",
		"amount":          settings.RewardPoint,
		"created_on":      time.Now(),
	}

	_, err = walletTxnCollection.InsertOne(ctx, txn)
	if err != nil {
		return shared.BadRequest("Reward given but failed to log transaction")
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message":         "Reward given successfully",
		"credited_points": settings.RewardPoint,
	})

}

func GetEditCountFromSettings(orgID string) int32 {
	db := database.GetConnection(orgID)
	ctx := context.Background()

	var setting struct {
		EditCount int32 `bson:"editCount"`
	}

	err := db.Collection("settings").
		FindOne(ctx, bson.M{}).
		Decode(&setting)

	if err != nil {
		return 0
	}

	return setting.EditCount
}

func PostQuotaHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("collection")
	quotaType := c.Params("type")

	allowedCollections := map[string]bool{
		"requirements": true,
		"stocks":       true,
		"stock_quotes": true,
		"quotes":       true,
	}

	if !allowedCollections[collectionName] {
		return shared.BadRequest("Invalid collection name")
	}

	if quotaType != "posts" && quotaType != "enquiries" {
		return shared.BadRequest("Invalid type")
	}

	var inputData map[string]interface{}
	if err := c.BodyParser(&inputData); err != nil {
		return shared.BadRequest(err.Error())
	}
	inputData["post_type"] = collectionName
	db := database.GetConnection(org.Id)
	ctx := context.Background()

	// Check user quota
	var userData map[string]interface{}
	err := db.Collection("users").FindOne(ctx, bson.M{"_id": userToken.UserId}).Decode(&userData)
	if err != nil {
		return shared.BadRequest("User not found")
	}

	// Get current quota value
	var currentPoints int32
	if val, ok := userData["points"]; ok {
		currentPoints = getInt32(val)
	}

	if currentPoints <= 0 {
		return shared.BadRequest("Buy any package.")
	}
	usageType := UsageType(quotaType)
	// Check the user can able to pay the fee ? if yes it will go else it will not
	err = ChekSufficentPoint(ctx, db, userToken.UserId, usageType)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// Prepare insert data
	helper.UpdateDateObject(inputData)
	helper.HandleIDGeneration(inputData, org.Id, collectionName)

	if _, ok := inputData["status"]; !ok {
		inputData["status"] = "Active"
	}

	inputData["created_on"] = time.Now().UTC()
	inputData["created_by"] = userToken.UserId
	inputData["remainingeditCount"] = GetEditCountFromSettings(org.Id)
	inputData["usereditCount"] = 0
	colName := collectionName
	switch collectionName {
	case "stock_quotes", "quotes":
		colName = "response"
	case "requirements", "stocks":
		colName = "post"
	}
	// Insert document
	_, err = db.Collection(colName).InsertOne(ctx, inputData)
	if err != nil {
		return shared.BadRequest("Insert failed: " + err.Error())
	}

	// Update user quota
	insertedID, _ := inputData["_id"].(string)
	// Verify sufficient balance
	usercol := db.Collection("users")
	settingcol := db.Collection("settings")
	go DeductUserPoints(ctx, usercol, settingcol, db.Collection("wallet_txn"), userToken.UserId, usageType, "Posted to "+collectionName, insertedID)

	switch collectionName {

	case "requirements":

		notificationData := map[string]interface{}{
			"_id":         insertedID,
			"name":        "requirements",
			"yearOfCrop":  inputData["yearOfCrop"],
			"grade":       inputData["grade"],
			"type":        inputData["type"],
			"role":        "processor",
			"requiredqty": inputData["requiredqty"],
			"buyerId":     inputData["buyerId"],
		}

		go sendGroupFcmNotification(
			org.Id,
			notificationData,
		)

	case "stocks":

		grade := "RCN" // default value

		if g, ok := inputData["grade"].(string); ok && g != "" {
			grade = g
		}

		notificationData := map[string]interface{}{
			"_id":          insertedID,
			"name":         "stocks",
			"yearofcrop":   inputData["yearofcrop"],
			"grade":        grade,
			"type":         inputData["type"],
			"role":         "buyer",
			"availableqty": inputData["availableqty"],
			"userId":       inputData["userid"],
		}

		go sendGroupFcmNotification(
			org.Id,
			notificationData,
		)

	case "stock_quotes":

		notificationData := map[string]interface{}{
			"_id":        insertedID,
			"name":       "stock_quotes",
			"buyerId":    inputData["buyerId"],
			"stockId":    inputData["stockId"],
			"role":       "processor",
			"merchantId": inputData["merchantId"],
			"status":     inputData["status"],
		}

		go sendEnquiryFcmNotification(
			org.Id,
			notificationData,
		)

	case "quotes":

		notificationData := map[string]interface{}{
			"_id":           insertedID,
			"name":          "quotes",
			"buyerId":       inputData["buyerId"],
			"requirementId": inputData["requirementId"],
			"role":          "buyer",
			"merchantId":    inputData["merchantId"],
			"status":        inputData["status"],
		}

		go sendEnquiryFcmNotification(
			org.Id,
			notificationData,
		)
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Inserted successfully",
	})
}

func SettingsHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collection")
	id := c.Params("id")
	db := database.GetConnection(org.Id)
	ctx := context.Background()
	userToken := utils.GetUserTokenValue(c)
	now := time.Now()

	// 1. FETCH GLOBAL SETTINGS (The Source of Truth)
	var globalSettings map[string]interface{}
	err := db.Collection("settings").FindOne(ctx, bson.M{}).Decode(&globalSettings)

	currentGlobalLimit := 0
	if err == nil {
		if val, ok := globalSettings["editCount"].(float64); ok {
			currentGlobalLimit = int(val)
		}
	}

	if collectionName == "settings" {
		var inputData map[string]interface{}
		if err := c.BodyParser(&inputData); err != nil {
			return shared.BadRequest(err.Error())
		}

		inputData["updatedOn"] = now
		inputData["updatedBy"] = userToken.UserId

		// Check if settings doc already exists
		var existing map[string]interface{}
		existsErr := db.Collection("settings").FindOne(ctx, bson.M{}).Decode(&existing)

		opts := options.Update().SetUpsert(true)

		if existsErr != nil {
			// FIRST INSERT: store _id as plain string, set createdOn/createdBy
			stringID := primitive.NewObjectID().Hex()
			inputData["createdOn"] = now
			inputData["createdBy"] = userToken.UserId

			_, err := db.Collection("settings").UpdateOne(
				ctx,
				bson.M{"_id": stringID},
				bson.M{
					"$setOnInsert": bson.M{"_id": stringID},
					"$set":         inputData,
				},
				opts,
			)
			if err != nil {
				return shared.BadRequest(err.Error())
			}
		} else {
			// SUBSEQUENT UPDATES: only update fields, never touch createdOn/createdBy/_id
			_, err := db.Collection("settings").UpdateOne(
				ctx,
				bson.M{},
				bson.M{
					"$set": inputData,
				},
				opts,
			)
			if err != nil {
				return shared.BadRequest(err.Error())
			}
		}

		InvalidateSettingsCache()

		// Sync logic for other collections if editCount changed
		if newLimit, ok := inputData["editCount"].(int); ok {
			if currentGlobalLimit != newLimit {
				targets := []string{"requirements", "stocks"}
				for _, t := range targets {
					pipeline := bson.A{
						bson.D{
							{"$set",
								bson.D{
									{"remainingeditCount",
										bson.D{
											{"$subtract",
												bson.A{
													newLimit,
													bson.D{
														{"$ifNull",
															bson.A{
																"$usereditCount",
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
							{"$merge",
								bson.D{
									{"into", t}, // FIX: was hardcoded "requirements", now uses loop variable
									{"on", "_id"},
									{"whenMatched", "merge"},
								},
							},
						},
					}
					db.Collection(t).Aggregate(ctx, pipeline)
				}
			}
		}

		return shared.SuccessResponse(c, fiber.Map{"message": "Settings updated and collections synced"})
	}

	allowedCollections := map[string]bool{
		"requirements": true,
		"stocks":       true,
	}

	if !allowedCollections[collectionName] {
		return shared.BadRequest("Invalid collection name")
	}

	if id == "" {
		return shared.BadRequest("Document id required")
	}

	var updateData map[string]interface{}
	if err := c.BodyParser(&updateData); err != nil {
		return shared.BadRequest("Invalid payload: " + err.Error())
	}
	updateData["post_type"] = collectionName
	delete(updateData, "usereditCount")
	delete(updateData, "remainingeditCount")
	colName := collectionName
	switch collectionName {
	case "stock_quotes", "quotes":
		colName = "response"
	case "requirements", "stocks":
		colName = "posts"
	}
	// _id is stored as plain string, filter directly with string id
	filter := bson.M{
		"_id":                id,
		"remainingeditCount": bson.M{"$gt": 0},
	}

	updatePipeline := mongo.Pipeline{
		{
			{"$set", bson.M(updateData)},
		},
		{
			{"$set", bson.M{
				"usereditCount": bson.M{"$add": bson.A{
					bson.M{"$ifNull": bson.A{"$usereditCount", 0}}, 1},
				},
				"remainingeditCount": bson.M{
					"$subtract": bson.A{
						currentGlobalLimit,
						bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$usereditCount", 0}}, 1}},
					},
				},
				"updatedOn": now,
				"updatedBy": userToken.UserId,
			}},
		},
	}

	res, err := db.Collection(colName).UpdateOne(ctx, filter, updatePipeline)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	if res.MatchedCount == 0 {
		return shared.BadRequest("Update failed: ID not found or remainingeditCount is 0/negative.")
	}

	return shared.SuccessResponse(c, fiber.Map{"message": "Data updated and quota deducted"})
}

func deleteHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collection")
	id := c.Params("id")

	if collectionName == "" || id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"message": "collection and id are required",
		})
	}

	db := database.GetConnection(org.Id)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	targetCol := db.Collection(collectionName)

	result, err := targetCol.UpdateOne(
		ctx,
		bson.M{
			"_id": id,
		},
		bson.M{
			"$set": bson.M{
				"isDelete":  true,
				"deletedAt": time.Now(),
			},
		},
	)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"message": "Failed to delete record",
			"error":   err.Error(),
		})
	}

	if result.MatchedCount == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"message": "Record not found or already deleted",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":    "Record deleted successfully",
		"collection": collectionName,
		"id":         id,
	})
}

func getInt32(val interface{}) int32 {
	switch v := val.(type) {
	case int32:
		return v
	case int:
		return int32(v)
	case float64:
		return int32(v)
	default:
		return 0
	}
}

func UpdateConfirmStatusHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("collection")
	id := c.Params("id")

	if collectionName != "quotes" && collectionName != "stock_quotes" {
		return shared.BadRequest("Invalid collection")
	}
	colName := "response"

	var payload map[string]interface{}
	if err := c.BodyParser(&payload); err != nil {
		return shared.BadRequest(err.Error())
	}

	statusRaw, _ := payload["status"].(string)
	status := strings.ToLower(statusRaw)

	db := database.GetConnection(org.Id)
	ctx := context.Background()

	//  Fetch existing record (for notification + confirm logic)
	var existing map[string]interface{}
	err := db.Collection(colName).
		FindOne(ctx, bson.M{"_id": id}).
		Decode(&existing)
	if err != nil {
		return shared.BadRequest("Record not found")
	}

	//  Define allowed previous statuses
	var allowedStatuses []string

	if collectionName == "quotes" {
		allowedStatuses = []string{"new", "viewed"}
	} else {
		allowedStatuses = []string{"new", "processing"}
	}

	// Prepare update payload
	payload["updated_on"] = time.Now().UTC()
	payload["updated_by"] = userToken.UserId
	payload["status"] = status
	payload["post_type"] = collectionName

	//  Atomic update with status restriction
	result, err := db.Collection(collectionName).UpdateOne(
		ctx,
		bson.M{
			"_id":    id,
			"status": bson.M{"$in": allowedStatuses},
		},
		bson.M{
			"$set": payload,
		},
	)

	if err != nil {
		return shared.BadRequest("Update failed")
	}

	if result.ModifiedCount == 0 {
		return shared.BadRequest("Action not allowed. Already processed.")
	}

	//  Run Confirmation Logic ONLY if confirmed
	if status == "confirmed" {
		switch collectionName {
		case "quotes":
			err = confirmQuote(ctx, db, existing)
		case "stock_quotes":
			err = confirmStockQuote(ctx, db, existing)
		}
		if err != nil {
			return shared.BadRequest(err.Error())
		}
	}

	var role string
	if collectionName == "quotes" {
		role = "buyer"
	} else {
		role = "processor"
	}

	//  Send Notification
	notificationData := map[string]interface{}{
		"_id":        existing["_id"],
		"name":       collectionName,
		"role":       role,
		"status":     status,
		"buyerId":    existing["buyerId"],
		"merchantId": existing["merchantId"],
	}

	if collectionName == "quotes" {
		notificationData["requirementId"] = existing["requirementId"]
	} else {
		notificationData["stockId"] = existing["stockId"]
	}

	go sendEnquiryFcmNotification(org.Id, notificationData)

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Status updated and notification sent",
	})
}

func confirmQuote(ctx context.Context, db *mongo.Database, quote map[string]interface{}) error {

	requirementId, ok1 := quote["requirementId"].(string)
	quoteId, okQuote := quote["_id"].(string)

	if !ok1 || !okQuote {
		return errors.New("Invalid requirementId or quoteId")
	}

	// Extract supplyQtyKg safely
	var supplyQtyKg int32
	switch v := quote["supplyQtyKg"].(type) {
	case int32:
		supplyQtyKg = v
	case float64:
		supplyQtyKg = int32(v)
	default:
		return errors.New("Invalid supplyQtyKg")
	}

	// 1️⃣ Check if quote already exists in requirement
	var requirement map[string]interface{}
	err := db.Collection("requirements").
		FindOne(ctx, bson.M{"_id": requirementId}).
		Decode(&requirement)
	if err != nil {
		return err
	}

	if arr, ok := requirement["confirmedUserId"].(primitive.A); ok {
		for _, id := range arr {
			if id == quoteId {
				return errors.New("You have already responded. Please apply another quote.")
			}
		}
	}

	// 2️⃣ Increment confirmedKg AND push quoteId
	_, err = db.Collection("requirements").UpdateOne(
		ctx,
		bson.M{"_id": requirementId},
		bson.M{
			"$inc": bson.M{"confirmedKg": supplyQtyKg},
			"$addToSet": bson.M{
				"confirmedUserId": quoteId,
			},
		},
	)
	if err != nil {
		return err
	}

	// 3️⃣ Fetch updated requirement
	err = db.Collection("requirements").
		FindOne(ctx, bson.M{"_id": requirementId}).
		Decode(&requirement)
	if err != nil {
		return err
	}

	confirmedKg := getInt32(requirement["confirmedKg"])
	requiredQty := getInt32(requirement["requiredqty"])

	// 4️⃣ Close if fully confirmed
	if confirmedKg >= requiredQty {
		_, err = db.Collection("requirements").UpdateOne(
			ctx,
			bson.M{"_id": requirementId},
			bson.M{
				"$set": bson.M{"status": "closed"},
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func confirmStockQuote(ctx context.Context, db *mongo.Database, quote map[string]interface{}) error {

	stockId, ok1 := quote["stockId"].(string)
	quoteId, okQuote := quote["_id"].(string)

	if !ok1 || !okQuote {
		return errors.New("Invalid stockId or quoteId")
	}

	var quantity int32
	switch v := quote["quantity"].(type) {
	case int32:
		quantity = v
	case float64:
		quantity = int32(v)
	default:
		return errors.New("Invalid quantity")
	}

	// 1️⃣ Check if already exists
	var stock map[string]interface{}
	err := db.Collection("stocks").
		FindOne(ctx, bson.M{"_id": stockId}).
		Decode(&stock)
	if err != nil {
		return err
	}

	if arr, ok := stock["confirmedUserId"].(primitive.A); ok {
		for _, id := range arr {
			if id == quoteId {
				return errors.New("You have already responded. Please apply another quote.")
			}
		}
	}

	// 2️⃣ Increment confirmedKg AND push quoteId
	_, err = db.Collection("stocks").UpdateOne(
		ctx,
		bson.M{"_id": stockId},
		bson.M{
			"$inc": bson.M{"confirmedKg": quantity},
			"$addToSet": bson.M{
				"confirmedUserId": quoteId,
			},
		},
	)
	if err != nil {
		return err
	}

	// 3️⃣ Fetch updated stock
	err = db.Collection("stocks").
		FindOne(ctx, bson.M{"_id": stockId}).
		Decode(&stock)
	if err != nil {
		return err
	}

	confirmedKg := getInt32(stock["confirmedKg"])
	availableQty := getInt32(stock["availableqty"])

	// 4️⃣ Close if fully sold
	if confirmedKg >= availableQty {
		_, err = db.Collection("stocks").UpdateOne(
			ctx,
			bson.M{"_id": stockId},
			bson.M{
				"$set": bson.M{"status": "closed"},
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func UpdateViewFavoriteHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	userToken := utils.GetUserTokenValue(c)
	collectionName := c.Params("collection")
	actionType := c.Params("type")
	documentId := c.Params("id")

	allowedCollections := map[string]bool{
		"requirements": true,
		"stocks":       true,
	}

	if !allowedCollections[collectionName] {
		return shared.BadRequest("Invalid collection name")
	}

	if actionType != "viewed" && actionType != "favorite" {
		return shared.BadRequest("Invalid action type")
	}

	db := database.GetConnection(org.Id)
	ctx := context.Background()

	filter := bson.M{"_id": documentId}
	colName := collectionName
	switch collectionName {
	case "stock_quotes", "quotes":
		colName = "response"

	case "requirements", "stocks":
		colName = "post"
	}
	// First check if user already exists in array
	var result bson.M
	err := db.Collection(colName).FindOne(ctx, filter).Decode(&result)
	if err != nil {
		return shared.BadRequest("Document not found")
	}

	// Convert array
	var userArray []interface{}
	if arr, ok := result[actionType].(primitive.A); ok {
		userArray = arr
	}

	// Check user already present
	for _, v := range userArray {
		if v == userToken.UserId {
			// If viewed → just return
			if actionType == "viewed" {
				return shared.SuccessResponse(c, fiber.Map{
					"message": "Already viewed",
				})
			}
		}
	}

	var update bson.M

	// VIEWED (no payload)
	if actionType == "viewed" {
		update = bson.M{
			"$addToSet": bson.M{
				"viewed": userToken.UserId,
			},
		}
	} else {
		// FAVORITE (true / false)
		var body struct {
			Status bool `json:"status"`
		}
		if err := c.BodyParser(&body); err != nil {
			return shared.BadRequest("Invalid payload")
		}

		if body.Status {
			update = bson.M{
				"$addToSet": bson.M{
					"favorite": userToken.UserId,
				},
			}
		} else {
			update = bson.M{
				"$pull": bson.M{
					"favorite": userToken.UserId,
				},
			}
		}
	}

	_, err = db.Collection(colName).UpdateOne(ctx, filter, update)
	if err != nil {
		return shared.BadRequest("Update failed: " + err.Error())
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message": actionType + " updated successfully",
	})
}

// func ThemeUpdateHandler(c *fiber.Ctx) error {

// 	org, exists := helper.GetOrg(c)
// 	if !exists {
// 		return shared.BadRequest("Organization Id missing")
// 	}

// 	userToken := utils.GetUserTokenValue(c)

// 	collectionName := c.Params("collection")

// 	// Allow only theme collection
// 	allowedCollections := map[string]bool{
// 		"theme": true,
// 	}

// 	if !allowedCollections[collectionName] {
// 		return shared.BadRequest("Invalid collection name")
// 	}

// 	// Payload structure
// 	var body struct {
// 		Payload bson.M `json:"payload"`
// 	}

// 	if err := c.BodyParser(&body); err != nil {
// 		return shared.BadRequest("Invalid payload")
// 	}

// 	// Set updated fields
// 	body.Payload["updated_on"] = time.Now()
// 	body.Payload["updated_by"] = userToken.UserId

// 	// Hardcoded _id
// 	objectID, err := primitive.ObjectIDFromHex("69fad8b51405a743ac1ae26a")
// 	if err != nil {
// 		return shared.BadRequest("Invalid theme id")
// 	}

// 	db := database.GetConnection(org.Id)
// 	ctx := context.Background()

// 	filter := bson.M{
// 		"_id": objectID,
// 	}

// 	update := bson.M{
// 		"$set": body.Payload,
// 	}

// 	result, err := db.Collection(collectionName).UpdateOne(ctx, filter, update)
// 	if err != nil {
// 		return shared.BadRequest("Update failed: " + err.Error())
// 	}

// 	if result.MatchedCount == 0 {
// 		return shared.BadRequest("Theme not found")
// 	}

// 	return shared.SuccessResponse(c, fiber.Map{
// 		"message": "Theme updated successfully",
// 	})
// }

func ThemeGetHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collection")

	allowedCollections := map[string]bool{
		"theme": true,
	}

	if !allowedCollections[collectionName] {
		return shared.BadRequest("Invalid collection name")
	}

	db := database.GetConnection(org.Id)
	ctx := context.Background()

	// _id is STRING
	filter := bson.M{
		"_id": "69fad8b51405a743ac1ae26a",
	}

	var result bson.M

	err := db.Collection(collectionName).FindOne(ctx, filter).Decode(&result)
	if err != nil {
		return shared.BadRequest("Theme record not found")
	}

	return shared.SuccessResponse(c, result)
}
