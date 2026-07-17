package helper

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"text/template"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared/database"
)

func GenerateOTP() string {
	rand.Seed(time.Now().UnixNano())
	for {
		otp := rand.Intn(9000) + 1000 // Generate a random number between 1000 and 9999 (inclusive)
		otpStr := fmt.Sprintf("%04d", otp)
		if len(otpStr) == 4 {
			return otpStr
		}
	}
}

func ConvertOTPStringToInt(otpStr string) (int, error) {
	otpInt, err := strconv.Atoi(otpStr)
	if err != nil {
		return 0, err
	}
	return otpInt, nil
}

const otpEmailTemplate = `
<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>OTP Verification</title>
  <style>
    body {
      font-family: Arial, sans-serif;
      background-color: #f7f9fc;
      margin: 0;
      padding: 0;
    }
    .container {
      max-width: 500px;
      margin: 30px auto;
      background-color: #ffffff;
      padding: 30px;
      border-radius: 10px;
      box-shadow: 0 4px 10px rgba(0, 0, 0, 0.05);
    }
    .otp {
      font-size: 32px;
      font-weight: bold;
      color: #2d3748;
      letter-spacing: 10px;
      text-align: center;
      margin: 20px 0;
    }
    .message {
      font-size: 16px;
      color: #4a5568;
      text-align: center;
    }
    .footer {
      margin-top: 30px;
      font-size: 12px;
      color: #a0aec0;
      text-align: center;
    }
  </style>
</head>
<body>
  <div class="container">
    <h2 style="text-align:center; color:#2b6cb0;">Your OTP Code</h2>
    <p class="message">Use the following OTP to verify your email address. This code is valid for 5 minutes:</p>
    <div class="otp">{{.OTP}}</div>
    <p class="message">If you didn’t request this, please ignore this email.</p>
    <div class="footer">
      &copy; {{.Year}} Your Company. All rights reserved.
    </div>
  </div>
</body>
</html>
`

type OTPData struct {
	OTP  string
	Year int
}

func GenerateOTPEmailHTML(otp string) (string, error) {
	tmpl, err := template.New("otpEmail").Parse(otpEmailTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, OTPData{
		OTP:  otp,
		Year: time.Now().Year(),
	})
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func SendOTPEmail(userData map[string]interface{}) error {
	otp := GenerateOTP()
	orgId := "shared"
	htmlBody, err := GenerateOTPEmailHTML(otp)
	if err != nil {
		return err
	}

	intOtp, err := ConvertOTPStringToInt(otp)
	if err != nil {
		return err
	}

	err = SendEmailS(userData["email_id"].(string), os.Getenv("CLIENT_EMAIL"), "OTP Verification", htmlBody)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": userData["email_id"].(string)}

	update := bson.M{
		"$set": bson.M{
			"otp":       intOtp,
			"issued_on": time.Now(),
			"verified":  false,
		},
	}

	_, err = database.GetConnection(orgId).Collection("temporary_user").UpdateOne(
		context.Background(),
		filter,
		update,
		options.Update().SetUpsert(true),
	)
	return err
}

func SendOTPForUser(userData map[string]interface{}) error {
	otp := GenerateOTP()

	htmlBody, err := GenerateOTPEmailHTML(otp)
	if err != nil {
		return err
	}

	intOtp, err := ConvertOTPStringToInt(otp)
	if err != nil {
		return err
	}

	err = SendEmailS(userData["email_id"].(string), os.Getenv("CLIENT_EMAIL"), "OTP Verification", htmlBody)
	if err != nil {
		return err
	}

	filter := bson.M{"email_id": userData["email_id"].(string)}

	update := bson.M{
		"$set": bson.M{
			"otp": bson.M{
				"code":      intOtp,
				"issued_on": time.Now(),
				"verified":  false,
			},
		},
	}
	_, err = database.GetConnection("shared").Collection("user").UpdateOne(
		context.Background(),
		filter,
		update,
		options.Update().SetUpsert(true),
	)
	return err
}
