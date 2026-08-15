package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/golang-jwt/jwt/v4"
	"hema-fruits-go/pkg/config"
)

// Org represents the organization context
type Org struct {
	Id   string `json:"id" bson:"_id"`
	Name string `json:"name" bson:"name"`
}

// UserToken represents decoded token values
type UserToken struct {
	UserId    string
	UserRole  string
	OrgId     string
	FactoryId string
}

// CORS returns CORS middleware configuration
func CORS() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowHeaders:     "OrgId, orgid, Origin, Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Requested-With, Cache-Control, fcmToken",
		AllowMethods:     "POST, GET, PUT, OPTIONS, DELETE, HEAD",
		ExposeHeaders:    "Content-Type, Cache-Control, Connection, Transfer-Encoding",
		AllowCredentials: false,
	})
}

// AuthRequired is the middleware to enforce JWT validation
func AuthRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"status":  "error",
				"message": "Auth Token Missing",
			})
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"status":  "error",
				"message": "Invalid token format",
			})
		}

		tokenString := parts[1]
		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			return config.GetJWTSecret(), nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"status":  "error",
				"message": "Request Unauthorized",
			})
		}

		c.Locals("user", token)
		return c.Next()
	}
}

// GetUserTokenValue extracts user claims from context
func GetUserTokenValue(c *fiber.Ctx) UserToken {
	token, ok := c.Locals("user").(*jwt.Token)
	if !ok || token == nil {
		return UserToken{}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return UserToken{}
	}

	var factoryId string
	if val, ok := claims["factory_id"].(string); ok {
		factoryId = val
	}

	userId, _ := claims["id"].(string)
	userRole, _ := claims["role"].(string)
	orgId, _ := claims["uo_id"].(string)

	return UserToken{
		UserId:    userId,
		UserRole:  userRole,
		OrgId:     orgId,
		FactoryId: factoryId,
	}
}

// GetOrg retrieves Org from headers or defaults
func GetOrg(c *fiber.Ctx) (Org, bool) {
	orgId := c.Get("orgid")
	if orgId == "" {
		orgId = c.Get("OrgId")
	}
	if orgId == "" {
		orgId = "HEMA_FRUITS"
	}
	orgId = strings.ToUpper(orgId)
	return Org{
		Id:   orgId,
		Name: orgId,
	}, true
}
