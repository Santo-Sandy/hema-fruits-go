package entities

import (
	"context"
	"fmt"
	"time"

	firebase "firebase.google.com/go/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/fcm"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

var marketplaceFirebaseConfig = &firebase.Config{
	ProjectID: "kajupro-market-place",
}

func sendAdminReportFcmNotification(orgId string, data map[string]interface{}) {
	ctx := context.Background()
	db := database.GetConnection(orgId)

	pipeline := bson.A{
		bson.D{{"$match", bson.D{
			{"role", bson.D{{"$in", bson.A{"admin", "Admin"}}}},
			{"$or", bson.A{
				bson.D{{"status", bson.D{{"$in", bson.A{"active", "Active", "ACTIVE"}}}}},
				bson.D{{"status", bson.D{{"$exists", false}}}},
				bson.D{{"status", nil}},
			}},
		}}},
		bson.D{{"$lookup", bson.D{
			{"from", "user_device_history"},
			{"localField", "_id"},
			{"foreignField", "user_id"},
			{"pipeline", bson.A{
				bson.D{{"$match", bson.D{
					{"session_closed", false},
					{"fcm_token", bson.D{{"$nin", bson.A{primitive.Null{}, "", nil}}}},
				}}},
				bson.D{{"$group", bson.D{
					{"_id", primitive.Null{}},
					{"fcmtokens", bson.D{{"$addToSet", "$fcm_token"}}},
				}}},
			}},
			{"as", "deviceData"},
		}}},
		bson.D{{"$unwind", bson.D{
			{"path", "$deviceData"},
			{"preserveNullAndEmptyArrays", false},
		}}},
		bson.D{{"$project", bson.D{
			{"receiverId", "$_id"},
			{"fcmtokens", "$deviceData.fcmtokens"},
		}}},
	}

	cursor, err := db.Collection("users").Aggregate(ctx, pipeline)
	if err != nil {
		fmt.Println("Admin report notification aggregation error:", err)
		return
	}
	defer cursor.Close(ctx)

	var allTokens []string
	var receiverIds []string

	for cursor.Next(ctx) {
		var result struct {
			ReceiverId interface{} `bson:"receiverId"`
			FcmTokens  []string    `bson:"fcmtokens"`
		}

		if err := cursor.Decode(&result); err != nil {
			fmt.Println("Admin report notification decode error:", err)
			continue
		}

		receiverIds = append(receiverIds, fmt.Sprintf("%v", result.ReceiverId))
		allTokens = append(allTokens, result.FcmTokens...)
	}

	if len(allTokens) == 0 {
		fmt.Println("No admin FCM tokens found for report notification")
		return
	}

	reportType := fmt.Sprintf("%v", data["type"])
	reason := fmt.Sprintf("%v", data["reason"])
	reportId := fmt.Sprintf("%v", data["_id"])

	title := "New user report"
	body := fmt.Sprintf("%s report submitted: %s", reportType, reason)
	webPath := "/admin/report"
	mobilePath := bson.M{
		"path": "/admin/report",
		"id":   reportId,
	}

	payload := map[string]string{
		"_id":     reportId,
		"type":    reportType,
		"reason":  reason,
		"webPath": webPath,
	}
	for k, v := range data {
		payload[k] = fmt.Sprintf("%v", v)
	}

	if err := fcm.SendMultipleFCMNotification(
		"marketPlace-serviceAccountKey.json",
		marketplaceFirebaseConfig,
		allTokens,
		title,
		body,
		payload,
	); err != nil {
		fmt.Println("Admin report FCM error:", err)
		return
	}

	senderId := data["userId"]
	if senderId == nil {
		senderId = data["buyerId"]
	}

	for _, receiverId := range receiverIds {
		history := bson.M{
			"_id":        helper.Generateuniquekey(),
			"senderId":   senderId,
			"receiverId": receiverId,
			"title":      title,
			"body":       body,
			"isread":     false,
			"webPath":    webPath,
			"mobilePath": mobilePath,
			"metadata":   payload,
			"created_on": time.Now().UTC(),
		}

		if _, err := db.Collection("notifications_history").InsertOne(ctx, history); err != nil {
			fmt.Println("Admin report notification history insert error:", err)
		}
	}
}
