package adminsubscription

import (
	"github.com/gofiber/fiber/v2"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

func SetupRoutes(app *fiber.App) {
	SubscriptionModules(app)
	EntityModules(app)
	// Restricted Routes
	// auth.Post("/changepassword", postforgetPasswordHandler)

}
func SubscriptionModules(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/capitalmarket/", "Subscription APIs")
	r.Post("/creditPoint", UpdateCreditPoint)
	r.Post("/:collection/:type", PostQuotaHandler)
	r.Put("/:collection/:id/", SettingsHandler)
	r.Post("/:collection/:id/:type", UpdateViewFavoriteHandler)
	r.Delete("/:collection/:id/", deleteHandler)
}

func EntityModules(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/confirm/", "Confirm APIs")
	r.Put("/:collection/:id", UpdateConfirmStatusHandler)
	r.Post("/:userid", GiveRewardHandler)
	r.Get("/:collection", ThemeGetHandler)
}
