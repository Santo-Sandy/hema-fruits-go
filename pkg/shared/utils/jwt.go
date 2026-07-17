package utils

import (
	"time"

	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v2"
	"github.com/golang-jwt/jwt/v4"
)

func GenerateJWTToken(claims jwt.MapClaims, ExpiryMinutes time.Duration) string {
	// Set Issued at and token expiry
	claims["iat"] = time.Now().Unix() // Issued at Time
	claims["exp"] = time.Now().Add(ExpiryMinutes * time.Minute).Unix()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// TODO: Set some string Keys and read from config securely instead of hard code (same as above)
	signedKey := getSignedKey()
	if signedKey == nil {
		return ""
	}

	s, err := token.SignedString(signedKey)
	if err != nil {
		return ""
	}

	return s
}

func GetUserTokenValue(c *fiber.Ctx) UserToken {
	var claim = GetUserClaims(c)
	if claim == nil {
		return UserToken{}
	}
	var factoryId string
	if val, ok := claim["factory_id"].(string); ok {
		factoryId = val
	}
	userId, _ := claim["id"].(string)
	userRole, _ := claim["role"].(string)
	org_id, _ := claim["uo_id"].(string)
	return UserToken{
		UserId:    userId,
		UserRole:  userRole,
		OrgId:     org_id,
		FactoryId: factoryId,
	}
}

func GetToken(c *fiber.Ctx) *jwt.Token {
	user, ok := c.Locals("user").(*jwt.Token)
	if !ok {
		return nil
	}
	return user
}
func GetUserClaims(c *fiber.Ctx) jwt.MapClaims {
	user := GetToken(c)
	if user == nil {
		return nil
	}
	claims, ok := user.Claims.(jwt.MapClaims)
	if !ok {
		return nil
	}
	return claims
}

// Protected protect routes
func JWTMiddleware() func(*fiber.Ctx) error {
	return jwtware.New(jwtware.Config{
		SigningKey:   getSignedKey(),
		ErrorHandler: jwtError,
	})
}

func jwtError(c *fiber.Ctx, err error) error {
	if err.Error() == "Missing or malformed JWT" {
		c.Status(fiber.StatusBadRequest)
		return c.JSON(fiber.Map{"status": "error", "message": "Auth Token Missing", "data": nil})
	} else {
		c.Status(fiber.StatusUnauthorized)
		return c.JSON(fiber.Map{"status": "error", "message": "Request Unauthorized", "data": nil})
	}
}

func GetNewJWTClaim() jwt.MapClaims {
	return jwt.MapClaims{}
}

func getSignedKey() []byte {
	return []byte(GetenvStr("KriyaTec@2023@pms$%ˆ"))
}
