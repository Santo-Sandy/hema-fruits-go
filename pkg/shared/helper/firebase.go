package helper

import (
	"context"
	"errors"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

func ValidateFirebaseUserByEmail(emailId, credentialFilePath string) (*auth.UserRecord, error) {
	ctx := context.Background()

	// Initialize Firebase app
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialFilePath))
	if err != nil {
		log.Printf("error initializing Firebase app: %v", err)
		return nil, errors.New("failed to initialize Firebase")
	}

	// Get Auth client
	client, err := app.Auth(ctx)
	if err != nil {
		log.Printf("error getting Auth client: %v", err)
		return nil, errors.New("failed to get Auth client")
	}

	// Get user by UID
	u, err := client.GetUserByEmail(ctx, emailId)
	if err != nil {
		log.Printf("error getting user %s: %v", emailId, err)
		return nil, errors.New("user not found")
	}

	log.Printf("Successfully fetched user data: %v", u)
	return u, nil
}
