package helper

import (
	"github.com/gofiber/fiber/v2"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

func CreateRouteGroup(app *fiber.App, path string, desc string) fiber.Router {
	r := app.Group(path)
	//without JWT Token validation (without auth)
	r.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(desc)
	})
	// JWT Middleware
	r.Use(utils.JWTMiddleware())
	r.Use(func(c *fiber.Ctx) error {
		if !CheckUserActive(c) {
			claims := utils.GetUserTokenValue(c)
			if claims.OrgId == "TEAMALPHA" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Organization is not active"})
			}
			return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "user is not active"})
		}
		return c.Next()
	})
	// r.Use(cache.New(cache.Config{
	// 	Next: func(c *fiber.Ctx) bool {
	// 		return c.Query("refresh") == "true"
	// 	},
	// 	Expiration:   30 * time.Minute,
	// 	CacheControl: true,
	// 	KeyGenerator: func(c *fiber.Ctx) string {
	// 		return c.Path() + "|" + c.Get("OrgId")
	// 	},
	// }))
	return r
}
