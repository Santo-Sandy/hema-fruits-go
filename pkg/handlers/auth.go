package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/smtp"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/middleware"
	"hema-fruits-go/pkg/models"
)

// GenerateUniqueKey generates a random hex ID
func GenerateUniqueKey() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// GenerateJWTToken creates a token valid for ExpiryMinutes
func GenerateJWTToken(claims jwt.MapClaims, ExpiryMinutes int) string {
	claims["iat"] = time.Now().Unix()
	claims["exp"] = time.Now().Add(time.Duration(ExpiryMinutes) * time.Minute).Unix()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString(config.GetJWTSecret())
	if err != nil {
		return ""
	}
	return s
}

// LoginHandler handles standard email/password login
func LoginHandler(c *fiber.Ctx) error {
	org, _ := middleware.GetOrg(c)

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid request body"})
	}

	if req.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid Email ID"})
	}

	db := config.GetDB()
	var user bson.M
	err := db.Collection("users").FindOne(context.Background(), bson.M{"email": req.Email}).Decode(&user)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid email or password"})
	}

	// Verify password
	var hashedPassword []byte
	if pwdVal, ok := user["pwd"]; ok {
		if bin, ok := pwdVal.(primitive.Binary); ok {
			hashedPassword = bin.Data
		} else if b, ok := pwdVal.([]byte); ok {
			hashedPassword = b
		}
	}

	if len(hashedPassword) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Account credentials not configured"})
	}

	err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(req.Password))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid email or password"})
	}

	// Check first time user
	if ft, ok := user["first_time_user"].(bool); ok && ft {
		db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": user["_id"]}, bson.M{"$set": bson.M{"first_time_user": false}})
	}

	role, _ := user["role"].(string)
	isProfileComplete, _ := user["is_profile_complete"].(bool)

	claims := jwt.MapClaims{
		"id":                user["_id"],
		"role":              role,
		"email":             user["email"],
		"uo_id":             org.Id,
		"isProfileComplete": isProfileComplete,
	}

	token := GenerateJWTToken(claims, 525600) // ~1 year

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data": fiber.Map{
			"success": true,
			"status":  "success",
			"token":   token,
			"org":     org,
		},
	})
}

// MarketSSoLoginHandler handles Google / Apple SSO logins
func MarketSSoLoginHandler(c *fiber.Ctx) error {
	org, _ := middleware.GetOrg(c)

	var req struct {
		Email          string `json:"email"`
		ProviderID     string `json:"provider_id"`
		ProviderBy     string `json:"provider_by"`
		Name           string `json:"name"`
		ProfilePicture string `json:"profilePicture"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Email == "" || req.ProviderID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Email and ProviderID are required"})
	}

	db := config.GetDB()
	userCollection := db.Collection("users")

	var user models.User
	err := userCollection.FindOne(context.Background(), bson.M{"email": req.Email}).Decode(&user)
	if err == nil {
		// User exists, generate token
		claims := jwt.MapClaims{
			"id":                user.ID,
			"role":              user.Role,
			"email":             user.Email,
			"uo_id":             org.Id,
			"isProfileComplete": user.IsProfileComplete,
		}
		token := GenerateJWTToken(claims, 525600)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  "success",
			"org":     org,
			"message": "SSO login successful",
			"token":   token,
		})
	}

	// User doesn't exist, create new one using details sent from SSO
	newUser := models.User{
		ID:                GenerateUniqueKey(),
		Email:             req.Email,
		Name:              req.Name,
		ProfilePicture:    req.ProfilePicture,
		Role:              "processor", // Default standard role
		Points:            0,
		IsProfileComplete: false,
		FirstLogin:        true,
		CreatedAt:         time.Now(),
	}

	_, err = userCollection.InsertOne(context.Background(), newUser)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	claims := jwt.MapClaims{
		"id":                newUser.ID,
		"role":              newUser.Role,
		"email":             newUser.Email,
		"uo_id":             org.Id,
		"isProfileComplete": newUser.IsProfileComplete,
	}
	token := GenerateJWTToken(claims, 525600)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"org":     org,
		"status":  "success",
		"message": "SSO login successful",
		"token":   token,
	})
}

// SendOtp generates and emails OTP
func SendOtp(c *fiber.Ctx) error {
	var req struct {
		EmailID string `json:"email_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid body"})
	}

	if req.EmailID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Email is required"})
	}

	db := config.GetDB()
	// Check if user already exists
	count, _ := db.Collection("users").CountDocuments(context.Background(), bson.M{"email": req.EmailID})
	if count > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Email Already Exists"})
	}

	// Generate 4 digit OTP
	otpNum, _ := rand.Int(rand.Reader, big.NewInt(9000))
	otp := int(otpNum.Int64() + 1000)

	// Update temporary_user
	_, err := db.Collection("temporary_user").UpdateOne(
		context.Background(),
		bson.M{"_id": req.EmailID},
		bson.M{"$set": bson.M{"otp": otp, "issued_on": time.Now(), "verified": false}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": err.Error()})
	}

	// Send Email
	clientEmail := os.Getenv("CLIENT_EMAIL")
	clientPassword := os.Getenv("CLIENT_EMAIL_PASSWORD")
	if clientEmail != "" && clientPassword != "" {
		msg := fmt.Sprintf("Subject: OTP Verification\nContent-Type: text/html\n\nYour OTP Code is <b>%d</b>. It expires in 5 minutes.", otp)
		auth := smtp.PlainAuth("", clientEmail, clientPassword, "smtp.gmail.com")
		go smtp.SendMail("smtp.gmail.com:587", auth, clientEmail, []string{req.EmailID}, []byte(msg))
	} else {
		fmt.Printf("Dev Mode: Send OTP %d to %s\n", otp, req.EmailID)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Verification Code Sent to Your Email",
	})
}

// VerifyOTP verifies the OTP code
func VerifyOTP(c *fiber.Ctx) error {
	var req struct {
		EmailID string `json:"email_id"`
		OTP     int    `json:"otp"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid body"})
	}

	db := config.GetDB()
	var tempUser models.TemporaryUser
	err := db.Collection("temporary_user").FindOne(context.Background(), bson.M{"_id": req.EmailID}).Decode(&tempUser)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "OTP not found or already used"})
	}

	if tempUser.Verified {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "OTP already used"})
	}

	if time.Since(tempUser.IssuedOn) > 5*time.Minute {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "OTP has expired"})
	}

	if tempUser.Otp != req.OTP {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid OTP"})
	}

	// Mark verified
	db.Collection("temporary_user").UpdateOne(context.Background(), bson.M{"_id": req.EmailID}, bson.M{"$set": bson.M{"verified": true}})

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "OTP Verified Successfully",
	})
}

// ResetPassword handles password reset
func ResetPassword(c *fiber.Ctx) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid body"})
	}

	db := config.GetDB()
	// Check temporary_user for verified OTP
	var tempUser models.TemporaryUser
	err := db.Collection("temporary_user").FindOne(context.Background(), bson.M{"_id": req.Email}).Decode(&tempUser)
	if err != nil || !tempUser.Verified {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "OTP verification required"})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": "Hashing failed"})
	}

	binaryHash := primitive.Binary{Subtype: 0x00, Data: hash}

	_, err = db.Collection("users").UpdateOne(
		context.Background(),
		bson.M{"email": req.Email},
		bson.M{"$set": bson.M{"pwd": binaryHash, "first_time_user": false}},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": "Update failed"})
	}

	// Clean up temp record
	db.Collection("temporary_user").DeleteOne(context.Background(), bson.M{"_id": req.Email})

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Password Reset Successfully",
	})
}

// ChangePassword updates password for logged in user
func ChangePassword(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	if userToken.UserId == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"status": "error", "message": "Unauthorized"})
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid body"})
	}

	db := config.GetDB()
	var user bson.M
	err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": userToken.UserId}).Decode(&user)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "User not found"})
	}

	var hashedPassword []byte
	if pwdVal, ok := user["pwd"]; ok {
		if bin, ok := pwdVal.(primitive.Binary); ok {
			hashedPassword = bin.Data
		} else if b, ok := pwdVal.([]byte); ok {
			hashedPassword = b
		}
	}

	err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(req.OldPassword))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid old password"})
	}

	newHash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	binaryHash := primitive.Binary{Subtype: 0x00, Data: newHash}

	db.Collection("users").UpdateOne(context.Background(), bson.M{"_id": userToken.UserId}, bson.M{"$set": bson.M{"pwd": binaryHash}})

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Password Changed Successfully",
	})
}

// OrgConfigHandler returns org settings
func OrgConfigHandler(c *fiber.Ctx) error {
	org, _ := middleware.GetOrg(c)
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data": fiber.Map{
			"org": org,
		},
	})
}

// GetImageUrl returns the static image base URL
func GetImageUrl(c *fiber.Ctx) error {
	imgUrl := os.Getenv("S3_APIENDPOINT")
	if imgUrl == "" {
		imgUrl = "https://cerp.sgp1.digitaloceanspaces.com/"
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data": fiber.Map{
			"data": imgUrl,
		},
	})
}

// CheckLayoutLogin returns whether layout login is required
func CheckLayoutLogin(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data": fiber.Map{
			"login": false,
		},
	})
}
