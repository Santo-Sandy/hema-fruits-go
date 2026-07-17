// Package fcm provides Firebase Cloud Messaging functionality for sending push notifications
package fcm

import (
	"context"
	"fmt"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/api/option"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

var ctx = context.Background()
var opts = options.Update().SetUpsert(true)

// SendSingleNewFCMNotification sends a push notification to a single device
// Parameters:
//   - deviceToken: FCM device token of the recipient
//   - title: notification title
//   - body: notification body text
//   - credentialFilePath: path to Firebase service account JSON file
//   - config: Firebase configuration
//   - data: additional custom data to send with the notification
func SendSingleNewFCMNotification(credentialFilePath string, config *firebase.Config, deviceToken, title, body string, data map[string]string) error {
	// Initialize Firebase App with credentials
	//opt := option.WithCredentialsFile("service-account.json") // Replace with actual path

	fmt.Println(deviceToken, "device token", title, body)
	fmt.Println("config", config)

	app, err := firebase.NewApp(ctx, config, option.WithCredentialsFile(credentialFilePath))
	if err != nil {
		return fmt.Errorf("error initializing Firebase app: %v", err)
	}

	// Get Firebase Messaging Client
	client, err := app.Messaging(ctx)
	if err != nil {
		return fmt.Errorf("error getting Messaging client: %v", err)
	}

	// Define FCM Message
	message := &messaging.Message{
		Token: deviceToken,
		Data:  data,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
	}

	// Send FCM Message
	response, err := client.Send(ctx, message)
	if err != nil {
		return err
	}

	fmt.Println("FCM Notification Sent Successfully! Response:", response)
	return nil
}

// SendMultipleFCMNotification sends a push notification to multiple devices
// Parameters:
//   - credentialFilePath: path to Firebase service account JSON file
//   - config: Firebase configuration
//   - deviceTokens: slice of FCM device tokens
//   - title: notification title
//   - body: notification body text
//   - data: additional custom data to send with the notification
func SendMultipleFCMNotification(credentialFilePath string, config *firebase.Config, deviceTokens []string, title, body string, data map[string]string) error {
	if len(deviceTokens) == 0 {
		return fmt.Errorf("no device tokens provided")
	}
	fmt.Println("config", config)
	// Initialize Firebase App
	app, err := firebase.NewApp(ctx, config, option.WithCredentialsFile(credentialFilePath))
	if err != nil {
		return fmt.Errorf("error initializing Firebase app: %v", err)
	}

	// Get Firebase Messaging Client
	client, err := app.Messaging(ctx)
	if err != nil {
		return fmt.Errorf("error getting Messaging client: %v", err)
	}

	// Define FCM Message
	message := &messaging.MulticastMessage{
		Tokens: deviceTokens,
		Data:   data,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
	}
	// Send FCM Message
	br, err := client.SendEachForMulticast(ctx, message)
	if err != nil {
		return fmt.Errorf("error sending FCM message: %v", err)
	}

	if br.FailureCount > 0 {
		var failedTokens []string
		for idx, resp := range br.Responses {
			if !resp.Success {
				failedTokens = append(failedTokens, deviceTokens[idx])
			}
		}
		fmt.Printf("List of tokens that failed: %v\n", failedTokens)
	}

	fmt.Println("FCM Notification Sent Successfully! Response:", br)
	return nil
}

// ValidateImageLink validates and formats image URLs
// Returns the formatted URL and a boolean indicating validity
func ValidateImageLink(link string) (string, bool) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", false
	}
	// Regex to check for valid image file extensions at the end of a URL/path
	// If it starts with https and doesn't match the pattern, reject
	if strings.HasPrefix(link, "https") {
		return link, true
	}
	// send all with base url + link
	return helper.GetImageBaseUrl() + link, true
}
