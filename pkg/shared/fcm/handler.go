package fcm

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

// RegisterFCM registers a new FCM token for a user device
// Steps:
// 1. Validate organization and user authentication
// 2. Parse request body and extract device information
// 3. Get IP address and user agent from request headers
// 4. Extract JWT expiration time from token claims
// 5. Build device record with session_closed set to false
// 6. Upsert device record in user_device_history collection
// 7. Close any expired sessions for the user
func RegisterFCM(c *fiber.Ctx) error {
	org, ok := helper.GetOrg(c)
	if !ok {
		return shared.BadRequest("Organization Id missing")
	}
	db := database.GetConnection(org.Id)

	// Who is the user?
	user := utils.GetUserTokenValue(c)
	if strings.TrimSpace(user.UserId) == "" {
		return shared.BadRequest("Unauthorized: missing user")
	}

	// Parse body
	var in FCMRegisterRequest
	if err := c.BodyParser(&in); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Error parsing request body")
	}
	if in.Platform == "" {
		in.Platform = "web"
	}

	// IP (respect X-Forwarded-For)
	ip := strings.TrimSpace(c.Get("X-Forwarded-For"))
	if ip == "" {
		ip = c.IP()
	}

	user_agent := c.Get("user-agent")

	// JWT exp (safe cast)
	jwtToken := utils.GetToken(c) // *jwt.Token
	claims, _ := jwtToken.Claims.(jwt.MapClaims)
	var expUnix int64
	switch v := claims["exp"].(type) {
	case float64:
		expUnix = int64(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			expUnix = i
		}
	}
	jwtExp := time.Unix(expUnix, 0)

	userId, _ := claims["_id"].(string)
	if userId == "" {
		userId = user.UserId
	}

	set := bson.M{
		"user_id":        userId,
		"platform":       in.Platform,
		"last_login":     time.Now(),
		"fcm_token":      in.FCMToken,
		"ip":             ip,
		"user_agent":     user_agent,
		"session_closed": false,
	}
	if !jwtExp.IsZero() {
		set["jwt_expires_at"] = jwtExp
	}

	// Only persist device details for non-web platforms AND when non-empty
	if in.Platform != "web" {
		if s := strings.TrimSpace(in.AppID); s != "" {
			set["app_id"] = s
		}
		if s := strings.TrimSpace(in.IMEI); s != "" {
			set["imei"] = s
		}
		if s := strings.TrimSpace(in.DeviceModel); s != "" {
			set["device_model"] = s
		}
	}

	// For web: ensure these fields are NOT saved (and removed if they existed)
	update := bson.M{"$set": set}
	if in.Platform == "web" {
		update["$unset"] = bson.M{
			"app_id":       "",
			"imei":         "",
			"device_model": "",
		}
	}
	docsID := helper.Generateuniquekey()
	col := db.Collection("user_device_history")
	if _, err := col.
		UpdateOne(ctx, bson.M{"_id": docsID}, update, opts); err != nil {
		return shared.BadRequest("failed to save fcm registration: " + err.Error())
	}

	// Close expired sessions
	go closeExpiredSessions(col, user.UserId)

	return shared.SuccessResponse(c, fiber.Map{
		"message": "registered",
		"doc_id":  docsID,
	})
}

// LogoutFCM closes the session for a specific device record
// Steps:
// 1. Validate organization and user authentication
// 2. Get device ID from URL parameter
// 3. Update the specific device record to set session_closed to true
// 4. Verify the record exists and belongs to the user
// 5. Close any expired sessions for the user
func LogoutFCM(c *fiber.Ctx) error {
	org, ok := helper.GetOrg(c)
	if !ok {
		return shared.BadRequest("Organization Id missing")
	}
	db := database.GetConnection(org.Id)
	col := db.Collection("user_device_history")
	user := utils.GetUserTokenValue(c)
	if strings.TrimSpace(user.UserId) == "" {
		return shared.BadRequest("Unauthorized: missing user")
	}
	id := c.Params("id")
	if strings.TrimSpace(id) == "" {
		return shared.BadRequest("ID is required")
	}

	// Update the specific record
	result, err := col.UpdateOne(
		ctx,
		bson.M{"_id": id, "user_id": user.UserId},
		bson.M{"$set": bson.M{"session_closed": true}},
	)
	if err != nil {
		return shared.BadRequest("failed to logout: " + err.Error())
	}
	if result.MatchedCount == 0 {
		return shared.BadRequest("record not found")
	}

	// Close expired sessions
	go closeExpiredSessions(col, user.UserId)

	return shared.SuccessResponse(c, fiber.Map{
		"message": "logged out successfully",
		"id":      id,
	})
}

// closeExpiredSessions marks all expired JWT sessions as closed
// Finds all user sessions where jwt_expires_at is less than current time
// and session_closed is false, then sets session_closed to true
func closeExpiredSessions(collection *mongo.Collection, userId string) {
	collection.UpdateMany(
		ctx,
		bson.M{
			"user_id":        userId,
			"session_closed": false,
			"jwt_expires_at": bson.M{"$lt": time.Now()},
		},
		bson.M{"$set": bson.M{"session_closed": true}},
	)
}
