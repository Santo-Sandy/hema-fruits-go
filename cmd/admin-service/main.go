package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/handlers"
	"hema-fruits-go/pkg/middleware"
)

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file, continuing with default environment variables")
	}

	// Initialize MongoDB Connection
	config.InitDB()

	// Initialize Fiber application
	app := fiber.New(fiber.Config{
		AppName:   "Hema Fruits REST API",
		BodyLimit: 300 * 1024 * 1024,
	})

	// Setup CORS Middlewares globally
	app.Use(middleware.CORS())

	// -- PUBLIC ROUTES --
	
	// SSO and Email logins
	app.Post("/market-auth/login", handlers.LoginHandler)
	app.Post("/market-auth/sso-login", handlers.MarketSSoLoginHandler)
	app.Get("/market-auth/checklayoutlogin", handlers.CheckLayoutLogin)
	app.Get("/market/imageurl", handlers.GetImageUrl)

	// OTP handling
	app.Post("/auth/send-otp", handlers.SendOtp)
	app.Get("/auth/generate-otp/:email_id", handlers.SendOtp) // Fallback support for GET query
	app.Post("/auth/verify-otp", handlers.VerifyOTP)
	app.Post("/auth/resetpassword", handlers.ResetPassword)
	app.Get("/auth/config", handlers.OrgConfigHandler)

	// -- SECURED ROUTES (Token Required) --
	secureGroup := app.Group("/", middleware.AuthRequired())

	// Password update
	secureGroup.Post("/auth/changepassword", handlers.ChangePassword)

	// FCM Device tokens
	secureGroup.Post("/fcm/register", handlers.RegisterFCM)
	secureGroup.Post("/fcm/logout/:id", handlers.LogoutFCM)

	// Dynamic CRUD / Filter engine
	secureGroup.Get("/entities/:collectionName/:id", handlers.GetDocByIdHandler)
	secureGroup.Post("/entities/:collectionName", handlers.PostDocHandler)
	secureGroup.Put("/entities/:collectionName/:id", handlers.PutDocByIDHandlers)
	secureGroup.Delete("/entities/:collectionName/:id", handlers.DeleteById)
	secureGroup.Delete("/entities/:collectionName", handlers.DeleteByAll)
	secureGroup.Post("/entities/filter/:collectionName", handlers.GetDocsHandler)

	// Payments integration
	secureGroup.Post("/payments/order", handlers.CreatePaymentOrder)
	secureGroup.Get("/payments/verify/:orderId", handlers.VerifyPaymentOrder)

	// Quota Point and States Confirmation handling
	secureGroup.Post("/capitalmarket/creditPoint", handlers.UpdateCreditPoint)
	secureGroup.Post("/capitalmarket/:collection/:type", handlers.PostQuotaHandler)
	secureGroup.Put("/capitalmarket/:collection/:id", handlers.SettingsHandler)
	secureGroup.Post("/capitalmarket/:collection/:id/:type", handlers.UpdateViewFavoriteHandler)
	secureGroup.Delete("/capitalmarket/:collection/:id", handlers.DeleteHandler)

	secureGroup.Put("/confirm/:collection/:id", handlers.UpdateConfirmStatusHandler)
	secureGroup.Post("/confirm/:userid", handlers.GiveRewardHandler)
	secureGroup.Get("/confirm/:collection", handlers.ThemeGetHandler)

	// Default fallback handler
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"status":  404,
			"message": "Endpoint not found",
		})
	})

	listenUrl := os.Getenv("ADMIN_SERVER_LISTEN_URL")
	if listenUrl == "" {
		listenUrl = "0.0.0.0:7002"
	}

	log.Printf("Starting Hema Fruits API Server on %s\n", listenUrl)
	if err := app.Listen(listenUrl); err != nil {
		log.Panic("Server failed to boot: ", err)
	}
}
