package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/smtp"
	"os"
	"strings"
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

// SeedDefaultUsers creates the 3 demo users if they don't already exist.
// Credentials: admin@fruits.com, seller@fruits.com, buyer@fruits.com / password1234
func SeedDefaultUsers() {
	db := config.GetDB()
	ctx := context.Background()

	type seedUser struct {
		email  string
		name   string
		role   string
		mobile string
		id     string
		pic    string
	}

	seeds := []seedUser{
		// Admin Accounts
		{email: "admin@fruits.com", name: "Super Admin User", role: "admin", mobile: "9000000001", id: "usr_admin_seed_001", pic: "https://images.unsplash.com/photo-1534528741775-53994a69daeb?w=300"},
		{email: "operations@fruits.com", name: "Operations Manager", role: "admin", mobile: "9000000004", id: "usr_admin_seed_002", pic: "https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?w=300"},

		// Seller / Merchant Accounts
		{email: "seller@fruits.com", name: "Green Valley Organic Farms", role: "processor", mobile: "9000000002", id: "usr_seller_seed_001", pic: "https://images.unsplash.com/photo-1560250097-0b93528c311a?w=300"},
		{email: "merchant@fruits.com", name: "Hema Cashew & Nut Traders", role: "processor", mobile: "9000000005", id: "usr_seller_seed_002", pic: "https://images.unsplash.com/photo-1573496359142-b8d87734a5a2?w=300"},
		{email: "supplier@fruits.com", name: "Sunrise Agricultural Orchards", role: "processor", mobile: "9000000006", id: "usr_seller_seed_003", pic: "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?w=300"},

		// Buyer / Customer Accounts
		{email: "buyer@fruits.com", name: "Anita Sharma (Retail Buyer)", role: "buyer", mobile: "9000000003", id: "usr_buyer_seed_001", pic: "https://images.unsplash.com/photo-1494790108377-be9c29b29330?w=300"},
		{email: "wholesaler@fruits.com", name: "Fresh Market Wholesalers", role: "buyer", mobile: "9000000007", id: "usr_buyer_seed_002", pic: "https://images.unsplash.com/photo-1472099645785-5658abf4ff4e?w=300"},
		{email: "customer@fruits.com", name: "Rajesh Patel (Direct Customer)", role: "buyer", mobile: "9000000008", id: "usr_buyer_seed_003", pic: "https://images.unsplash.com/photo-1519085360753-af0119f7cbe7?w=300"},
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte("password1234"), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("SeedDefaultUsers: failed to hash password: %v\n", err)
		return
	}

	for _, s := range seeds {
		count, _ := db.Collection("users").CountDocuments(ctx, bson.M{"email": s.email})
		if count > 0 {
			fmt.Printf("SeedDefaultUsers: user %s already exists, skipping\n", s.email)
			continue
		}

		doc := bson.M{
			"_id":                 s.id,
			"email":               s.email,
			"name":                s.name,
			"role":                s.role,
			"mobile_number":       s.mobile,
			"pwd":                 primitive.Binary{Data: hashedPwd},
			"is_profile_complete": true,
			"points":              int32(1000),
			"first_login":         false,
			"isrewardgiven":       true,
			"profilePicture":      s.pic,
			"created_on":          time.Now().UTC(),
		}

		_, err := db.Collection("users").InsertOne(ctx, doc)
		if err != nil {
			fmt.Printf("SeedDefaultUsers: failed to insert %s: %v\n", s.email, err)
		} else {
			fmt.Printf("SeedDefaultUsers: created user %s (role=%s)\n", s.email, s.role)
		}
	}
}

// RegisterHandler handles direct user registration (buyer, seller/processor, admin)
func RegisterHandler(c *fiber.Ctx) error {
	org, _ := middleware.GetOrg(c)

	var req struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		Name         string `json:"name"`
		Role         string `json:"role"`
		MobileNumber string `json:"mobile_number"`
		Phone        string `json:"phone"`
		StoreName    string `json:"store_name"`
		Address      string `json:"address"`
		City         string `json:"city"`
		State        string `json:"state"`
		Pincode      string `json:"pincode"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Invalid request body"})
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Email is required"})
	}
	if req.Password == "" || len(req.Password) < 6 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Password must be at least 6 characters"})
	}
	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Name is required"})
	}

	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" || (role != "buyer" && role != "processor" && role != "seller" && role != "admin") {
		role = "buyer"
	}

	phone := req.MobileNumber
	if phone == "" {
		phone = req.Phone
	}

	db := config.GetDB()
	if db == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "error", "message": "Database not available"})
	}

	userCollection := db.Collection("users")

	// Check duplicate email
	count, err := userCollection.CountDocuments(context.Background(), bson.M{"email": email})
	if err == nil && count > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "An account with this email already exists"})
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": "Password encryption failed"})
	}

	userId := "usr_" + GenerateUniqueKey()
	defaultPic := "https://images.unsplash.com/photo-1534528741775-53994a69daeb?w=300"
	if role == "processor" || role == "seller" {
		defaultPic = "https://images.unsplash.com/photo-1560250097-0b93528c311a?w=300"
	}

	userDoc := bson.M{
		"_id":                 userId,
		"email":               email,
		"name":                req.Name,
		"role":                role,
		"mobile_number":       phone,
		"store_name":          req.StoreName,
		"address":             req.Address,
		"city":                req.City,
		"state":               req.State,
		"pincode":             req.Pincode,
		"pwd":                 primitive.Binary{Data: hashedPwd},
		"is_profile_complete": true,
		"points":              int32(1000), // 1000 starter reward points
		"first_login":         false,
		"isrewardgiven":       true,
		"profilePicture":      defaultPic,
		"created_on":          time.Now().UTC(),
	}

	_, err = userCollection.InsertOne(context.Background(), userDoc)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": "error", "message": "Failed to create user: " + err.Error()})
	}

	claims := jwt.MapClaims{
		"id":                userId,
		"role":              role,
		"email":             email,
		"uo_id":             org.Id,
		"isProfileComplete": true,
	}
	token := GenerateJWTToken(claims, 525600)

	delete(userDoc, "pwd")

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data": fiber.Map{
			"success": true,
			"status":  "success",
			"token":   token,
			"org":     org,
			"user":    userDoc,
		},
	})
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

	delete(user, "pwd")

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data": fiber.Map{
			"success": true,
			"status":  "success",
			"token":   token,
			"org":     org,
			"user":    user,
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
		// Update profile picture if provided from Google SSO / Apple SSO
		if req.ProfilePicture != "" {
			userCollection.UpdateOne(
				context.Background(),
				bson.M{"email": req.Email},
				bson.M{"$set": bson.M{"profilePicture": req.ProfilePicture}},
			)
			user.ProfilePicture = req.ProfilePicture
		}
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
			"user":    user,
			"data": fiber.Map{
				"token": token,
				"user":  user,
			},
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
		"user":    newUser,
		"data": fiber.Map{
			"token": token,
			"user":  newUser,
		},
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
