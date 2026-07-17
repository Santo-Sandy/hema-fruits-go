package adminsubscription

import (
	"context"
	"fmt"
	"time"

	firebase "firebase.google.com/go/v4"
	"go.mongodb.org/mongo-driver/bson"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/fcm"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

var config = &firebase.Config{
	ProjectID: "kajupro-market-place",
}

func sendGroupFcmNotification(orgId string, data map[string]interface{}) {

	ctx := context.Background()
	db := database.GetConnection(orgId)

	name := fmt.Sprintf("%v", data["name"])

	var senderId interface{}
	var roles []string
	var businessTypes []string

	if t, ok := data["type"].(string); ok && t != "" {
		businessTypes = []string{t, "Both"}
	} else {
		businessTypes = []string{"Both"}
	}

	// Determine sender & target roles
	if name == "requirements" {
		senderId = data["buyerId"]
		roles = []string{"processor", "both"}
	}
	if name == "stocks" {
		senderId = data["userId"]
		roles = []string{"buyer", "both"}
	}

	//  Build pipeline
	pipeline := buildFcmPipeline(senderId, roles, businessTypes)

	cursor, err := db.Collection("users").Aggregate(ctx, pipeline)
	if err != nil {
		fmt.Println("Aggregation error:", err)
		return
	}
	defer cursor.Close(ctx)

	var allTokens []string
	var receiverIds []string

	for cursor.Next(ctx) {

		var result struct {
			ReceiverId interface{} `bson:"receiverId"`
			Fcmtokens  []string    `bson:"fcmtokens"`
		}

		if err := cursor.Decode(&result); err == nil {

			receiverIds = append(receiverIds, fmt.Sprintf("%v", result.ReceiverId))
			allTokens = append(allTokens, result.Fcmtokens...)
		}
	}

	if len(allTokens) == 0 {
		fmt.Println("No tokens found")
		return
	}

	//  Prepare notification
	var title string
	var body string
	var webPath string
	var mobilePath string

	if name == "requirements" {
		title = "New requirement 🔔"
		body = fmt.Sprintf("A buyer needs %v Kg of %v", data["requiredqty"], data["grade"])
		webPath = fmt.Sprintf("/merchant/enquiries/%v", data["_id"])
		mobilePath = fmt.Sprintf("/sellerposts/:id")

	}

	if name == "stocks" {
		title = "New stock 🔔"
		body = fmt.Sprintf("A seller has %v Kg of %v", data["availableqty"], data["grade"])
		webPath = fmt.Sprintf("/product/%v", data["_id"])
		mobilePath = fmt.Sprintf("/posts/:id")
	}

	payload := make(map[string]string)
	for k, v := range data {
		payload[k] = fmt.Sprintf("%v", v)
	}

	mobilerout := bson.M{
		"path": mobilePath,
		"id":   payload["_id"],
	}

	err = fcm.SendMultipleFCMNotification(
		"marketPlace-serviceAccountKey.json",
		config,
		allTokens,
		title,
		body,
		payload,
	)

	if err != nil {
		fmt.Println("FCM error:", err)
		return
	}

	//  Store Notification History
	for _, receiverId := range receiverIds {

		history := bson.M{
			"_id":        helper.Generateuniquekey(),
			"senderId":   senderId,
			"receiverId": receiverId,
			"title":      title,
			"isread":     false,
			"body":       body,
			"webPath":    webPath,
			"mobilePath": mobilerout,
			"metadata":   payload,
			"created_on": time.Now().UTC(),
		}

		_, err := db.Collection("notifications_history").InsertOne(ctx, history)
		if err != nil {
			fmt.Println("History insert error:", err)
		}
	}

	fmt.Println("Notification sent and stored successfully")
}

func sendEnquiryFcmNotification(orgId string, data map[string]interface{}) {
	ctx := context.Background()
	db := database.GetConnection(orgId)

	name := fmt.Sprintf("%v", data["name"])
	status := fmt.Sprintf("%v", data["status"])

	var sendNotificationUserId interface{} // The Receiver
	var responderId interface{}            // The Actor

	// --- Logic to Determine Receiver and Responder ---
	if name == "stock_quotes" {
		if status == "confirmed" || status == "rejected" {
			sendNotificationUserId = data["buyerId"]
			responderId = data["merchantId"]
		} else {
			sendNotificationUserId = data["merchantId"]
			responderId = data["buyerId"]
		}
	} else if name == "quotes" {
		if status == "confirmed" || status == "rejected" {
			sendNotificationUserId = data["merchantId"]
			responderId = data["buyerId"]
		} else {
			sendNotificationUserId = data["buyerId"]
			responderId = data["merchantId"]
		}
	}

	// Build the pipeline using the logic you provided
	pipeline := buildResponseNotificationPipeline(sendNotificationUserId, responderId)

	cursor, err := db.Collection("users").Aggregate(ctx, pipeline)
	if err != nil {
		fmt.Println("Aggregation error:", err)
		return
	}
	defer cursor.Close(ctx)

	var result struct {
		ReceiverId    interface{} `bson:"receiverId"`
		ResponderName string      `bson:"responderName"`
		FcmTokens     []string    `bson:"fcmtokens"`
	}

	// Since we are targeting a specific user, we only need the first result
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			fmt.Println("Decode error:", err)
			return
		}
	} else {
		fmt.Println("No active devices found for receiver")
		return
	}

	// --- Prepare Notification Content ---

	var webPath string

	title := "Quote Update"

	if name == "stock_quotes" {
		title = "👉 Stock Quote Update"
		webPath = fmt.Sprintf("/product/%v/response/%v", data["stockId"], data["_id"])
	} else {
		title = "👉 Requirement Quote Update"
		webPath = fmt.Sprintf("/requirement/%v/%v", data["requirementId"], data["_id"])
	}

	body := fmt.Sprintf("%s has responded to you", result.ResponderName)

	payload := make(map[string]string)
	for k, v := range data {
		payload[k] = fmt.Sprintf("%v", v)
	}
	payload["responderName"] = result.ResponderName

	var mobilerout bson.M

	if name == "stock_quotes" {
		mobilerout = bson.M{
			"path":      "/posts/:id",
			"id":        payload["_id"],
			"enquiryID": payload["stockId"],
		}
	} else {
		mobilerout = bson.M{
			"path":      "/sellerposts/:id",
			"id":        payload["_id"],
			"enquiryID": payload["requirementId"],
		}
	}

	// --- Send FCM ---
	err = fcm.SendMultipleFCMNotification(
		"marketPlace-serviceAccountKey.json",
		config,
		result.FcmTokens,
		title,
		body,
		payload,
	)
	if err != nil {
		fmt.Println("FCM error:", err)
		return
	}

	// --- Store Notification History ---
	history := bson.M{
		"_id":        helper.Generateuniquekey(),
		"senderId":   responderId,
		"receiverId": fmt.Sprintf("%v", result.ReceiverId),
		"title":      title,
		"body":       body,
		"isread":     false,
		"webPath":    webPath,
		"mobilePath": mobilerout,
		"created_on": time.Now().UTC(),
		"metadata":   payload,
	}

	_, err = db.Collection("notifications_history").InsertOne(ctx, history)
	if err != nil {
		fmt.Println("History insert error:", err)
	}

	fmt.Println("Enquiry notification sent successfully to:", result.ReceiverId)
}
