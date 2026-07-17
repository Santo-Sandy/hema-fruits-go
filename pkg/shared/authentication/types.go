package authentication

import (
	"time"

	"github.com/sirupsen/logrus"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

var loggerName string = "authentication"
var log *logrus.Entry = utils.GetLogEntry(loggerName)

// LoginRequest
type LoginRequest struct {
	Id       string `json:"id" bson:"id" validate:"required"`
	Password string `json:"password" bson:"password" validate:"required"`
}

// LoginResponse - for Login Response
type LoginResponse struct {
	Name              string              `json:"name"`
	Email             string              `json:"email"`
	MobileNumber      string              `json:"mobile_number"`
	UserRole          interface{}         `json:"role"`
	RoleData          interface{}         `json:"role_data"`
	UserOrg           helper.Organization `json:"org" bson:"org"`
	Token             string              `json:"token"`
	EmployeeID        interface{}         `json:"employee_id,omitempty"`
	FirstLogin        bool                `json:"first_login"`
	IsProfileComplete *bool               `json:"is_profile_completed,omitempty"`
}

// ResetPasswordRequestDto - Dto for reset password Request
type ResetPasswordRequest struct {
	Id     string `json:"id" bson:"id" validate:"required,id"`
	OldPwd string `json:"old_pwd" bson:"old_pwd" validate:"required"`
	NewPwd string `json:"new_pwd" bson:"new_pwd" validate:"required"`
}

type OtpRequest struct {
	Email string `json:"email" bson:"email" validate:"required,email"`
	Otp   int32  `json:"otp" bson:"otp" validate:"required"`
}

type forgetPasswordRequestDto struct {
	Id         string `json:"id" bson:"id" validate:"required,id"`
	NewPwd     string `json:"new_pwd" bson:"new_pwd" validate:"required"`
	ConfirmPwd string `json:"confirmPwd" bson:"confirmPwd" validate:"required"`
}

type AdminInviteEmailRequest struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	Link      string `json:"link"`
	ExpiresAt string `json:"expiresAt"`
	Body      string `json:"body"`
}

type SSOUser struct {
	Email        string    `json:"email" bson:"email"`
	MobileNumber string    `json:"mobile_number" bson:"mobile_number"`
	FirstName    string    `json:"first_name" bson:"first_name"`
	LastName     string    `json:"last_name" bson:"last_name"`
	CreatedOn    time.Time `json:"created_on" bson:"created_on"`
	Role         string    `json:"role"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	ProfileImage string    `json:"profile_image" bson:"profile_image"`
	ProvideBy    string    `json:"provide_by" bson:"provide_by"`
	ProvideId    string    `json:"provide_id" bson:"provide_id"`
	Id           string    `json:"_id" bson:"_id,omitempty"`
}

// ! Market SSO Login Request
type ssoRequest struct {
	Email      string `json:"email"`
	ProviderID string `json:"provider_id"`
	ProviderBy string `json:"provider_by"`
}
type User struct {
	ID                string    `json:"_id" bson:"_id"`
	Email             string    `json:"email" bson:"email"`
	Name              string    `json:"name" bson:"name"`
	MobileNumber      string    `json:"mobile_number,omitempty" bson:"mobile_number,omitempty"`
	Phone             string    `json:"phone,omitempty" bson:"phone,omitempty"`
	Role              string    `json:"role" bson:"role"`
	ProfilePicture    string    `json:"profilePicture,omitempty" bson:"profilePicture,omitempty"`
	CompanyName       string    `json:"companyName,omitempty" bson:"companyName,omitempty"`
	RegistrationType  string    `json:"registrationType,omitempty" bson:"registrationType,omitempty"`
	OfficeEmail       string    `json:"officeEmail,omitempty" bson:"officeEmail,omitempty"`
	OfficePhone       string    `json:"officePhone,omitempty" bson:"officePhone,omitempty"`
	OfficeAddress     string    `json:"officeAddress,omitempty" bson:"officeAddress,omitempty"`
	IsGSTRegistered   bool      `json:"isGstRegistered,omitempty" bson:"isGstRegistered,omitempty"`
	EstablishedYear   string    `json:"establishedYear,omitempty" bson:"establishedYear,omitempty"`
	BusinessType      string    `json:"businessType,omitempty" bson:"businessType,omitempty"`
	Description       string    `json:"description,omitempty" bson:"description,omitempty"`
	Location          *Location `json:"location,omitempty" bson:"location,omitempty"`
	FCMToken          string    `json:"fcmToken,omitempty" bson:"fcmToken,omitempty"`
	IsProfileComplete bool      `json:"isProfileComplete" bson:"isProfileComplete"`
	FirstLogin        bool      `json:"first_login" bson:"first_login"`
	CreatedAt         time.Time `json:"createdAt" bson:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt" bson:"updatedAt"`
	ProvideBy         string    `json:"provide_by,omitempty" bson:"provide_by,omitempty"`
	ProvideId         string    `json:"provide_id,omitempty" bson:"provide_id,omitempty"`
}
type Location struct {
	Country    string `bson:"country,omitempty" json:"country,omitempty"`
	Region     string `bson:"region,omitempty" json:"region,omitempty"`
	City       string `bson:"city,omitempty" json:"city,omitempty"`
	Address    string `bson:"address,omitempty" json:"address,omitempty"`
	PostalCode string `bson:"postalCode,omitempty" json:"postalCode,omitempty"`
}
