package authentication

import (
	"github.com/gofiber/fiber/v2"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

func SetupRoutes(app *fiber.App) {

	//without JWT Token validation (without auth)

	auth := app.Group("/auth")
	market := app.Group("/market-auth")

	auth.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Auth APIs")
	})

	auth.Post("/login", LoginHandler)
	auth.Post("/sso-login", HebbaSSoLoginHandler)
	market.Post("/sso-login", MarketSSoLoginHandler)
	//admin invite email
	market.Post("/admin-invite-email", SendAdminInviteEmailHandler)
	market.Post("/send-admin-invite", SendAdminInviteEmailHandler)
	market.Post("/send-email", SendAdminInviteEmailHandler)
	auth.Post("/sso-register", RegisterUserWithSSO)
	auth.Get("/config", OrgConfigHandler)
	auth.Get("/generate-otp/:email_id", GenerateOtpHandler)
	// auth.Post("/verify-otp", ValidateOtpHandler)
	auth.Post("/send-otp", SendOtp)
	auth.Post("/verify-otp", VerifyOTP)
	auth.Post("/get/:collectionName", getDocsHandler)
	// auth.Post("/changePassword", OrgChangePasswordHandler)
	auth.Post("/resetpassword", postResetPasswordHandler)
	auth.Post("/file/:folder/:refId", helper.StaticFileUpload)
	auth.Post("/changepassword", postPasswordChangeHandler)
	auth.Post("/admin-register", OrgRegister)
	auth.Post("/user-register", RegisterAllUser)
	auth.Get("/org-data/:ID?", GetOrgData)
	auth.Get("/get-cust-data/:ID", GetCustomerData)
	auth.Get("/get-org/:installCode", GetOrgaData)
	auth.Post("/public/getAvailableCustomerModule", getAvailableCustomerModule)
	auth.Get("/public/questions", getPublicQuestionsHandler)
	auth.Get("/public/organization/:id", getPublicOrganizationHandler)
	// auth.Get("/org/:ID")
	auth.Use(utils.JWTMiddleware())

	// Restricted Routes
	// auth.Post("/changepassword", postforgetPasswordHandler)

}

func SetupRegisterUser(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/register", "User APIs")
	r.Post("/user", RegisterAllUser)

}
