package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// UploadHandler handles file uploads (e.g. product photos, certificates, profile images)
func UploadHandler(c *fiber.Ctx) error {
	// Create uploads directory if not exists
	uploadsDir := "./uploads"
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"status":  500,
			"message": "Failed to initialize upload directory: " + err.Error(),
		})
	}

	form, err := c.MultipartForm()
	if err != nil {
		// Try single file
		file, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"status":  400,
				"message": "No file uploaded. Use field name 'file'",
			})
		}

		cleanFileName := strings.ReplaceAll(filepath.Base(file.Filename), " ", "_")
		savedFileName := fmt.Sprintf("%d_%s", time.Now().UnixNano()/int64(time.Millisecond), cleanFileName)
		destPath := filepath.Join(uploadsDir, savedFileName)

		if err := c.SaveFile(file, destPath); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"status":  500,
				"message": "Failed to save file: " + err.Error(),
			})
		}

		protocol := "http"
		if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
			protocol = "https"
		}
		host := c.Hostname()
		if host == "" {
			host = "localhost:7002"
		}
		fileURL := fmt.Sprintf("%s://%s/uploads/%s", protocol, host, savedFileName)

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  200,
			"message": "File uploaded successfully",
			"data": []fiber.Map{
				{
					"filename": savedFileName,
					"name":     file.Filename,
					"url":      fileURL,
					"path":     "/uploads/" + savedFileName,
					"size":     file.Size,
					"mimetype": file.Header.Get("Content-Type"),
				},
			},
		})
	}

	var results []fiber.Map
	protocol := "http"
	if c.Secure() || c.Get("X-Forwarded-Proto") == "https" {
		protocol = "https"
	}
	host := c.Hostname()
	if host == "" {
		host = "localhost:7002"
	}

	for _, files := range form.File {
		for _, file := range files {
			cleanFileName := strings.ReplaceAll(filepath.Base(file.Filename), " ", "_")
			savedFileName := fmt.Sprintf("%d_%s", time.Now().UnixNano()/int64(time.Millisecond), cleanFileName)
			destPath := filepath.Join(uploadsDir, savedFileName)

			if err := c.SaveFile(file, destPath); err == nil {
				fileURL := fmt.Sprintf("%s://%s/uploads/%s", protocol, host, savedFileName)
				results = append(results, fiber.Map{
					"filename": savedFileName,
					"name":     file.Filename,
					"url":      fileURL,
					"path":     "/uploads/" + savedFileName,
					"size":     file.Size,
					"mimetype": file.Header.Get("Content-Type"),
				})
			}
		}
	}

	if len(results) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"status":  400,
			"message": "No valid files could be processed",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  200,
		"message": "Files uploaded successfully",
		"data":    results,
	})
}
