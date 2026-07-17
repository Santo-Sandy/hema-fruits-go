package helper

import (
	"fmt"
	"html"
	"os"
	"strings"

	"gopkg.in/mail.v2"
)

var ServerConfig map[string]EmailServerConfig

//	func Init(orgId string) EmailServerConfig {
//		var config EmailServerConfig
//		err := database.SharedDB.Collection("email_config").FindOne(context.Background(), bson.M{"org_id": orgId}).Decode(&config)
//		if err != nil {
//			println(err)
//		}
//		return config
//	}

func SimpleEmailHandler(recipientEmail string, senderEmail string, subject string, body string) error {
	email := mail.NewMessage()
	email.SetHeader("From", senderEmail)
	email.SetHeader("To", recipientEmail)

	email.SetHeader("Subject", subject)
	email.SetBody("text/html", body)

	sendinmail := mail.NewDialer("smtp.gmail.com", 587, senderEmail, os.Getenv("CLIENT_EMAIL_PASSWORD"))

	err := sendinmail.DialAndSend(email)
	if err != nil {
		return err
	}

	return nil
}

// func SendEmail(orgId string, to []string, cc []string, subject string, htmlBody string) bool {
// 	config := ServerConfig[orgId]
// 	if config.Host == "" {
// 		config = Init(orgId)
// 	}

// 	server := mail.NewSMTPClient()
// 	// SMTP Server
// 	server.Host = config.Host
// 	server.Port = config.Port
// 	server.Username = config.UserName
// 	server.Password = config.Password
// 	server.Encryption = mail.EncryptionSTARTTLS
// 	// serv/er.Encryption = mail.Encryption(mail.AuthNone)

// 	// Since v2.3.0 you can specified authentication type:
// 	// - PLAIN (default)
// 	// - LOGIN
// 	// - CRAM-MD5
// 	// - None

// 	server.Authentication = mail.AuthPlain

// 	// Variable to keep alive connection
// 	server.KeepAlive = false

// 	// Timeout for connect to SMTP Server
// 	// server.ConnectTimeout = 10 * time.Second


// 	// Timeout for send the data and wait respond
// 	// server.SendTimeout = 10 * time.Second

// 	// Set TLSConfig to provide custom TLS configuration. For example,
// 	// to skip TLS verification (useful for testing):
// 	server.TLSConfig = &tls.Config{
// 		InsecureSkipVerify: true,
// 	}

// 	// SMTP client

// 	smtpClient, err := server.Connect()
// 	if err != nil {
// 		println(err.Error())
// 		return false
// 	}

// 	// New email simple html with inline and CC
// 	email := mail.NewMSG()
// 	email.SetFrom(config.UserName).
// 		SetReplyTo(config.UserName).
// 		AddTo(to...).
// 		AddCc(cc...).
// 		SetSubject(subject).
// 		SetBody(mail.TextHTML, htmlBody)

// 	// also you can add body from []byte with SetBodyData, example:
// 	email.SetBodyData(mail.TextHTML, []byte(htmlBody))
// 	// or alternative part
// 	email.AddAlternativeData(mail.TextHTML, []byte(htmlBody))

// 	// add inline
// 	email.Attach(&mail.File{FilePath: "/path/to/image.png", Name: "Gopher.png", Inline: true})

// 	// you can add dkim signature to the email.
// 	// to add dkim, you need a private key already created one.
// 	// if privateKey != "" {
// 		options := dkim.NewSigOptions()
// 		// options.PrivateKey = []byte(privateKey)
// 		options.Domain = "example.com"
// 		options.Selector = "default"
// 		options.SignatureExpireIn = 3600
// 		options.Headers = []string{"from", "date", "mime-version", "received", "received"}
// 		options.AddSignatureTimestamp = true
// 		options.Canonicalization = "relaxed/relaxed"

// 		email.SetDkim(options)
// 	// }

// 	// always check error after send
// 	if email.Error != nil {
// 		println(email.Error.Error())
// 		return false
// 	}

// 	// Call Send and pass the client
// 	err = email.Send(smtpClient)
// 	if err != nil {
// 		println(err.Error())
// 		return false
// 	} else {
// 		return true
// 	}

// }

func SendEmailS(recipientEmail string, senderEmail string, subject string, body string) error {
	email := mail.NewMessage()
	email.SetHeader("From", senderEmail)
	email.SetHeader("To", recipientEmail)

	email.SetHeader("Subject", subject)
	email.SetBody("text/html", body)

	sendinmail := mail.NewDialer("smtp.gmail.com", 587, senderEmail, os.Getenv("CLIENT_EMAIL_PASSWORD"))

	err := sendinmail.DialAndSend(email)
	if err != nil {
		return err
	}

	return nil
}

func SendOnBoardingMail(loginURL string, password string, toEmail string) error {

	// 	var onboardingTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
	// <html dir="ltr" xmlns="http://www.w3.org/1999/xhtml" xmlns:o="urn:schemas-microsoft-com:office:office" lang="en">
	// <head>
	//   <meta charset="UTF-8">
	//   <meta content="width=device-width, initial-scale=1" name="viewport">
	//   <meta name="x-apple-disable-message-reformatting">
	//   <meta http-equiv="X-UA-Compatible" content="IE=edge">
	//   <meta content="telephone=no" name="format-detection">
	//   <title>Welcome Email</title>
	//   <link href="https://fonts.googleapis.com/css2?family=Imprima&display=swap" rel="stylesheet">
	// </head>
	// <body style="margin:0;padding:0;background-color:#ffffff;">
	//   <table width="100%" cellpadding="0" cellspacing="0" role="presentation">
	//     <tr>
	//       <td align="center">
	//         <table width="600" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#f4f4f4;padding:40px;border-radius:8px;">
	//           <tr>
	//             <td align="center">
	//               <img src="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/logo-1-removebg-preview__2025-07-23-14-55-30.png" alt="Welcome" width="100" style="border-radius:50px;">
	//               <h2 style="font-family:Imprima, Arial, sans-serif;color:#2D3142;">Welcome to KajuPro</h2>
	//             </td>
	//           </tr>
	//           <tr>
	//             <td style="font-family:Imprima, Arial, sans-serif;font-size:18px;color:#2D3142;padding-top:20px;">
	//               <p>You're receiving this message because you recently completed your account setup.</p>
	//               <p>Here are your login details:</p>
	//               <p><strong>🔐 Password:</strong> {{your_password}}<br>
	//                  <strong>🔗 Login URL:</strong> <a href="{{login_url}}" style="color:#2D3142;text-decoration:underline;">Login to Your Account</a><br>
	// 			<strong>📱 Get the KajuPro Android app for quick access:</strong><br>
	// <a href="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/app-arm64-v8a-release__2025-07-23-14-57-11.apk" style="color:#2D3142;text-decoration:underline;">Tap here to download the app</a><br>
	// <br>
	//           <strong>🔐 App Installation Key:</strong> DEMO11 <br>
	//               </p>
	//               <p>Use the above credentials to log in and get started.</p>
	//               <p>For your security, we recommend changing your password after your first login.</p>
	//               <p>If you didn’t request this account, please contact our support team immediately.</p>
	//               <br>
	//               <p>Thanks,<br><strong>KajuPro Team</strong></p>
	//               <hr style="border:none;border-top:1px solid #ccc;margin-top:30px;margin-bottom:10px;">
	//               <p style="font-size:14px;color:#666;">This link expires in 24 hours. If you have questions, <a href="https://viewstripo.email" style="color:#2D3142;text-decoration:underline;">we're here to help</a>.</p>
	//             </td>
	//           </tr>
	//         </table>
	//       </td>
	//     </tr>
	//   </table>
	// </body>
	// </html>`

	var onboardingTemplate = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html dir="ltr" xmlns="http://www.w3.org/1999/xhtml" xmlns:o="urn:schemas-microsoft-com:office:office" lang="en">
<head>
  <meta charset="UTF-8">
  <meta content="width=device-width, initial-scale=1" name="viewport">
  <meta name="x-apple-disable-message-reformatting">
  <meta http-equiv="X-UA-Compatible" content="IE=edge">
  <meta content="telephone=no" name="format-detection">
  <title>Welcome Email</title>
  <link href="https://fonts.googleapis.com/css2?family=Imprima&display=swap" rel="stylesheet">
</head>
<body style="margin:0;padding:0;background-color:#ffffff;font-family:Imprima, Arial, sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" role="presentation">
    <tr>
      <td align="center">
        <table width="600" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#f4f4f4;padding:40px;border-radius:8px;">
          <tr>
            <td align="center">
              <img src="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/logo-1-removebg-preview__2025-07-23-14-55-30.png" alt="Welcome" width="100" style="border-radius:50px;">
              <h2 style="font-family:Imprima, Arial, sans-serif;color:#2D3142;margin-top:20px;">Welcome to KajuPro</h2>
            </td>
          </tr>
          <tr>
            <td style="font-family:Imprima, Arial, sans-serif;font-size:18px;color:#2D3142;padding-top:20px;">
              <p>You're receiving this message because you recently completed your account setup.</p>
              <p><strong>Here are your login credentials:</strong></p>
              
              <!-- Login Credentials Box -->
              <div style="background:#ffffff;border:2px solid #2D3142;border-radius:10px;padding:25px;margin:20px 0;">
                <p style="margin:0 0 15px 0;">
                  <strong style="font-size:16px;">🔗 Login URL:</strong><br>
                  <a href="{{login_url}}" style="display:inline-block;margin-top:8px;background:#2D3142;color:#ffffff;text-decoration:none;padding:12px 24px;border-radius:6px;font-weight:bold;font-size:16px;">Click Here to Login</a><br>
                  <span style="font-size:14px;color:#666;margin-top:5px;display:inline-block;">Or copy: {{login_url}}</span>
                </p>
                
                <p style="margin:15px 0;">
                  <strong style="font-size:16px;">🔐 UserID:</strong><br>
                  <span style="background:#f5f5f5;padding:8px 15px;border-radius:6px;font-family:monospace;font-size:16px;display:inline-block;margin-top:5px;">{{UserId}}</span>
                </p>
                
                <p style="margin:15px 0 0 0;">
                  <strong style="font-size:16px;">🔐 Your Auto-Generated Password:</strong><br>
                  <span style="background:#e8f5e9;padding:10px 20px;border-radius:6px;font-family:monospace;font-size:20px;font-weight:bold;color:#2e7d32;display:inline-block;margin-top:8px;letter-spacing:3px;">{{your_password}}</span><br>
                  <em style="font-size:13px;color:#666;margin-top:8px;display:inline-block;">(This is a temporary password. Please change it after your first login)</em>
                </p>
              </div>

              <!-- Decorative App Download Section -->
              <div style="margin-top:30px;padding:20px;background:#ffffff;border:1px dashed #ccc;border-radius:8px;">
                <p style="font-size:20px;margin:0 0 10px 0;">📱 <strong>Ready to go mobile?</strong></p>
                <p style="font-size:16px;color:#2D3142;margin:0 0 20px 0;">
                  Download the <strong>KajuPro Android App</strong> and manage your account anytime, anywhere.
                </p>
                <a href="https://cerp.sgp1.digitaloceanspaces.com/app/organization/kajupro.apk"
                   style="display:inline-block;background-color:#2D3142;color:#ffffff;text-decoration:none;
                   padding:12px 24px;border-radius:6px;font-weight:bold;font-size:16px;">
                  📥 Tap to Download App
                </a>
                <p style="margin-top:15px;font-size:16px;">
                  🔑 <strong>App Installation Key:</strong> <span style="color:#000;">DEMO25</span>
                </p>
              </div>

              <p style="margin-top:25px;">Use the above credentials to log in and get started.</p>
              <p>For your security, we recommend changing your password after your first login.</p>
              <p>If you didn’t request this account, please contact our support team immediately.</p>

              <br>
              <p>Thanks,<br><strong>KajuPro Team</strong></p>
              <hr style="border:none;border-top:1px solid #ccc;margin-top:30px;margin-bottom:10px;">
              <p style="font-size:14px;color:#666;">This link expires in 24 hours. If you have questions, <a href="https://viewstripo.email" style="color:#2D3142;text-decoration:underline;">we're here to help</a>.</p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>
`

	// Replace placeholders
	onboardingTemplate = strings.ReplaceAll(onboardingTemplate, "{{UserId}}", toEmail)
	onboardingTemplate = strings.ReplaceAll(onboardingTemplate, "{{your_password}}", password)

	onboardingTemplate = strings.ReplaceAll(onboardingTemplate, "{{login_url}}", loginURL)

	// Print or send the email
	// fmt.Println(onboardingTemplate)
	err := SendEmailS(toEmail, os.Getenv("CLIENT_EMAIL"), "Onboarding", onboardingTemplate)
	if err != nil {
		fmt.Println(err.Error(), "error")
		return err
	}
	return nil

}
func SendRegistrationMail(userName string, orgName string, toEmail string) error {

	var template = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>Registration Successful</title>

<style>
body{
  background:#f5f6f8;
  font-family:Arial, sans-serif;
}
.container{
  max-width:600px;
  margin:40px auto;
  background:#ffffff;
  padding:40px;
  border-radius:8px;
}
.logo{
  text-align:center;
  margin-bottom:20px;
}
.logo img{
  width:120px;
}
h2{
  text-align:center;
  color:#2D3142;
}
p{
  font-size:16px;
  line-height:1.6;
  color:#333;
}
.info-box{
  background:#f4f4f4;
  padding:15px;
  border-radius:6px;
  margin:20px 0;
}
.footer{
  margin-top:30px;
  font-size:14px;
  color:#777;
  text-align:center;
}
</style>
</head>

<body>

<div class="container">

<div class="logo">
<img src="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/logo-1-removebg-preview__2025-07-23-14-55-30.png"/>
</div>

<h2>Registration Received</h2>

<p>Hello <strong>{{username}}</strong>,</p>

<p>
Thank you for registering with <strong>{{organization}}</strong> through the
<strong>KajuPro</strong> platform.
</p>

<div class="info-box">
<p style="margin:0;">
Your registration has been successfully submitted and is currently under review.
Our team will verify the submitted documents and information.
</p>
</div>

<p>
Once the verification process is completed, you will receive another email
containing your <strong>Onboarding Link</strong> to activate your account.
</p>

<p>
We appreciate your patience during this process.
</p>

<p>
Thanks,<br>
<strong>KajuPro Team</strong>
</p>

<div class="footer">
© 2025 KajuPro. All rights reserved.<br>
For queries: kajupro@gmail.com
</div>

</div>

</body>
</html>`

	template = strings.ReplaceAll(template, "{{username}}", userName)
	template = strings.ReplaceAll(template, "{{organization}}", orgName)

	err := SendEmailS(toEmail, os.Getenv("CLIENT_EMAIL"), "Registration Received", template)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	return nil
}

func SendAdminInviteMail(name string, inviteLink string, expiresAt string, toEmail string) error {
	senderEmail := os.Getenv("CLIENT_EMAIL")
	if senderEmail == "" {
		return fmt.Errorf("CLIENT_EMAIL is not configured")
	}
	if os.Getenv("CLIENT_EMAIL_PASSWORD") == "" {
		return fmt.Errorf("CLIENT_EMAIL_PASSWORD is not configured")
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = "Admin"
	}

	expiryText := "5 minutes"

	template := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>Admin Access Invitation</title>
  <style>
    body {
      margin: 0;
      padding: 0;
      background: #f5f6f8;
      font-family: Arial, sans-serif;
      color: #2D3142;
    }
    .container {
      max-width: 600px;
      margin: 40px auto;
      background: #ffffff;
      padding: 36px;
      border-radius: 8px;
    }
    .logo {
      text-align: center;
      margin-bottom: 20px;
    }
    .logo img {
      width: 120px;
    }
    h2 {
      text-align: center;
      margin: 0 0 24px;
      color: #2D3142;
    }
    p {
      font-size: 16px;
      line-height: 1.6;
      color: #333333;
    }
    .button {
      display: inline-block;
      background: #2D3142;
      color: #ffffff;
      padding: 12px 22px;
      border-radius: 6px;
      text-decoration: none;
      font-weight: bold;
      margin: 14px 0;
    }
    .link-box {
      word-break: break-all;
      background: #f4f4f4;
      padding: 14px;
      border-radius: 6px;
      font-size: 13px;
      color: #444444;
    }
    .footer {
      margin-top: 30px;
      font-size: 14px;
      color: #777777;
      text-align: center;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="logo">
      <img src="https://cerp.sgp1.digitaloceanspaces.com/logo/organization/logo-1-removebg-preview__2025-07-23-14-55-30.png" alt="MarketPlace" />
    </div>
    <h2>Admin Access Invitation</h2>
    <p>Hello <strong>%s</strong>,</p>
    <p>You have been invited to set up admin access for the MarketPlace platform.</p>
    <p>
      <a class="button" href="%s">Set Up Admin Access</a>
    </p>
    <p>This access link is valid for %s. If the button does not open, copy and paste this link into your browser:</p>
    <div class="link-box">%s</div>
    <p>If you were not expecting this invitation, you can safely ignore this email.</p>
    <p>Thanks,<br><strong>MarketPlace Team</strong></p>
    <div class="footer">
      Marketplace admin registration mail
    </div>
  </div>
</body>
</html>`, html.EscapeString(displayName), html.EscapeString(inviteLink), expiryText, html.EscapeString(inviteLink))

	return SendEmailS(toEmail, senderEmail, "Your admin access link", template)
}

func SendOrgBoardingMail(loginURL string, toEmail string, companyName string, firstName string, yourName string, yourPosition string) error {

	fmt.Printf("Sending email to %s with company: %s, firstName: %s, yourName: %s, position: %s, loginURL: %s\n", toEmail, companyName, firstName, yourName, yourPosition, loginURL)

	onboardingTemplate := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>Welcome Onboard</title>
  <style>
    body {
      font-family: 'Segoe UI', sans-serif;
      background-color: #f4f4f4;
      margin: 0;
      padding: 0;
    }

    .email-container {
      max-width: 600px;
      margin: 30px auto;
      background-color: #ffffff;
      padding: 30px;
      border-radius: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
    }

    .header {
      text-align: center;
      background-color: #0052cc;
      color: white;
      padding: 20px 0;
      border-radius: 8px 8px 0 0;
    }

    .header h1 {
      margin: 0;
      font-size: 24px;
    }

    .content {
      padding: 20px;
      color: #333;
      line-height: 1.6;
    }

    .content h2 {
      color: #0052cc;
    }

    .button {
      display: inline-block;
      background-color: #0052cc;
      color: black;
      padding: 12px 20px;
      margin-top: 20px;
      text-decoration: none;
      border-radius: 5px;
      font-weight: bold;
    }

    .footer {
      text-align: center;
      font-size: 12px;
      color: #777;
      margin-top: 30px;
    }

    @media (max-width: 600px) {
      .email-container {
        padding: 20px;
      }
    }
  </style>
</head>
<body>
  <div class="email-container">
    <div class="header">
      <h1>Welcome to %s 🎉</h1>
    </div>
    <div class="content">
      <p>Hi <strong>%s</strong>,</p>

      <p>We're excited to have you onboard! Here's everything you need to get started with us.</p>

      <h2>📝 Next Steps</h2>
      <ul>
        <li>✔️ Log in to your account: <a href="%s">%s</a></li>
        <li>✔️ Complete your profile setup</li>
        <li>✔️ Review the onboarding guide</li>
      </ul>

      <a href="%s" style="display: inline-block; background-color: #0052cc; color: white; padding: 12px 20px; margin-top: 20px; text-decoration: none; border-radius: 5px; font-weight: bold;">Get Started</a>

      <p>If the button above doesn't work, click here: <a href="%s">%s</a></p>

      <p>If you have any questions, feel free to reply to this email. We're here to help!</p>

      <p>Cheers,<br><strong>%s</strong><br>%s</p>
    </div>
    <div class="footer">
      © 2025 %s. All rights reserved.<br>
      [Company Address] · [Contact Info]
    </div>
  </div>
</body>
</html>`, companyName, firstName, loginURL, loginURL, loginURL, loginURL, loginURL, yourName, yourPosition, companyName)

	err := SendEmailS(toEmail, os.Getenv("CLIENT_EMAIL"), "Onboarding", onboardingTemplate)
	if err != nil {
		fmt.Println(err.Error(), "error")
		return err
	}
	return nil
}
