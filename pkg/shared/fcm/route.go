package fcm

import (
	"github.com/gofiber/fiber/v2"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

// SetupRoutes configures FCM routes
func SetupRoutes(app *fiber.App) {
	fcm := helper.CreateRouteGroup(app, "/fcm/", "FCM Messagenig")
	fcm.Post("/register", RegisterFCM)
	fcm.Post("/logout/:id", LogoutFCM)
}
