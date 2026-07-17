package helper

import (
	"fmt"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/tealeg/xlsx"
	// "github.com/tealeg/xlsx/v3"

	"kriyatec.com/pms-api/pkg/shared"
)

func ExcelGenerator(c *fiber.Ctx) error {
	var req map[string]interface{}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Invalid request payload",
		})
	}

	if req["table_header"] == nil || req["data"] == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Missing required fields in the request payload",
		})
	}

	// Convert "table_header" to []string
	tableHeader, ok := req["table_header"].([]interface{})
	if !ok {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Invalid table_header format",
		})
	}

	// Convert "table_header" to []string
	var headerStrings []string
	for _, h := range tableHeader {
		headerStrings = append(headerStrings, fmt.Sprintf("%v", h))
	}

	// Create a new XLSX file
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("Sheet1")
	if err != nil {
		log.Println("Error creating sheet:", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Internal Server Error",
		})
	}

	// // Add title row with merged columns
	// title := "Your Title Here" // Replace with your desired title
	// titleRow := sheet.AddRow()

	// titleCell := titleRow.AddCell()
	// titleCell.Value = title

	// // Merge cells across the length of the tableHeader
	// mergeLength := len(headerStrings) - 1
	// err = sheet.MergeCells(0, 0, 0, mergeLength)
	// if err != nil {
	// 	log.Println("Error merging cells:", err)
	// 	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 		"error":   true,
	// 		"message": "Failed to merge cells",
	// 	})
	// }

	// // Style the title
	// titleStyle := xlsx.NewStyle()
	// titleFont := *xlsx.NewFont(12, "Arial")
	// titleFont.Bold = true
	// titleStyle.Font = titleFont
	// titleStyle.Alignment.Horizontal = "center"
	// titleStyle.Alignment.Vertical = "center"
	// titleCell.SetStyle(titleStyle)

	// Write the header
	row := sheet.AddRow()
	// Define a style for the header to make it bold
	style := xlsx.NewStyle()
	font := *xlsx.NewFont(9, "Arial")
	font.Bold = true
	style.Font = font
	// Add headers, convert to uppercase, and apply the bold style
	for _, header := range headerStrings {
		cell := row.AddCell()
		cell.Value = strings.ToUpper(header) // Convert to uppercase
		cell.SetStyle(style)
		// Apply bold style
	}

	// Write the data
	for _, record := range req["data"].([]interface{}) {
		recordMap, ok := record.(map[string]interface{})
		if !ok {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid data format",
			})
		}

		row := sheet.AddRow()

		// for _, key := range headerStrings {
		// 	value, exists := recordMap[key]
		// 	if !exists {
		// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		// 			"error":   true,
		// 			"message": "Missing field in data",
		// 		})
		// 	}
		// 	cell := row.AddCell()
		// 	cell.Value = fmt.Sprintf("%v", value)
		// }

		// Iterate over the headers and assign cell values with alignment
		for _, key := range headerStrings {
			value, exists := recordMap[key]
			if !exists {
				fmt.Println(fiber.Map{
					"error":   true,
					"message": "Missing field in data",
				})
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   true,
					"message": "Missing field in data",
				})
			}

			cell := row.AddCell()

			// Determine the data type and set alignment accordingly
			style := xlsx.NewStyle()
			font := *xlsx.NewFont(8, "Arial")
			font.Bold = false
			style.Font = font

			alignment := xlsx.Alignment{
				Vertical:   "center",
				Horizontal: "left", // Default alignment
			}

			fmt.Printf("type %T\n", value)

			switch v := value.(type) {
			case string:
				fmt.Println("string")
				alignment.Horizontal = "left"
			case int, int32, int64, float64, float32:
				fmt.Println("number")
				alignment.Horizontal = "right"
			default:
				fmt.Printf("default type: %T\n", v)
				alignment.Horizontal = "left"
			}

			cell.Value = fmt.Sprintf("%v", value)
			style.Alignment = alignment
			cell.SetStyle(style)

		}
	}

	// Save the file locally
	filePath := "output.xlsx"
	err = file.Save(filePath)
	if err != nil {
		log.Println("Error saving XLSX file:", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Failed to save XLSX file",
		})
	}

	res := map[string]interface{}{
		"filepath": filePath,
	}
	return shared.SuccessResponse(c, res)
}
