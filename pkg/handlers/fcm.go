package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/middleware"
)

type FCMRegisterRequest struct {
	FCMToken    string `json:"fcmToken"`
	Platform    string `json:"platform"`
	AppID       string `json:"app_id"`
	IMEI        string `json:"imei"`
	DeviceModel string `json:"device_model"`
}

// RegisterFCM registers a new FCM device token
func RegisterFCM(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	if userToken.UserId == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req FCMRegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if req.Platform == "" {
		req.Platform = "web"
	}

	ip := c.Get("X-Forwarded-For")
	if ip == "" {
		ip = c.IP()
	}
	userAgent := c.Get("user-agent")

	db := config.GetDB()
	ctx := context.Background()

	docId := GenerateUniqueKey()
	set := bson.M{
		"user_id":        userToken.UserId,
		"platform":       req.Platform,
		"last_login":     time.Now().UTC(),
		"fcm_token":      req.FCMToken,
		"ip":             ip,
		"user_agent":     userAgent,
		"session_closed": false,
	}

	if req.Platform != "web" {
		if req.AppID != "" {
			set["app_id"] = req.AppID
		}
		if req.IMEI != "" {
			set["imei"] = req.IMEI
		}
		if req.DeviceModel != "" {
			set["device_model"] = req.DeviceModel
		}
	}

	update := bson.M{"$set": set}
	if req.Platform == "web" {
		update["$unset"] = bson.M{
			"app_id":       "",
			"imei":         "",
			"device_model": "",
		}
	}

	opts := options.Update().SetUpsert(true)
	_, err := db.Collection("user_device_history").UpdateOne(ctx, bson.M{"fcm_token": req.FCMToken}, update, opts)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save FCM registration: " + err.Error()})
	}

	// Clean up expired sessions asynchronously
	go func() {
		db.Collection("user_device_history").UpdateMany(
			context.Background(),
			bson.M{
				"user_id":        userToken.UserId,
				"session_closed": false,
				"jwt_expires_at": bson.M{"$lt": time.Now().UTC()},
			},
			bson.M{"$set": bson.M{"session_closed": true}},
		)
	}()

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "registered",
		"doc_id":  docId,
	})
}

// LogoutFCM marks device session as closed
func LogoutFCM(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	if userToken.UserId == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID is required"})
	}

	db := config.GetDB()
	ctx := context.Background()

	result, err := db.Collection("user_device_history").UpdateOne(
		ctx,
		bson.M{"_id": id, "user_id": userToken.UserId},
		bson.M{"$set": bson.M{"session_closed": true}},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if result.MatchedCount == 0 {
		// Fallback lookup by token if parameter represents token
		db.Collection("user_device_history").UpdateOne(
			ctx,
			bson.M{"fcm_token": id, "user_id": userToken.UserId},
			bson.M{"$set": bson.M{"session_closed": true}},
		)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "logged out successfully",
		"id":      id,
	})
}
