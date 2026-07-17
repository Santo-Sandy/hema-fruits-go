package server

import (
	"flag"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"

	//"github.com/gofiber/fiber/v2/middleware/cache"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"

	//"github.com/gofiber/fiber/v2/middleware/csrf"

	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"kriyatec.com/pms-api/pkg/shared"
)

// func EntryExitMiddleware(c *fiber.Ctx) error {
// 	entryTime := time.Now()
// 	fmt.Printf("➡️ Entry | %s %s | Time: %s", c.Method(), c.Path(), entryTime.Format(time.RFC3339))

// 	// Proceed to the next handler
// 	err := c.Next()

// 	exitTime := time.Now()
// 	duration := exitTime.Sub(entryTime)
// 	fmt.Printf("⬅️ Exit  | %s %s | Time: %s | Duration: %v\n", c.Method(), c.Path(), exitTime.Format(time.RFC3339), duration)

// 	return err
// }

func setupMiddlewares(app *fiber.App) {

	var loggger = flag.Bool("logger", false, "Whether log service is required or not")

	app.Use(logger.New())

	// Provide a custom compression level
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed, // 1
	}))

	//extend your config for customization
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowHeaders:     "OrgId, Origin, Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization,X-Requested-With,Cache-Control",
		AllowMethods:     "POST,GET,PUT,OPTIONS,DELETE,HEAD",
		ExposeHeaders:    "Content-Type,Cache-Control,Connection,Transfer-Encoding",
		AllowCredentials: false,
		MaxAge:           10,
	}))

	// app.Use(EntryExitMiddleware)

	//Cache config for customization
	// app.Use(cache.New(cache.Config{
	// 	Next: func(c *fiber.Ctx) bool {
	// 		return c.Query("refresh") == "true"
	// 	},
	// 	Expiration:   30 * time.Minute,
	// 	CacheControl: true,
	// }))

	//CSRF config for customization
	// app.Use(csrf.New(csrf.Config{
	// 	KeyLookup:      "header:X-Csrf-Token",
	// 	CookieName:     "csrf_",
	// 	CookieSameSite: "Strict",
	// 	Expiration:     1 * time.Hour,
	// 	//TODO
	// 	//	KeyGenerator:   utils.UUID,
	// }))

	//ETag middleware for Fiber that lets caches be more efficient and save bandwidth,
	//as a web server does not need to resend a full response if the content has not changed.
	// app.Use(etag.New(etag.Config{
	// 	Weak: true,
	// }))

	if *loggger {
		//Logger middleware for Fiber that logs HTTP request/response details.
		file, err := os.OpenFile("./log/debug.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("error opening file: %v", err)
		}
		defer file.Close()
		app.Use(logger.New(logger.Config{
			Output:     file,
			Format:     "${pid} ${status} - ${method} ${path}\n",
			TimeFormat: "02-Jan-2006",
			TimeZone:   "America/New_York",
		}))
	}

	//Recover middleware for Fiber that recovers from panics anywhere in the stack chain and handles the control to the centralized ErrorHandler.
	// Default middleware config
	app.Use(recover.New())

}

func Create() *fiber.App {
	var (
		appname = flag.String("appname", os.Getenv("APP_NAME"), "Application Name")
		// prod    = flag.Bool("prod", false, "Enable prefork in Production")
	)
	app := fiber.New(fiber.Config{
		AppName:      *appname,
		Prefork:      false,
		ErrorHandler: CustomErrorHandler,
		BodyLimit:    300 * 1024 * 1024,
	})
	//middleware config
	setupMiddlewares(app)
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Welcome to KriyaTec")
	})
	//See the API dashboard
	app.Get("/server-dashboard", monitor.New())
	return app
}

func Listen(app *fiber.App, port string) error {
	var url = flag.String("port", port, "Port to listen on")
	var ssl_cert_file = os.Getenv("SSL_CERT_FILE")
	var ssl_key_file = os.Getenv("SSL_KEY_FILE")
	// 404 Handler
	app.Use(func(c *fiber.Ctx) error {
		return c.SendStatus(404)
	})
	if ssl_cert_file != "" {
		return app.ListenTLS(*url, ssl_cert_file, ssl_key_file)
	} else {
		return app.Listen(*url) // go run app.go -port=:3000
	}
}

// Override default error handler
func CustomErrorHandler(ctx *fiber.Ctx, err error) error {
	if e, ok := err.(*shared.Error); ok {
		return ctx.Status(e.Status).JSON(e)
	} else if e, ok := err.(*fiber.Error); ok {
		return ctx.Status(e.Code).JSON(shared.Error{Status: e.Code, Code: "internal-server", Message: e.Message})
	} else {
		return ctx.Status(500).JSON(shared.Error{Status: 500, Code: "internal-server", Message: err.Error()})
	}
}
