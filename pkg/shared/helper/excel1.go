package helper

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

// func colIndexToLetter(index int) string {
// 	letter := ""
// 	for index >= 0 {
// 		letter = string('A'+index%26) + letter
// 		index = index/26 - 1
// 	}
// 	return letter
// }

// func ExcelGenerator1(c *fiber.Ctx) error {
// 	var req map[string]interface{}
// 	if err := c.BodyParser(&req); err != nil {
// 		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Invalid request payload",
// 		})
// 	}

// 	if req["table_header"] == nil || req["data"] == nil {
// 		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Missing required fields in the request payload",
// 		})
// 	}

// 	// Convert "table_header" to []string
// 	tableHeader, ok := req["table_header"].([]interface{})
// 	if !ok {
// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Invalid table_header format",
// 		})
// 	}

// 	var headerStrings []string
// 	for _, h := range tableHeader {
// 		headerStrings = append(headerStrings, fmt.Sprintf("%v", h))
// 	}

// 	// Create a new XLSX file
// 	f := excelize.NewFile()
// 	sheet := "Sheet1"
// 	_, err := f.NewSheet(sheet)
// 	if err != nil {
// 		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Invalid request payload",
// 		})
// 	}
// 	// Add title row with merged columns
// 	title := req["title"].(string) // Replace with your desired title
// 	f.SetCellValue(sheet, "A1", strings.ToUpper(title))

// 	// Merge cells across the length of the tableHeader
// 	mergeLength := len(headerStrings) - 1
// 	startCell := "A1"
// 	endCell := fmt.Sprintf("%s1", colIndexToLetter(mergeLength))
// 	if err := f.MergeCell(sheet, startCell, endCell); err != nil {
// 		log.Println("Error merging cells:", err)
// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Failed to merge cells",
// 		})
// 	}

// 	// Define the style for the title
// 	titleStyle := excelize.Style{
// 		Font: &excelize.Font{
// 			Bold:   true,
// 			Size:   15,
// 			Family: "Arial",
// 		},
// 		Alignment: &excelize.Alignment{
// 			Horizontal: "center",
// 			Vertical:   "center",
// 		},
// 	}

// 	// Apply the style to the title cell
// 	titleStyleID, err := f.NewStyle(&titleStyle)
// 	if err != nil {
// 		log.Println("Error creating style:", err)
// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Failed to create style",
// 		})
// 	}
// 	f.SetCellStyle(sheet, startCell, endCell, titleStyleID)
// 	colWidths := make(map[string]int)
// 	// Write the header
// 	for i, header := range headerStrings {
// 		cell := fmt.Sprintf("%s2", colIndexToLetter(i))
// 		f.SetCellValue(sheet, cell, strings.ToUpper(header))

// 		// Update column width based on header length
// 		if len(header) > colWidths[cell] {
// 			colWidths[cell] = len(header)
// 		}
// 		// Define the style for the header
// 		headerStyle := excelize.Style{
// 			Font: &excelize.Font{
// 				Bold:   true,
// 				Size:   9,
// 				Family: "Arial",
// 			},
// 			Alignment: &excelize.Alignment{
// 				Horizontal: "center",
// 				Vertical:   "center",
// 			},
// 		}

// 		// Apply the style to the header cell
// 		headerStyleID, err := f.NewStyle(&headerStyle)
// 		if err != nil {
// 			log.Println("Error creating style:", err)
// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 				"error":   true,
// 				"message": "Failed to create style",
// 			})
// 		}
// 		f.SetCellStyle(sheet, cell, cell, headerStyleID)
// 	}

// 	// Write the data
// 	rowNumber := 3
// 	for _, record := range req["data"].([]interface{}) {
// 		recordMap, ok := record.(map[string]interface{})
// 		if !ok {
// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 				"error":   true,
// 				"message": "Invalid data format",
// 			})
// 		}

// 		for i, key := range headerStrings {
// 			value, exists := recordMap[key]
// 			if !exists {
// 				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 					"error":   true,
// 					"message": "Missing field in data",
// 				})
// 			}

// 			cell := fmt.Sprintf("%s%d", colIndexToLetter(i), rowNumber)
// 			f.SetCellValue(sheet, cell, fmt.Sprintf("%v", value))

// 			// Determine the data type and set alignment accordingly
// 			var align string
// 			switch value.(type) {
// 			case string:
// 				align = "left"
// 			case int, int32, int64, float64, float32:
// 				align = "right"
// 			default:
// 				align = "left"
// 			}

// 			// Define the style for data cells
// 			dataStyle := excelize.Style{
// 				Font: &excelize.Font{
// 					Size:   8,
// 					Family: "Arial",
// 				},
// 				Alignment: &excelize.Alignment{
// 					Horizontal: align,
// 					Vertical:   "center",
// 				},
// 			}

// 			// Apply the style to the data cell
// 			dataStyleID, err := f.NewStyle(&dataStyle)
// 			if err != nil {
// 				log.Println("Error creating style:", err)
// 				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 					"error":   true,
// 					"message": "Failed to create style",
// 				})
// 			}
// 			f.SetCellStyle(sheet, cell, cell, dataStyleID)
// 		}
// 		rowNumber++
// 	}

// 	// Save the file locally
// 	filePath := "output.xlsx"
// 	if err := f.SaveAs(filePath); err != nil {
// 		log.Println("Error saving XLSX file:", err)
// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
// 			"error":   true,
// 			"message": "Failed to save XLSX file",
// 		})
// 	}

// 	res := map[string]interface{}{
// 		"filepath": filePath,
// 	}
// 	return c.JSON(fiber.Map{
// 		"success": true,
// 		"data":    res,
// 	})
// }

func colIndexToLetter(index int) string {
	letter := ""
	for index >= 0 {
		letter = string('B'+index%26) + letter
		index = index/26 - 1
	}
	return letter
}

func ExcelGenerator1(c *fiber.Ctx) error {
	var req map[string]interface{}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Invalid request payload",
		})
	}
	userToken := utils.GetUserTokenValue(c)

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

	var headerStrings []string
	for _, h := range tableHeader {
		headerStrings = append(headerStrings, fmt.Sprintf("%v", h))
	}

	// Create a new XLSX file
	f := excelize.NewFile()
	sheet := "Sheet1"
	_, err := f.NewSheet(sheet)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Invalid request payload",
		})
	}

	rowValue := ""
	// Add title row with merged columns
	if req["title"] != nil {
		title := req["title"].(string) // Replace with your desired title
		f.SetCellValue(sheet, "A1", strings.ToUpper(title))
		rowValue = "A2"
		// Merge cells across the length of the tableHeader
		mergeLength := len(headerStrings) - 1
		startCell := "A1"
		endCell := fmt.Sprintf("%s1", colIndexToLetter(mergeLength))
		if err := f.MergeCell(sheet, startCell, endCell); err != nil {
			log.Println("Error merging cells:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to merge cells",
			})
		}

		// Define the style for the title
		titleStyle := excelize.Style{
			Font: &excelize.Font{
				Bold:   true,
				Size:   15,
				Family: "Arial",
			},
			Alignment: &excelize.Alignment{
				Horizontal: "center",
				Vertical:   "center",
			},
			Border: []excelize.Border{
				{Type: "left", Color: "000000", Style: 1},
				{Type: "right", Color: "000000", Style: 1},
				{Type: "top", Color: "000000", Style: 1},
				{Type: "bottom", Color: "000000", Style: 1},
			},
		}

		// Apply the style to the title cell
		titleStyleID, err := f.NewStyle(&titleStyle)
		if err != nil {
			log.Println("Error creating style:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to create style",
			})
		}
		// Set row height for title row and header row
		if err := f.SetRowHeight(sheet, 1, 25); err != nil { // Title row
			log.Println("Error setting row height:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to set row height",
			})
		}
		f.SetCellStyle(sheet, startCell, endCell, titleStyleID)
	} else {
		rowValue = "A1"
	}
	// Define a map to track the maximum width of each column
	colWidths := make(map[string]int)
	// Insert the S.No header at the first column (A1)
	f.SetCellValue(sheet, rowValue, strings.ToUpper("S.No"))
	// Write the header
	for i, header := range headerStrings {
		cell := fmt.Sprintf("%s2", colIndexToLetter(i))
		f.SetCellValue(sheet, cell, strings.ToUpper(header))

		// Update column width based on header length
		colName := colIndexToLetter(i)
		if len(header) > colWidths[colName] {
			colWidths[colName] = len(header)
		}

		// Define the style for the header
		headerStyle := excelize.Style{
			Font: &excelize.Font{
				Bold:   true,
				Size:   12,
				Family: "Arial",
			},

			Alignment: &excelize.Alignment{
				Horizontal: "center",
				Vertical:   "center",
			},
			Border: []excelize.Border{
				{Type: "left", Color: "000000", Style: 1},
				{Type: "right", Color: "000000", Style: 1},
				{Type: "top", Color: "000000", Style: 1},
				{Type: "bottom", Color: "000000", Style: 1},
			},
		}

		// Apply the style to the header cell
		headerStyleID, err := f.NewStyle(&headerStyle)
		if err != nil {
			log.Println("Error creating style:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to create style",
			})
		}
		f.SetCellStyle(sheet, cell, cell, headerStyleID)
		if err := f.SetRowHeight(sheet, 2, 20); err != nil { // Header row
			log.Println("Error setting row height:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to set row height",
			})
		}
	}

	// Write the data
	//rowCount := 1
	rowNumber := 3
	//serialNo:=1
	// for _, record := range req["data"].([]interface{}) {
	// 	recordMap, ok := record.(map[string]interface{})
	// 	if !ok {
	// 		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 			"error":   true,
	// 			"message": "Invalid data format",
	// 		})
	// 	}

	// 	f.SetCellValue(sheet, fmt.Sprintf("A", rowNumber), fmt.Sprintf("%v", rowCount))
	// 	rowCount = rowCount + 1
	// 	for i, key := range headerStrings {
	// 		value, exists := recordMap[key]
	// 		if !exists {
	// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 				"error":   true,
	// 				"message": "Missing field in data",
	// 			})
	// 		}

	// 		cell := fmt.Sprintf("%s%d", colIndexToLetter(i), rowNumber)
	// 		f.SetCellValue(sheet, cell, fmt.Sprintf("%v", value))

	// 		// Determine the data type and set alignment accordingly
	// 		var align string
	// 		switch value.(type) {
	// 		case string:
	// 			align = "left"
	// 		case int, int32, int64, float64, float32:
	// 			align = "right"
	// 		default:
	// 			align = "left"
	// 		}

	// 		// Define the style for data cells
	// 		dataStyle := excelize.Style{
	// 			Font: &excelize.Font{
	// 				Size:   10,
	// 				Family: "Arial",
	// 			},
	// 			Alignment: &excelize.Alignment{
	// 				Horizontal: align,
	// 				Vertical:   "center",
	// 			},
	// 			Border: []excelize.Border{
	// 				{Type: "left", Color: "000000", Style: 1},
	// 				{Type: "right", Color: "000000", Style: 1},
	// 				{Type: "top", Color: "000000", Style: 1},
	// 				{Type: "bottom", Color: "000000", Style: 1},
	// 			},
	// 		}

	// 		// Apply the style to the data cell
	// 		dataStyleID, err := f.NewStyle(&dataStyle)
	// 		if err != nil {
	// 			log.Println("Error creating style:", err)
	// 			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 				"error":   true,
	// 				"message": "Failed to create style",
	// 			})
	// 		}
	// 		f.SetCellStyle(sheet, cell, cell, dataStyleID)

	// 		// Update column width based on cell value length
	// 		colName := colIndexToLetter(i)
	// 		if len(fmt.Sprintf("%v", value)) > colWidths[colName] {
	// 			colWidths[colName] = len(fmt.Sprintf("%v", value))
	// 		}
	// 	}
	// 	rowNumber++
	// }
	rowCount := 1 // Initialize the row counter
	for _, record := range req["data"].([]interface{}) {
		recordMap, ok := record.(map[string]interface{})
		if !ok {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid data format",
			})
		}

		// Set the first cell of each row with the rowCount value
		firstCell := fmt.Sprintf("A%d", rowNumber)
		f.SetCellValue(sheet, firstCell, fmt.Sprintf("%v", rowCount))
		rowCount++

		// Define the style for the first column's cell (rowCount)
		dataStyle := excelize.Style{
			Font: &excelize.Font{
				Size:   10,
				Family: "Arial",
			},
			Alignment: &excelize.Alignment{
				Horizontal: "center",
				Vertical:   "center",
			},
			Border: []excelize.Border{
				{Type: "left", Color: "000000", Style: 1},
				{Type: "right", Color: "000000", Style: 1},
				{Type: "top", Color: "000000", Style: 1},
				{Type: "bottom", Color: "000000", Style: 1},
			},
		}

		// Apply the style to the first cell (A column)
		dataStyleID, err := f.NewStyle(&dataStyle)
		if err != nil {
			log.Println("Error creating style:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to create style",
			})
		}
		f.SetCellStyle(sheet, firstCell, firstCell, dataStyleID)

		for i, key := range headerStrings {
			value, exists := recordMap[key]
			if !exists {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   true,
					"message": "Missing field in data",
				})
			}

			cell := fmt.Sprintf("%s%d", colIndexToLetter(i), rowNumber)
			f.SetCellValue(sheet, cell, fmt.Sprintf("%v", value))

			// Determine the data type and set alignment accordingly
			var align string
			switch value.(type) {
			case string:
				align = "left"
			case int, int32, int64, float64, float32:
				align = "right"
			default:
				align = "left"
			}

			// Define the style for data cells
			dataStyle := excelize.Style{
				Font: &excelize.Font{
					Size:   10,
					Family: "Arial",
				},
				Alignment: &excelize.Alignment{
					Horizontal: align,
					Vertical:   "center",
				},
				Border: []excelize.Border{
					{Type: "left", Color: "000000", Style: 1},
					{Type: "right", Color: "000000", Style: 1},
					{Type: "top", Color: "000000", Style: 1},
					{Type: "bottom", Color: "000000", Style: 1},
				},
			}

			// Apply the style to the data cell
			dataStyleID, err := f.NewStyle(&dataStyle)
			if err != nil {
				log.Println("Error creating style:", err)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   true,
					"message": "Failed to create style",
				})
			}
			f.SetCellStyle(sheet, cell, cell, dataStyleID)

			// Update column width based on cell value length
			colName := colIndexToLetter(i)
			if len(fmt.Sprintf("%v", value)) > colWidths[colName] {
				colWidths[colName] = len(fmt.Sprintf("%v", value))
			}
		}
		rowNumber++
	}

	// Set the column widths based on the maximum length of the content
	for col, _ := range colWidths {
		// Add a bit of extra space for padding
		if err := f.SetColWidth(sheet, col, col, float64(25.5)); err != nil {
			log.Println("Error setting column width:", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to set column width",
			})
		}
	}
	//check the user folder,
	// folderName := "./uploads/system"

	// if _, err := os.Stat(folderName); os.IsNotExist(err) {
	// 	os.MkdirAll(folderName, 0777)
	// }
	// // Save the file locally
	// filePath := "./uploads/system/"
	times := "report" + "__" + time.Now().Format("2006-01-02-15-04-05") + ".xlsx"

	// if err := f.SaveAs(filePath); err != nil {
	// 	log.Println("Error saving XLSX file:", err)
	// 	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
	// 		"error":   true,
	// 		"message": "Failed to save XLSX file",
	// 	})
	// }
	// Create a buffer to hold the file data
	var buf bytes.Buffer

	// Write the Excel file content to the buffer
	if err := f.Write(&buf); err != nil {
		log.Println("Error writing XLSX to buffer:", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Failed to generate XLSX file",
		})
	}

	// Convert the buffer to []byte
	fileBytes := buf.Bytes()
	fmt.Println(userToken.OrgId, userToken.UserId)
	path, err := UploadGeneratedFile(fileBytes, times, userToken.OrgId, userToken.UserId)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}
	res := map[string]interface{}{
		"file_path": path,
	}
	return shared.SuccessResponse(c, res)
}
