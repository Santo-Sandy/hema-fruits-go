package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"kriyatec.com/pms-api/pkg/shared/admin-service/entities"
	"kriyatec.com/pms-api/pkg/shared/adminsubscription"
	"kriyatec.com/pms-api/pkg/shared/authentication"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/fcm"
	"kriyatec.com/pms-api/pkg/shared/onboarding"
	"kriyatec.com/pms-api/server"
)

func main() {

	// Load environment variables from the .env file.
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Server initialization
	app := server.Create()

	// By Default try to connect shared db
	database.Init()

	// Start daily trial data scheduler
	// log.Println("[Main] Starting daily trial data scheduler...")
	// go onboarding.StartDailyTrialDataScheduler()

	// Set up authentication routes for routes that do not require a token.
	authentication.SetupRoutes(app)
	fcm.SetupRoutes(app)
	app.Get("/get-data/:installCode", authentication.GetOrgaData)
	// Set up register User
	authentication.SetupRegisterUser(app)

	// Set up all routes for the application.
	entities.SetupAllRoutes(app)
	adminsubscription.SetupRoutes(app)
	// Initialize custom validators for data validation.
	// helper.InitCustomValidator()

	if err := server.Listen(app, os.Getenv("ADMIN_SERVER_LISTEN_URL")); err != nil {
		log.Panic(err)
	}

}
