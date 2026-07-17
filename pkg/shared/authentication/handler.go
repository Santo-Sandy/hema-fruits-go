package authentication

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/exp/rand"

	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

// To get the ctx for Global
var ctx = context.Background()

func SendAdminInviteEmailHandler(c *fiber.Ctx) error {
	var req AdminInviteEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest("Invalid admin invite email request")
	}

	toEmail := strings.TrimSpace(req.To)
	if toEmail == "" {
		toEmail = strings.TrimSpace(req.Email)
	}
	name := strings.TrimSpace(req.Name)
	link := strings.TrimSpace(req.Link)

	if toEmail == "" {
		return shared.BadRequest("email is required")
	}
	if _, err := mail.ParseAddress(toEmail); err != nil {
		return shared.BadRequest("valid email is required")
	}
	if link == "" {
		return shared.BadRequest("invite link is required")
	}

	if err := helper.SendAdminInviteMail(name, link, strings.TrimSpace(req.ExpiresAt), toEmail); err != nil {
		return shared.InternalServerError(fmt.Sprintf("Admin invite email sending failed: %s", err.Error()))
	}

	return shared.SuccessResponse(c, fiber.Map{
		"message": "Admin invite email sent successfully",
		"email":   toEmail,
	})
}

// getDocsHandler --METHOD get the data from Db with pagination
func getDocsHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}
	if c.Params("collectionName") != "country" {
		return shared.BadRequest("Request Unauthorised")
	}

	//	userToken := utils.GetUserTokenValue(c)

	// collectionName := c.Params("collectionName")
	var requestBody helper.PaginationRequest

	if err := c.BodyParser(&requestBody); err != nil {
		return nil
	}

	var pipeline []primitive.M
	pipeline = helper.MasterAggregationPipeline(requestBody, c)
	// if userToken.UserRole != "SA" {
	// 	OrgPipeline := helper.GenerateOrgIdFilter(userToken.OrgId)
	// 	pipeline = append(pipeline, OrgPipeline)
	// 	//fmt.Println(OrgPipeline)
	// }
	if len(requestBody.Sort) > 0 {
		sortConditions := helper.BuildSortConditions(requestBody.Sort)
		pipeline = append(pipeline, sortConditions)
	}

	PagiantionPipeline := helper.PagiantionPipeline(requestBody.Start, requestBody.End)
	pipeline = append(pipeline, PagiantionPipeline)

	// if c.Params("collectionName") == "organization" || c.Params("collectionName") == "user" || c.Params("collectionName") == "db_config" {
	// 	org.Id = "shared"
	// }

	if c.Params("collectionName") == "organization" || c.Params("collectionName") == "role_acl" || c.Params("collectionName") == "user" || c.Params("collectionName") == "db_config" {
		org.Id = "shared"
	}

	fmt.Println(pipeline, org.Id)
	OrgId := org.Id
	if requestBody.FromAnotherOrg {
		OrgId = requestBody.OrgId
	}
	Response, err := helper.GetAggregateQueryResult(OrgId, c.Params("collectionName"), pipeline)
	if err != nil {
		if cmdErr, ok := err.(mongo.CommandError); ok {
			return shared.BadRequest(cmdErr.Message)
		}
	}
	return shared.SuccessResponse(c, Response)

}

// LoginHandler - Method to Valid the user id and password Auth
func LoginHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
		//org.Id = "shared" // for temporary
	}

	loginRequest := new(LoginRequest)
	if err := c.BodyParser(loginRequest); err != nil {
		return shared.BadRequest("Invalid params")
	}

	if loginRequest.Id == "" {
		return shared.BadRequest("Invalid User ID") // Added return statement
	}

	fmt.Println(org.Id)

	user, err := helper.FindOneDocument(org.Id, "user", bson.D{{"email", loginRequest.Id}})
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	if user == nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID and passwords")
		// shared.BadRequest("Invalid user ID") // Added return statement

		// 	if err == mongo.ErrNoDocuments {
		// 		return shared.BadRequest("Invalid user ID") // Added return statement
		// 	}

	}

	if !helper.CheckPassword(loginRequest.Password, primitive.Binary(user["pwd"].(primitive.Binary)).Data) {
		//return shared.BadRequest("Invalid User Password")
		return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID and password")
	}

	if user["first_time_user"] != nil {
		firstTimeUser := user["first_time_user"].(bool)
		if firstTimeUser {
			updateData := map[string]interface{}{
				"first_time_user": false,
			}
			helper.UpdateDataToDb(org.Id, bson.M{"email": loginRequest.Id}, updateData, "user")
		}
	}

	// If the password is valid, generate a JWT token
	claims := utils.GetNewJWTClaim()
	claims["id"] = user["_id"]
	claims["role"] = user["role"]
	roleConfig, err := helper.FindOneDocument(org.Id, "role_acl", bson.D{{"_id", user["role"]}})
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	if org.Id == "shared" {
		// claims["uo_id"] = user["org_id"]
		// org.Id = user["org_id"].(string)
		var orgMap map[string]interface{}
		database.GetConnection("shared").Collection("organization").FindOne(context.Background(), bson.M{"_id": org.Id}).Decode(&orgMap)
		if orgMap["name"] != nil {

			org.Name = orgMap["name"].(string)
			org.Id = orgMap["_id"].(string)
		}
	} else {
		claims["uo_id"] = org.Id
	}
	if user["factory_id"] != nil {
		claims["factory_id"] = user["factory_id"]

		wareHousePipeline := bson.A{
			bson.D{{"$match", bson.D{{"factory_id", user["factory_id"]}}}},
			bson.D{
				{"$group",
					bson.D{
						{"_id", "$factory_id"},
						{"warehouse_id", bson.D{{"$push", "$_id"}}},
					},
				},
			},
		}
		wareHouseData, _ := helper.GetAggregateQueryResult(org.Id, "company", wareHousePipeline)
		if len(wareHouseData) >= 0 {

			claims["warehouse_id"] = wareHouseData[0]["warehouse_id"]
		}

	} else {
		claims["factory_id"] = ""
	}

	// claims["uo_type"] = org.Type

	userName := user["name"]
	if userName == nil {
		userName = user["first_name"]
	}

	token := utils.GenerateJWTToken(claims, 525600) // 24*60
	// var response LoginResponse
	response := LoginResponse{
		Name:         userName.(string),
		UserRole:     user["role"].(string),
		RoleData:     roleConfig,
		UserOrg:      org,
		Token:        token,
		Email:        user["email"].(string),
		MobileNumber: user["mobile_number"].(string),
	}
	employeeId := user["org_id"]
	if employeeId != nil {
		response.EmployeeID = employeeId
	}
	if user["employee_id"] != nil {
		response.EmployeeID = user["employee_id"].(string)
	}

	if val, ok := user["first_login"].(bool); ok && val {
		response.FirstLogin = true
		updateData := map[string]interface{}{
			"first_login": false,
		}
		helper.UpdateDataToDb(org.Id, bson.M{"email": loginRequest.Id}, updateData, "user")
	}

	if val, ok := user["is_profile_completed"].(bool); ok {
		v := val
		response.IsProfileComplete = &v
	}

	return shared.SuccessResponse(c, fiber.Map{
		"Message":       "Login Successfully",
		"LoginResponse": response,
	})

}
func SSoLoginHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
		//org.Id = "shared" // for temporary
	}

	loginRequest := new(LoginRequest)
	if err := c.BodyParser(loginRequest); err != nil {
		return shared.BadRequest("Invalid params")
	}

	if loginRequest.Id == "" {
		return shared.BadRequest("Invalid User ID") // Added return statement
	}

	fmt.Println(org.Id)

	user, err := helper.FindOneDocument(org.Id, "user", bson.D{{"email", loginRequest.Id}})
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	if user == nil {
		// return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID and passwords")
		// shared.BadRequest("Invalid user ID") // Added return statement

		// 	if err == mongo.ErrNoDocuments {
		// 		return shared.BadRequest("Invalid user ID") // Added return statement
		// 	}

	}

	// if !helper.CheckPassword(loginRequest.Password, primitive.Binary(user["pwd"].(primitive.Binary)).Data) {
	// 	//return shared.BadRequest("Invalid User Password")
	// 	return fiber.NewError(fiber.StatusBadRequest, "Invalid user ID and password")
	// }

	if user["first_time_user"] != nil {
		firstTimeUser := user["first_time_user"].(bool)
		if firstTimeUser {
			updateData := map[string]interface{}{
				"first_time_user": false,
			}
			helper.UpdateDataToDb(org.Id, bson.M{"email": loginRequest.Id}, updateData, "user")
		}
	}

	// If the password is valid, generate a JWT token
	claims := utils.GetNewJWTClaim()
	claims["id"] = user["_id"]
	claims["role"] = user["role"]
	roleConfig, err := helper.FindOneDocument(org.Id, "role_acl", bson.D{{"_id", user["role"]}})
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	if org.Id == "shared" {
		// claims["uo_id"] = user["org_id"]
		// org.Id = user["org_id"].(string)
		var orgMap map[string]interface{}
		database.GetConnection("shared").Collection("organization").FindOne(context.Background(), bson.M{"_id": org.Id}).Decode(&orgMap)
		if orgMap["name"] != nil {

			org.Name = orgMap["name"].(string)
			org.Id = orgMap["_id"].(string)
		}
	} else {
		claims["uo_id"] = org.Id
	}
	if user["factory_id"] != nil {
		claims["factory_id"] = user["factory_id"]
	} else {
		claims["factory_id"] = ""
	}

	// claims["uo_type"] = org.Type

	userName := user["name"]
	if userName == nil {
		userName = user["first_name"]
	}

	token := utils.GenerateJWTToken(claims, 525600) // 24*60
	// var response LoginResponse
	response := LoginResponse{
		Name:         userName.(string),
		UserRole:     user["role"].(string),
		RoleData:     roleConfig,
		UserOrg:      org,
		Token:        token,
		Email:        user["email"].(string),
		MobileNumber: user["mobile_number"].(string),
	}

	if user["employee_id"] != nil {
		response.EmployeeID = user["employee_id"].(string)
	}

	if val, ok := user["first_login"].(bool); ok && val {
		response.FirstLogin = true
		updateData := map[string]interface{}{
			"first_login": false,
		}
		helper.UpdateDataToDb(org.Id, bson.M{"email": loginRequest.Id}, updateData, "user")
	}

	if val, ok := user["is_profile_completed"].(bool); ok {
		v := val
		response.IsProfileComplete = &v
	}

	return shared.SuccessResponse(c, fiber.Map{
		"Message":       "Login Successfully",
		"LoginResponse": response,
	})

}
func MarketSSoLoginHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
		//org.Id = "shared" // for temporary
	}
	var req ssoRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}
	if req.Email == "" || req.ProviderID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email and ProviderID are required",
		})
	}
	db := database.GetConnection(org.Id)
	userCollection := db.Collection("users")
	filter := bson.M{"email": req.Email}
	var user User
	err := userCollection.FindOne(ctx, filter).Decode(&user)
	if err == nil {
		// User exists, update last login and return token
		claims := utils.GetNewJWTClaim()
		claims["id"] = user.ID
		claims["role"] = user.Role
		claims["email"] = user.Email
		claims["uo_id"] = org.Id
		claims["isProfileComplete"] = user.IsProfileComplete
		token := utils.GenerateJWTToken(claims, 525600)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  "success",
			"org":     org,
			"message": "SSO login successful",
			"token":   token,
		})
	}
	// User doesn't exist, validate Firebase user and create new user
	firebaseUser, err := helper.ValidateFirebaseUserByEmail(req.Email, "marketPlace-serviceAccountKey.json")

	if user.Role == "Admin" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Firebase validation failed: " + err.Error(),
		})
	}

	// if err != nil {
	// 	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
	// 		"error": "Firebase validation failed: " + err.Error(),
	// 	})
	// }

	newUser := &User{
		ID:                helper.Generateuniquekey(),
		Email:             req.Email,
		Name:              firebaseUser.DisplayName,
		ProfilePicture:    firebaseUser.PhotoURL,
		CreatedAt:         time.Now(),
		IsProfileComplete: false,
	}
	_, err = userCollection.InsertOne(ctx, newUser)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	claims := utils.GetNewJWTClaim()
	claims["id"] = newUser.ID
	claims["role"] = newUser.Role
	claims["uo_id"] = org.Id
	claims["email"] = newUser.Email
	claims["isProfileComplete"] = false
	token := utils.GenerateJWTToken(claims, 525600)
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"org":     org,
		"status":  "success",
		"message": "SSO login successful",
		"token":   token,
	})
}

func HebbaSSoLoginHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
		//org.Id = "shared" // for temporary
	}
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}
	if req.Id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email and ProviderID are required",
		})
	}
	db := database.GetConnection(org.Id)
	userCollection := db.Collection("user")
	filter := bson.M{"email": req.Id}
	var user User
	err := userCollection.FindOne(ctx, filter).Decode(&user)
	if err == nil {
		// User exists, update last login and return token
		claims := utils.GetNewJWTClaim()
		claims["id"] = user.ID
		claims["role"] = user.Role
		claims["email"] = user.Email
		claims["uo_id"] = org.Id
		claims["isProfileComplete"] = user.IsProfileComplete
		token := utils.GenerateJWTToken(claims, 525600)
		response := LoginResponse{
			Name:              user.Name,
			Email:             user.Email,
			MobileNumber:      user.MobileNumber,
			UserRole:          "SA",
			UserOrg:           org,
			Token:             token,
			FirstLogin:        user.FirstLogin,
			IsProfileComplete: helper.BoolPtr(user.IsProfileComplete),
		}

		return shared.SuccessResponse(c, fiber.Map{
			"Message":       "Login Successfully",
			"LoginResponse": response,
		})
	}
	// User doesn't exist, validate Firebase user and create new user
	firebaseUser, err := helper.ValidateFirebaseUserByEmail(req.Id, "hebba-firebase.json")
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Firebase validation failed: " + err.Error(),
		})
	}

	newUser := &User{
		ID:                helper.Generateuniquekey(),
		Email:             req.Id,
		Name:              firebaseUser.DisplayName,
		ProfilePicture:    firebaseUser.PhotoURL,
		CreatedAt:         time.Now(),
		IsProfileComplete: false,
	}
	_, err = userCollection.InsertOne(ctx, newUser)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	claims := utils.GetNewJWTClaim()
	claims["id"] = newUser.ID
	claims["role"] = newUser.Role
	claims["uo_id"] = org.Id
	claims["email"] = newUser.Email
	claims["isProfileComplete"] = false
	token := utils.GenerateJWTToken(claims, 525600)
	response := LoginResponse{
		Name:              firebaseUser.DisplayName,
		Email:             firebaseUser.Email,
		MobileNumber:      firebaseUser.PhoneNumber,
		UserRole:          "SA",
		UserOrg:           org,
		Token:             token,
		FirstLogin:        true,
		IsProfileComplete: helper.BoolPtr(false),
	}

	return shared.SuccessResponse(c, fiber.Map{
		"Message":       "Login Successfully",
		"LoginResponse": response,
	})
}

// func postResetPasswordHandler(c *fiber.Ctx) error {
// 	org, exists := helper.GetOrg(c)
// 	if !exists {

// 		return shared.BadRequest("Organization Id missing")
// 	}

// 	// userToken := utils.GetUserTokenValue(c)
// 	ctx := context.Background()
// 	var req ResetPasswordRequest

// 	err := c.BodyParser(&req)
// 	if err != nil {
// 		shared.BadRequest("Invalid")
// 	}

// 	result := database.GetConnection(org.Id).Collection("user").FindOne(ctx, bson.M{
// 		"_id": req.Id,
// 	})
// 	var user bson.M
// 	err = result.Decode(&user)
// 	if err == mongo.ErrNoDocuments {
// 		shared.InternalServerError("User Id not available")

// 	}
// 	if err != nil {
// 		log.Errorf("Error getting user :%s error:%s", req.Id, err.Error())

// 		shared.InternalServerError("Internal server Error")

// 	}

// 	// if userToken.UserRole == "" {
// 	//Check the old password
// 	// if !helper.CheckPassword(req.OldPwd, primitive.Binary(user["pwd"].(primitive.Binary)).Data) {

// 	// 	shared.BadRequest("Given user id and old password mismated")
// 	// }
// 	// }
// 	if !helper.CheckPassword(req.OldPwd, primitive.Binary(user["pwd"].(primitive.Binary)).Data) {
// 		return shared.BadRequest("Invalid User Password")
// 	}
// 	passwordHash, _ := helper.GeneratePasswordHash(req.NewPwd)
// 	_, err = database.GetConnection(org.Id).Collection("user").UpdateByID(ctx,
// 		req.Id,
// 		bson.M{"$set": bson.M{"pwd": passwordHash}},
// 	)
// 	if err != nil {
// 		log.Errorf("Error Reset password for :%s error:%s", req.Id, err.Error())
// 		shared.SendErrorResponse(c, http.StatusInternalServerError)

//		}
//		shared.SuccessResponse(c, "Password Updated")
//		// automatically return 200 success (http.StatusOK) - no need to send explictly
//		return nil
//	}

func GenerateOtpHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}

	Email := c.Params("email_id")
	ctx := context.Background()
	otp := rand.Intn(90000) + 10000
	payload := bson.M{
		"$set": bson.M{
			"otp": otp,
		},
	}

	result := database.GetConnection(org.Id).Collection("user").FindOneAndUpdate(ctx, bson.M{
		"_id": Email,
	}, payload,
	)

	var user bson.M
	err := result.Decode(&user)
	if err != nil {
		return shared.InternalServerError("User Id not available")
	}

	name := user["name"].(string)

	filter := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"title", "user"},
					{"emailtype", "otp_verification"},
				},
			},
		},
	}

	Response, err := helper.GetAggregateQueryResult(org.Id, "email_template", filter)
	if err != nil {
		fmt.Println("Err",
			err.Error(),
		)

	}

	body := strings.ReplaceAll(Response[0]["template"].(string), "{{otp}}", strconv.Itoa(otp))
	body = strings.ReplaceAll(body, "{{name}}", name)
	if err := helper.SimpleEmailHandler(Email, os.Getenv("CLIENT_EMAIL"), "Password Resetting", body); err == nil {

	} else {
		return shared.BadRequest("Email sending failed:")
	}

	// Send a mail with Otp
	return shared.SuccessResponse(c, fiber.Map{
		"message": "Otp Sent successfully",
	})

}

func ValidateOtpHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}

	// userToken := utils.GetUserTokenValue(c)
	ctx := context.Background()
	var req OtpRequest

	err := c.BodyParser(&req)
	if err != nil {
		shared.BadRequest("Invalid")
	}

	result := database.GetConnection(org.Id).Collection("user").FindOne(ctx, bson.M{
		"_id": req.Email,
		"otp": req.Otp,
	},
	)

	var user bson.M
	err = result.Decode(&user)
	if err == mongo.ErrNoDocuments {

		return shared.InternalServerError("invalid Otp")

	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  200,
		"message": "User verified",
	})

}

func postResetPasswordHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// userToken := utils.GetUserTokenValue(c)
	ctx := context.Background()
	var req ResetPasswordRequest

	err := c.BodyParser(&req)
	if err != nil {
		shared.BadRequest("Invalid")
	}

	result := database.GetConnection(org.Id).Collection("user").FindOne(ctx, bson.M{
		"email": req.Id,
	})
	var user bson.M
	err = result.Decode(&user)
	if err == mongo.ErrNoDocuments {
		shared.InternalServerError("User Id not available")

	}
	fmt.Println(user)
	if err != nil {
		log.Errorf("Error getting user :%s error:%s", req.Id, err.Error())
		shared.InternalServerError("Internal server Error")
	}

	// if userToken.UserRole == "" {
	//Check the old password
	// if !helper.CheckPassword(req.OldPwd, primitive.Binary(user["pwd"].(primitive.Binary)).Data) {

	// 	shared.BadRequest("Given user id and old password mismated")
	// }
	// }

	if !helper.CheckPassword(req.OldPwd, primitive.Binary(user["pwd"].(primitive.Binary)).Data) {
		return shared.BadRequest("Invalid User Password")
	}

	passwordHash, _ := helper.GeneratePasswordHash(req.NewPwd)
	fmt.Println(string(passwordHash))

	data, err := database.GetConnection("shared").Collection("user").UpdateOne(ctx,
		bson.M{
			"email": req.Id,
		},
		bson.M{"$set": bson.M{"pwd": passwordHash}},
	)
	if err != nil {
		log.Errorf("Error Reset password for :%s error:%s", req.Id, err.Error())
		shared.SendErrorResponse(c, http.StatusInternalServerError)

	}
	return shared.SuccessResponse(c, data)
	// automatically return 200 success (http.StatusOK) - no need to send explictly
	// return nil
}

func postPasswordChangeHandler(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}

	// userToken := utils.GetUserTokenValue(c)
	ctx := context.Background()
	var req forgetPasswordRequestDto

	err := c.BodyParser(&req)
	if err != nil {
		shared.BadRequest("Invalid")
	}
	if req.NewPwd != req.ConfirmPwd {
		return shared.BadRequest("Please Check Confirm Password")

	}
	user, err := helper.FindOneDocument(org.Id, "user", bson.D{{"_id", req.Id}})
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	if user == nil {
		return shared.BadRequest("Invalid User Id")

	}

	passwordHash, _ := helper.GeneratePasswordHash(req.NewPwd)
	_, err = database.GetConnection(org.Id).Collection("user").UpdateByID(ctx,
		req.Id,
		bson.M{"$set": bson.M{"pwd": passwordHash}, "$unset": bson.M{"otp": ""}},
	)
	if err != nil {
		log.Errorf("Error Reset password for :%s error:%s", req.Id, err.Error())
		shared.SendErrorResponse(c, http.StatusInternalServerError)

	}
	shared.SuccessResponse(c, "Password Updated")

	return nil
}

//todo Currently not use
// func MobileOtpGenerate(c *fiber.Ctx) error {
// 	var req bson.M
// 	otpInfo := make(map[string]interface{})
// 	resp := make(map[string]string)
// 	orgId := c.Get("OrgId")
// 	if orgId == "" {
// 		return helper.BadRequest("Organization Id missing")
// 	}
// 	err := c.BodyParser(&req)
// 	_, isMobileNumExist := req["mobile"]
// 	if !isMobileNumExist {
// 		return helper.BadRequest("Invalid request, Unable to parse Mobile number")
// 	}
// 	mobile := req["mobile"].(string)
// 	result := database.GetConnection(orgId).Collection("user").FindOne(ctx,
// 		bson.M{
// 			"mobile":        req["mobile"].(string),
// 			"mobile_access": "Y",
// 			"status":        "A",
// 		})
// 	var user bson.M
// 	err = result.Decode(&user)
// 	if err == mongo.ErrNoDocuments {
// 		return helper.BadRequest("User Id not available")
// 	}
// 	if err != nil {
// 		return helper.BadRequest("Internal server Error")
// 	}
// 	id := uuid.New().String()
// 	otp := helper.GetOTPValue()
// 	helper.SmsInitOTP(req["mobile"].(string), otp)
// 	otpInfo["_id"] = id
// 	otpInfo["otp"] = otp
// 	otpInfo["otp_expired"] = false
// 	otpInfo["otp_verified"] = false
// 	if req["device_info"] != nil {
// 		otpInfo["device_info"] = req["device_info"]
// 	}
// 	otpInfo["created_by"] = req["mobile"].(string)
// 	otpInfo["created_on"] = time.Now()
// 	_, err = database.GetConnection(orgId).Collection("user").UpdateOne(
// 		ctx,
// 		bson.M{"mobile": mobile},
// 		bson.M{
// 			"$addToSet": bson.M{
// 				"otp_info": otpInfo,
// 			},
// 			//"$set": res,
// 		}, options.Update().SetUpsert(false))
// 	if err != nil {
// 		log.Print(err.Error())
// 	}
// 	//_, err = database.GetConnection(orgId).Collection("user_device").InsertOne(ctx, req)
// 	if err != nil {
// 		return helper.BadRequest(err.Error())
// 	}
// 	//resp = OTP{AuthKey: id, Otp: otp}
// 	resp["auth_key"] = id
// 	return helper.SuccessResponse(c, resp)
// }

//todo Currently not use

// func MobileOtpValidation(c *fiber.Ctx) error {
// 	var req OTP
// 	orgId := c.Get("OrgId")
// 	if orgId == "" {
// 		return helper.BadRequest("Organization Id missing")
// 	}
// 	//ctx := context.Background()
// 	err := c.BodyParser(&req)
// 	if err != nil || req.Otp == 0 || req.AuthKey == "" {
// 		return helper.BadRequest("Invalid request, Unable to parse OTP or Auth Key")
// 	}
// 	filter := bson.M{
// 		"otp_info": bson.M{
// 			"$elemMatch": bson.M{
// 				"otp_expired":  false,
// 				"otp_verified": false,
// 				"_id":          req.AuthKey,
// 				"otp":          req.Otp,
// 				"created_on": bson.M{
// 					"$gte": time.Now().Add(-5 * time.Minute),
// 					"$lt":  time.Now(),
// 				},
// 			},
// 		},
// 	}

// 	// Run the query and retrieve the matching document
// 	var result bson.M
// 	err = database.GetConnection(orgId).Collection("user").FindOne(ctx, filter).Decode(&result)
// 	if err == mongo.ErrNoDocuments {
// 		return helper.BadRequest("Invalid OTP")
// 	}
// 	if err != nil {
// 		return helper.BadRequest("Internal server Error")
// 	}
// 	updateDoc := bson.M{
// 		"$set": bson.M{
// 			"otp_info.$[].otp_expired":      true,
// 			"otp_info.$[elem].otp_verified": true,
// 			"otp_info.$[elem].updated_by":   result["mobile"].(string),
// 			"otp_info.$[elem].updated_on":   time.Now(),
// 		},
// 	}

// 	// Define the filter to match the document containing the array
// 	updateFilter := bson.M{"_id": result["_id"].(string)}

// 	// Define the array element positional operator
// 	arrayFilters := options.Update().SetArrayFilters(options.ArrayFilters{
// 		Filters: []interface{}{bson.M{"elem._id": req.AuthKey}},
// 	})
// 	_, err = database.GetConnection(orgId).Collection("user").UpdateOne(ctx, updateFilter, updateDoc, arrayFilters)
// 	if err != nil {
// 		log.Print(err.Error())
// 	}
// 	claims := helper.GetNewJWTClaim()
// 	claims["id"] = result["_id"]
// 	claims["role"] = result["role"]
// 	claims["org_id"] = orgId
// 	// claims["org_group"] = orgId
// 	userName := result["email"]
// 	if userName == nil {
// 		userName = result["name"]
// 	}
// 	token := helper.GenerateJWTToken(claims, 365*10)
// 	response := OTPResponse{token, result["_id"].(string)}
// 	return helper.SuccessResponse(c, response)
// }

func OrgConfigHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Org not found")
	}
	return shared.SuccessResponse(c, org)
}

func RegisterAllUser(c *fiber.Ctx) error {

	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	//userToken := utils.GetUserTokenValue(c)
	//collectionName := c.Params("model_name")
	var inputData map[string]interface{}

	// Get collection name based on Model Name
	collectionName, err := helper.CollectionNameGet("user", org.Id)
	if err != nil {
		return shared.BadRequest("Invalid CollectionName")
	}
	err = c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	// Validate Fields from body
	// inputData, errmsg := helper.InsertValidateInDatamodel(collectionName, string(c.Body()), org.Id)
	// if errmsg != nil {
	// 	err := helper.GenerateErrorMessage(errmsg)
	// 	// Return the error message map as part of BadRequest response
	// 	return shared.BadRequest(err)
	// }
	// var firstName string
	// var lastName string
	// var fullname string
	email := inputData["email"].(string)
	mobile := inputData["mobile_number"]
	userFindPipeline := bson.A{
		bson.D{
			{"$match",
				bson.D{
					{"$and",
						bson.A{
							bson.D{{"first_time_user", false}},
						},
					},
				},
			},
		},
		bson.D{
			{"$match",
				bson.D{
					{"$or",
						bson.A{
							bson.D{{"email", email}},
							bson.D{{"mobile_number", mobile}},
						},
					},
				},
			},
		},
		bson.D{
			{"$project",
				bson.D{
					{"_id", 0},
					{"emailExists",
						bson.D{
							{"$cond",
								bson.A{
									bson.D{
										{"$eq",
											bson.A{
												"$email",
												email,
											},
										},
									},
									true,
									false,
								},
							},
						},
					},
					{"mobileExists",
						bson.D{
							{"$cond",
								bson.A{
									bson.D{
										{"$eq",
											bson.A{
												"$mobile_number",
												mobile,
											},
										},
									},
									true,
									false,
								},
							},
						},
					},
				},
			},
		},
	}
	user, err := helper.GetAggregateQueryResult(org.Id, collectionName, userFindPipeline)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	if len(user) > 0 {
		userMap := user[0]
		emailExists := userMap["emailExists"].(bool)
		mobileExists := userMap["mobileExists"].(bool)
		if emailExists && mobileExists {
			return shared.BadRequest("Email and Mobile Number Already Registered")
		} else if emailExists {
			return shared.BadRequest("Email Already Registered")
		} else if mobileExists {
			return shared.BadRequest("Mobile Number Already Registered")
		}
	}

	// if user == nil {
	// 	return shared.BadRequest("Invalid User Id")

	// }

	rand := helper.GenerateRandomString(6)
	pwd, _ := helper.GeneratePasswordHash(rand)

	inputData["pwd"] = pwd
	inputData["factory_id"] = "IVCFAC--021"
	inputData["unit_id"] = "6a6UNI--003"
	inputData["role"] = "OA"
	inputData["first_time_user"] = true
	// if inputData["first_name"] != nil {
	// 	firstName = inputData["first_name"].(string)
	// }
	// if inputData["last_name"] != nil {
	// 	lastName = inputData["last_name"].(string)
	// }
	// fullname = firstName + " " + lastName
	// inputData["name"] = fullname
	database.GetConnection(org.Id).Collection(collectionName).DeleteOne(context.Background(), bson.M{"email": email})
	inputData["_id"] = uuid.New().String()
	_, err = database.GetConnection(org.Id).Collection(collectionName).InsertOne(ctx, inputData)
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database " + err.Error())
	}

	// Get organization data to retrieve domain name
	var orgData map[string]interface{}
	err = database.GetConnection("shared").Collection("organization").FindOne(context.Background(), bson.M{"_id": org.Id}).Decode(&orgData)
	domainName := "app" // Default to 'app' instead of 'demo'
	if err == nil && orgData["domain_name"] != nil {
		if dn, ok := orgData["domain_name"].(string); ok && dn != "" {
			domainName = dn
		}
	}
	loginURL := "https://" + domainName + ".kajupro.com/"

	err = helper.SendOnBoardingMail(loginURL, rand, email)
	if err != nil {
		return shared.InternalServerError("Mail Not Sent")
	}
	shared.SuccessResponse(c, "User Registered Successfully")

	return nil
}

func GetOrgData(c *fiber.Ctx) error {
	// Get organization
	// st := time.Now()
	// fmt.Println(st)
	org, exists := helper.GetOrg(c)
	orgId := c.Params("ID")

	var filter bson.M
	if orgId != "" {
		filter = bson.M{"_id": orgId}
	} else if exists {
		filter = bson.M{"_id": org.Id}
	} else {
		return shared.BadRequest("Organization Id missing")
	}
	var orgData map[string]interface{}
	database.GetConnection("shared").Collection("organization").FindOne(context.Background(), filter).Decode(&orgData)
	if orgData == nil {
		return shared.BadRequest("Organization Not Found")
	}

	return shared.SuccessResponse(c, orgData)
}

func GetCustomerData(c *fiber.Ctx) error {
	// Get organization
	// st := time.Now()
	// fmt.Println(st)
	// org, exists := helper.GetOrg(c)
	// if !exists {
	// 	return shared.BadRequest("Organization Id missing")
	// }
	orgId := c.Params("ID")
	var filter bson.M

	filter = bson.M{"_id": orgId}

	var orgData map[string]interface{}
	database.GetConnection("shared").Collection("temporary_user").FindOne(context.Background(), filter).Decode(&orgData)
	if orgData == nil {
		return shared.BadRequest("User Not Found")
	}

	return shared.SuccessResponse(c, orgData)
}

func GetOrgaData(c *fiber.Ctx) error {
	// Get organization
	// org, exists := helper.GetOrg(c)
	// if !exists {
	// 	return shared.BadRequest("Organization Id missing")
	// }
	installcode := c.Params("installCode")
	fmt.Println(installcode)
	var orgData map[string]interface{}
	//database.GetConnection("shared").Collection("organization").FindOne(context.Background(), bson.M{"install_code": installcode}).Decode(&orgData)
	database.SharedDB.Collection("organization").FindOne(context.Background(), bson.M{"install_code": installcode}).Decode(&orgData)
	if orgData == nil {
		return shared.BadRequest("Organization Not Found")
	}

	return shared.SuccessResponse(c, orgData)
}

func OrgRegister(c *fiber.Ctx) error {

	// Get organization
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := "organization"
	var inputData map[string]interface{}

	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	fmt.Printf("DEBUG: full inputData: %+v\n", inputData)
	if collectionName == "organization" {
		org.Id = "shared"
	}
	// Update Date Object
	helper.UpdateDateObject(inputData)
	// helper.HandleIDGeneration(inputData, org.Id, collectionName)

	if _, ok := inputData["status"]; !ok || inputData["status"] == "" {
		inputData["status"] = "Active"
	}

	inputData["created_on"] = time.Now()

	nxtSeq, _ := helper.GetNextSeqNumber("ORG", org.Id)
	year := time.Now().Year()

	orgId := "ORG-" + helper.ToString(year) + "-" + helper.ToString(nxtSeq)
	inputData["_id"] = orgId
	inputData["firstLogin"] = true
	if collectionName == "organization" {

		emailIdVal, ok := inputData["email_id"]
		if !ok {
			return shared.BadRequest("email_id is required")
		}
		emailId, ok := emailIdVal.(string)
		if !ok {
			return shared.BadRequest("email_id must be a string")
		}
		var userData map[string]interface{}
		err := database.GetConnection("shared").Collection("temporary_user").FindOne(context.Background(), bson.M{"_id": emailId}).Decode(&userData)
		if err != nil {
			isEmailVerified, _ := inputData["is_email_verified"].(bool)
			if !isEmailVerified {
				return shared.InternalServerError("No user Found")
			}
		} else {
			// Store userData for later use
			inputData["temp_user_data"] = userData
		}
	}

	// Generate random password
	password := helper.GenerateRandomString(8)
	inputData["password"] = password

	// Insert data into the database
	res, err := Insert("shared", collectionName, inputData)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	emailIdVal, ok := inputData["email_id"]
	if !ok {
		return shared.BadRequest("email_id is required")
	}
	emailId, ok := emailIdVal.(string)
	if !ok {
		return shared.BadRequest("email_id must be a string")
	}

	// Get first name from inputData
	firstName := "User"
	if val, exists := inputData["first_name"]; exists && val != nil {
		if fn, ok := val.(string); ok && fn != "" {
			firstName = fn
		}
	}

	// Get domain name from inputData
	// Domain name generation removed - email will be sent only when organization is approved via PUT
	// domainName := "app" // Default to 'app' instead of 'demo'
	// if val, exists := inputData["domain_name"]; exists && val != nil {
	// 	if dn, ok := val.(string); ok && dn != "" {
	// 		domainName = dn
	// 	}
	// }
	// loginURL := "https://" + domainName + ".kajupro.com/"

	// Get last name from inputData
	lastName := ""
	if val, exists := inputData["last_name"]; exists && val != nil {
		if ln, ok := val.(string); ok && ln != "" {
			lastName = ln
		}
	}

	pwdHash, _ := helper.GeneratePasswordHash(password)

	// Prepare Admin User Data
	adminUser := map[string]interface{}{
		"_id":                  uuid.New().String(),
		"name":                 firstName + " " + lastName,
		"mobile_number":        inputData["mobile_number"],
		"pwd":                  pwdHash,
		"email":                emailId,
		"role":                 "OA", // Organization Admin
		"status":               "Active",
		"created_on":           time.Now(),
		"org_id":               orgId,
		"first_login":          true,
		"is_profile_completed": false,
	}

	// Insert user into the database (defaults to shared if org DB not yet provisioned)
	Insert("shared", "user", adminUser)

	// Clean up temporary user record if it exists
	database.GetConnection("shared").Collection("temporary_user").DeleteOne(context.Background(), bson.M{"_id": emailId})

	// Email sending removed - will be sent only when organization is approved via PUT /entities/organization/{ID}
	// helper.SendOnBoardingMail(loginURL, password, emailId)

	return shared.SuccessResponse(c, fiber.Map{
		"message":   "Organization and Admin User registered successfully",
		"insert ID": res.InsertedID,
	})

}

func InsertUser(inputData map[string]interface{}, orgId interface{}) error {

	firstName := inputData["first_name"].(string)
	lastName := inputData["last_name"].(string)
	mobileNo := inputData["mobile_number"].(string)
	emailId := inputData["email_id"].(string)
	password := inputData["password"].(string)
	Id := inputData["_id"].(string)
	// rand := helper.GenerateRandomString(8)
	pwd, err := helper.GeneratePasswordHash(password)

	if err != nil {
		return err
	}

	createUserData := map[string]interface{}{
		"_id":           uuid.New().String(),
		"name":          firstName + " " + lastName,
		"mobile_number": mobileNo,
		"pwd":           pwd,
		"email":         emailId,
		"role":          "OA",
		"status":        "Active",
		"user_type":     "687788232ae447bb0d41c72b",
		"created_on":    time.Now(),
		"org_id":        Id,
	}
	val, ok := orgId.(string)
	if ok {
		fmt.Println("Converted string:", val)
	} else {
		fmt.Println("Conversion failed")
	}

	Insert(val, "user", createUserData)
	database.GetConnection("shared").Collection("temporary_user").DeleteOne(context.Background(), bson.M{"_id": emailId})
	helper.SendOnBoardingMail("https://cerp.kriyatec.com/", password, emailId)
	return nil
}

func Insert(orgId string, collectionName string, inputData map[string]interface{}) (*mongo.InsertOneResult, error) {
	res, err := database.GetConnection(orgId).Collection(collectionName).InsertOne(ctx, inputData)

	if err != nil {
		var dupValue string
		errors, id, Name := IsDup(err)
		fmt.Println(errors)
		if errors {
			if id != "" {
				dupValue = id
			}
			if Name != "" {
				dupValue = Name
			}
			return res, fiber.NewError(200, "Duplicate Value : "+dupValue)
		}

		return res, err
	}
	return res, nil
}

func IsDup(err error) (bool, string, string) {
	if wes, ok := err.(mongo.WriteException); ok {
		for i := range wes.WriteErrors {
			if wes.WriteErrors[i].Code == 11000 || wes.WriteErrors[i].Code == 11001 || wes.WriteErrors[i].Code == 12582 || wes.WriteErrors[i].Code == 16460 {
				// Extract the values associated with "_id" and "name" using regular expressions
				errorMessage := wes.WriteErrors[i].Error()

				// Extract the "_id" value
				reID := regexp.MustCompile(`_id: "([^"]+)"`)
				matchesID := reID.FindStringSubmatch(errorMessage)
				id := ""
				if len(matchesID) == 2 {
					id = matchesID[1]
				}

				// Extract the "name" value
				reName := regexp.MustCompile(`name: "([^"]+)"`)
				matchesName := reName.FindStringSubmatch(errorMessage)
				name := ""
				if len(matchesName) == 2 {
					name = matchesName[1]
				}

				return true, id, name
			}
		}
	}
	return false, "", ""
}

func SendOtp(c *fiber.Ctx) error {
	var req map[string]interface{}

	err := c.BodyParser(&req)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	emailId := req["email_id"].(string)

	filter := bson.M{"email_id": emailId}

	var userData map[string]interface{}

	database.GetConnection("shared").Collection("user").FindOne(context.Background(), filter).Decode(&userData)

	if userData != nil {
		return fmt.Errorf("Email Already Exists")
	}

	err = helper.SendOTPEmail(req)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Verification Code Sent to Your Email",
	})
}

func VerifyOTP(c *fiber.Ctx) error {
	var req map[string]interface{}

	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest(err.Error())
	}

	emailId, ok := req["email_id"].(string)
	if !ok {
		return shared.BadRequest("Invalid or missing email_id")
	}

	otp := helper.ToInt(req["otp"])

	filter := bson.M{"_id": emailId}
	var userData map[string]interface{}

	err := database.GetConnection("shared").Collection("temporary_user").FindOne(context.Background(), filter).Decode(&userData)
	if err != nil {
		return shared.BadRequest("OTP not found or already used")
	}

	// Check if already verified
	if verified, ok := userData["verified"].(bool); ok && verified {
		return shared.InternalServerError("OTP already used")
	}

	// Compare OTP
	userOtp := helper.ToInt(userData["otp"])
	if otp != userOtp {
		return shared.InternalServerError("Invalid OTP")
	}

	// Check expiry (5 minutes)
	var issuedTime time.Time
	switch t := userData["issued_on"].(type) {
	case primitive.DateTime:
		issuedTime = t.Time()
	case time.Time:
		issuedTime = t
	default:
		return shared.InternalServerError("Invalid issued_on format")
	}

	if time.Since(issuedTime) > 5*time.Minute {
		return shared.InternalServerError("OTP has expired")
	}

	// Mark OTP as used
	update := bson.M{"$set": bson.M{"verified": true}}
	_, err = database.GetConnection("shared").Collection("temporary_user").UpdateOne(context.Background(), filter, update)
	if err != nil {
		return shared.InternalServerError("Failed to update OTP status")
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "OTP Verified Successfully",
	})
}

func RegisterUserWithSSO(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	ctx := context.Background()

	// Parse incoming request
	var req SSOUser
	if err := c.BodyParser(&req); err != nil {
		return shared.BadRequest(err.Error())
	}

	collection := database.GetConnection(org.Id).Collection("user")

	// Check if user already exists
	var user bson.M
	err := collection.FindOne(ctx, bson.M{"email_id": req.Email}).Decode(&user)
	userExists := err == nil

	if userExists {
		// User already exists, just generate JWT and return
		claims := utils.GetNewJWTClaim()
		claims["id"] = user["_id"]
		claims["role"] = user["role"]
		claims["uo_id"] = org.Id

		token := utils.GenerateJWTToken(claims, 525600) // expires in minutes (~1 year)

		firstLogin := false
		if fl, ok := user["first_login"].(bool); ok && fl {
			firstLogin = true
			updateData := map[string]interface{}{
				"first_login": false,
			}
			helper.UpdateDataToDb(org.Id, bson.M{"email_id": req.Email}, updateData, "user")
		}

		isProfileComplete := false
		if ipc, ok := user["is_profile_complete"].(bool); ok {
			isProfileComplete = ipc
		}

		response := LoginResponse{
			Name:              user["name"].(string),
			Email:             user["email"].(string),
			MobileNumber:      user["mobile_number"].(string),
			UserRole:          user["role"].(string),
			UserOrg:           org,
			Token:             token,
			FirstLogin:        firstLogin,
			IsProfileComplete: helper.BoolPtr(isProfileComplete),
		}

		return shared.SuccessResponse(c, fiber.Map{
			"Message":       "Login Successfully",
			"LoginResponse": response,
		})
	}

	// User does not exist, create new user
	req.Id = helper.Generateuniquekey()
	req.Status = "Active"
	req.CreatedOn = time.Now().UTC()

	// Insert new user
	_, err = collection.InsertOne(ctx, req)
	if err != nil {
		return shared.BadRequest(err.Error())
	}

	// Convert struct to bson.M for JWT claims
	bsonBytes, _ := bson.Marshal(req)
	bson.Unmarshal(bsonBytes, &user)

	// Generate JWT
	claims := utils.GetNewJWTClaim()
	claims["id"] = user["_id"]
	claims["role"] = req.Role
	claims["uo_id"] = org.Id

	token := utils.GenerateJWTToken(claims, 525600) // expires in minutes (~1 year)

	response := LoginResponse{
		Name:              req.Name,
		Email:             req.Email,
		MobileNumber:      req.MobileNumber,
		UserRole:          req.Role,
		UserOrg:           org,
		Token:             token,
		FirstLogin:        true,
		IsProfileComplete: helper.BoolPtr(false),
	}

	return shared.SuccessResponse(c, fiber.Map{
		"Message":       "Login Successfully",
		"LoginResponse": response,
	})
}

func getPublicOrganizationHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return shared.BadRequest("ID is required")
	}

	var result map[string]interface{}
	err := database.SharedDB.Collection("organization").FindOne(context.Background(), bson.M{"_id": id}).Decode(&result)
	if err != nil {
		return shared.BadRequest("Organization not found")
	}

	return shared.SuccessResponse(c, result)
}

func getPublicQuestionsHandler(c *fiber.Ctx) error {
	var dbConfig struct {
		OrgId string `bson:"org_id"`
	}
	err := database.SharedDB.Collection("db_config").FindOne(context.Background(), bson.M{"db_name": "cerp_new_test", "status": "A"}).Decode(&dbConfig)
	if err != nil {
		return shared.InternalServerError("Could not find org config for cerp_new_test: " + err.Error())
	}

	fmt.Printf("[PUBLIC QUESTIONS] using org_id: %s\n", dbConfig.OrgId)

	cursor, err := database.GetConnection(dbConfig.OrgId).Collection("questions").Find(context.Background(), bson.M{})
	if err != nil {
		return shared.InternalServerError("Failed to fetch questions: " + err.Error())
	}
	defer cursor.Close(context.Background())

	questions := make([]map[string]interface{}, 0)
	if err = cursor.All(context.Background(), &questions); err != nil {
		return shared.InternalServerError("Failed to parse questions: " + err.Error())
	}

	return shared.SuccessResponse(c, questions)
}

func getAvailableCustomerModule(c *fiber.Ctx) error {
	var requestBody helper.PaginationRequest

	if err := c.BodyParser(&requestBody); err != nil {
		return shared.BadRequest("Invalid request body")
	}

	var pipeline []primitive.M
	pipeline = helper.MasterAggregationPipeline(requestBody, c)

	// Handle new filter structure
	if len(requestBody.Filter) > 0 {
		for _, filterGroup := range requestBody.Filter {
			if len(filterGroup.Conditions) > 0 {
				var conditions []bson.M
				for _, condition := range filterGroup.Conditions {
					if condition.Operator == "EQUALS" {
						conditions = append(conditions, bson.M{
							condition.Column: condition.Value,
						})
					}
				}
				if len(conditions) > 0 {
					if filterGroup.Clause == "AND" {
						pipeline = append(pipeline, bson.M{"$match": bson.M{"$and": conditions}})
					} else {
						pipeline = append(pipeline, bson.M{"$match": bson.M{"$or": conditions}})
					}
				}
			}
		}
	}

	if len(requestBody.Sort) > 0 {
		sortConditions := helper.BuildSortConditions(requestBody.Sort)
		pipeline = append(pipeline, sortConditions)
	}

	PagiantionPipeline := helper.PagiantionPipeline(requestBody.Start, requestBody.End)
	pipeline = append(pipeline, PagiantionPipeline)

	Response, err := helper.GetAggregateQueryResult("shared", "role_acl", pipeline)
	if err != nil {
		if cmdErr, ok := err.(mongo.CommandError); ok {
			return shared.BadRequest(cmdErr.Message)
		}
	}
	return shared.SuccessResponse(c, Response)
}
