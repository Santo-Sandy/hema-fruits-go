package helper

import (
	"bytes"
	"fmt"
	"math"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/numfmt"
	"github.com/jung-kurt/gofpdf"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

type GoPdf struct {
	*gofpdf.Fpdf
}

var currentXpostion, currentYpostion, lineHeight float64

func GenerateInvoicePDF(invoice map[string]interface{}, orgId string) (map[string]interface{}, error) {
	// Attempt to retrieve and assert the "shop_result" from the invoice map
	Data, ok := invoice["shop_result"]
	if !ok {
		return nil, fmt.Errorf("key shop_result not found in invoice map")
	}

	shopData, ok := Data.(primitive.M)
	if !ok {
		return nil, fmt.Errorf("shop_result is of type %T, expected map[string]interface{}", Data)
	}

	// Attempt to retrieve and assert the "location" from the shopData map
	shopAddress, ok := shopData["location"].(primitive.M)

	if !ok {
		return nil, fmt.Errorf("error asserting location as map[string]interface{}")
	}

	// Attempt to retrieve and assert the "billing_details_result" as a slice of map[string]interface{}
	billingProducts, ok := invoice["billing_details_result"].(primitive.A)
	if !ok {
		return nil, fmt.Errorf("billing_details_result is of type %T, expected map[string]interface{}", invoice["billing_details_result"])
	}

	companyInfo := "No.10, 1st Street Valliammal Nagar Valasaravakkam, Chennai-600087, Tamil Nadu"
	CompanyContactNumber := "044 4852 1151"
	CompanyEmailId := "customercare@sakthipharma.com"

	pdf := GoPdf{gofpdf.New("P", "mm", "A4", "")}

	// Add a new page
	pdf.AddPage()
	//incNo := ToString(GetNextSeqNumber(orgId, "SHOPINV"))
	// Set fonts
	pdf.SetFont("Arial", "B", 16)
	title := "Centered Title"
	_, pageWidth := pdf.GetPageSize()
	titleWidth := pdf.GetStringWidth(title)

	// Calculate the X position to center the title
	centerX := (pageWidth - titleWidth) / 2

	// Set the Y position for the title
	y := 10.0 // Y coordinate of the title, adjust as needed

	// Add the title
	pdf.SetXY(centerX, y)
	pdf.CellFormat(0, 10, title, "", 0, "C", false, 0, "")
	sakthiPharmaWidth := pdf.GetStringWidth("TAX INVOICE")
	pdf.SetXY(210-sakthiPharmaWidth-10, 10) // Right align "TAX INVOICE"
	pdf.Cell(sakthiPharmaWidth, 10, "TAX INVOICE")
	pdf.SetXY(210-sakthiPharmaWidth-10, 15)
	pdf.SetFont("Arial", "B", 8)
	pdf.Cell(sakthiPharmaWidth, 10, "DL No: TN-05-20-00267 & 21")
	pdf.SetXY(210-sakthiPharmaWidth-10, 19)
	pdf.Cell(sakthiPharmaWidth, 10, "GSTIN: 33AAECJ6856G1ZG")
	pdf.SetXY(10, 10) // Left align "SAKTHI PHARMA"
	pdf.ImageOptions("sakthi.png", pdf.GetX(), pdf.GetY(), 40, 15, false, gofpdf.ImageOptions{ReadDpi: true}, 0, "")
	pdf.Ln(12)

	// Company Info
	pdf.SetFont("Arial", "", 10)
	pdf.SetXY(10, pdf.GetY()+5) // Reset position
	pdf.MultiCell(60, 6, companyInfo, "", "", false)
	pdf.Ln(3)
	pdf.ImageOptions("phone.png", pdf.GetX(), pdf.GetY(), 5, 0, false, gofpdf.ImageOptions{ReadDpi: true}, 0, "")
	pdf.Cell(60, 6, "       "+CompanyContactNumber)
	pdf.Ln(6)
	pdf.ImageOptions("./mail.png", pdf.GetX(), pdf.GetY(), 5, 0, false, gofpdf.ImageOptions{ReadDpi: true}, 0, "")
	pdf.Cell(60, 6, "       "+CompanyEmailId)
	pdf.Ln(12)

	// Order and Invoice details
	pdf.SetFont("Arial", "B", 12)
	startX := pdf.GetX() // Save start X position
	startY := pdf.GetY() // Save start Y position

	pdf.SetXY(startX, startY) // Set to saved X and Y positions
	pdf.Cell(95, 10, "Bill # : "+invoice["_id"].(string))
	pdf.SetXY(startX+135, startY)
	pdf.Cell(95, 10, "Invoice # : ")
	pdf.Ln(6)
	pdf.SetXY(startX, pdf.GetY()) // Reset X position for dates
	pdf.SetFont("Arial", "", 10)
	billCreatedOn := invoice["created_on"].(primitive.DateTime)

	// Convert primitive.DateTime to time.Time
	createdOnTime := billCreatedOn.Time()

	// Format the date as a string (e.g., "YYYY-MM-DD")
	dateString := createdOnTime.Format("02-01-2006")
	pdf.Cell(95, 10, "Date : "+dateString)
	pdf.SetXY(startX+135, pdf.GetY()) // Move to next column for Invoice Date
	pdf.Cell(95, 10, "Date : "+ToString(time.Now().Format("02-01-2006")))
	pdf.Ln(12)

	// Shipping Address
	pdf.SetFont("Arial", "B", 12)
	pdf.SetXY(pdf.GetX(), pdf.GetY()) // Reset position
	pdf.Cell(95, 10, "Shop Address:")
	pdf.SetXY(startX+135, pdf.GetY()) // Adjust Y to align with Shipping Address
	pdf.Cell(95, 10, "Billing Address:")
	pdf.Ln(8)
	pdf.SetFont("Arial", "", 10)
	y = pdf.GetY()
	pdf.SetXY(pdf.GetX(), y) // Reset position
	pdf.MultiCell(95, 6, fmt.Sprintf("Shop Name: %s\n%s\nMobile No: %s", shopData["shop_name"], shopAddress["street"], shopData["mobile_number"]), "", "", false)
	pdf.SetXY(pdf.GetX()+135, y) // Reset to the same starting X position
	pdf.MultiCell(95, 6, fmt.Sprintf("Customer Name: %s\n%s\nMobile No: %s", invoice["customer_name"].(string), invoice["customer_address"], invoice["customer_mobile_no"]), "", "", false)
	// Billing Address

	pdf.Ln(12)

	// Table header
	// pdf.SetFont("Arial", "B", 10)
	// pdf.SetXY(startX, pdf.GetY()) // Reset position for table header
	// pdf.CellFormat(10, 10, "#", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(30, 10, "HSNC", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(50, 10, "Description", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(20, 10, "MRP", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(20, 10, "Price", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(10, 10, "Qty", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(20, 10, "Amount", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(10, 10, "GST%", "1", 0, "C", false, 0, "")
	// pdf.CellFormat(20, 10, "GST Total", "1", 1, "C", false, 0, "")
	// Table content

	pdf.SetFont("Arial", "", 10)

	headersMap := []string{"#", "HSNC", "Description", "MRP", "Price", "Qty", "Amount", "GST%", "GST", "Total"}
	headersWithDataType := convertHeaders(headersMap, false)
	finalHeadersData := GetWidthForPdfTable(headersWithDataType, headersMap)

	pdf.addTable1(finalHeadersData, billingProducts)
	pdf.Ln(8)
	// GST Tax Details
	pdf.SetFont("Arial", "B", 10)
	pageWidth, _ = pdf.GetPageSize()
	cellWidth := 50.0
	//cellHeight := 10.0

	// Calculate X position to center the cell
	centerX = (pageWidth - cellWidth) / 2
	pdf.SetXY(centerX, pdf.GetY()) // Reset position for GST details
	pdf.CellFormat(50, 10, "GST Tax Details", "0", 1, "C", false, 0, "")
	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(30, 10, "Value", "1", 0, "C", false, 0, "") //1

	pdf.CellFormat(30, 10, "CGST%", "1", 0, "C", false, 0, "")    //2
	pdf.CellFormat(30, 10, "CGST Amt", "1", 0, "C", false, 0, "") //3
	pdf.CellFormat(30, 10, "SGST%", "1", 0, "C", false, 0, "")    //4
	pdf.CellFormat(30, 10, "SGST Amt", "1", 0, "C", false, 0, "") //5
	pdf.CellFormat(30, 10, "Tax Amt", "1", 1, "C", false, 0, "")  //6

	pdf.CellFormat(30, 10, ToString(ConvertToDataType("float64", ToString(invoice["amount"]))), "1", 0, "C", false, 0, "") //1

	pdf.CellFormat(30, 10, "6", "1", 0, "C", false, 0, "")                                                                      //2
	pdf.CellFormat(30, 10, ToString(ConvertToDataType("float64", ToString(invoice["cgst_amount"]))), "1", 0, "C", false, 0, "") //3 cgst_amount
	pdf.CellFormat(30, 10, "6", "1", 0, "C", false, 0, "")                                                                      //4
	pdf.CellFormat(30, 10, ToString(ConvertToDataType("float64", ToString(invoice["sgst_amount"]))), "1", 0, "C", false, 0, "") //5

	taxAmount := (invoice["sgst_amount"].(float64) + invoice["cgst_amount"].(float64))
	formatedTax := fmt.Sprintf("%.2f", taxAmount)
	pdf.CellFormat(30, 10, formatedTax, "1", 1, "C", false, 0, "") //6

	pdf.Ln(12)

	// Footer

	pdf.SetFont("Arial", "I", 8)
	pdf.SetXY(startX, pdf.GetY()) // Reset position for footer
	pdf.CellFormat(190, 10, "All disputes related to this order are subject to the jurisdiction of courts at Chennai, Tamil Nadu.", "0", 1, "C", false, 0, "")
	pdf.Ln(12)
	pdf.SetFont("Arial", "B", 12)
	signX := pdf.GetX()
	signY := pdf.GetY()
	pdf.SetXY(signX, signY)
	//pdf.CellFormat(80, 10, "Sakthi Pharma Ltd", "0", 1, "L", false, 0, "")
	//pdf.Cell(95, 10, "Sakthi Pharma Ltd")

	pdf.CellFormat(190, 10, "Sakthi Pharma Ltd", "0", 1, "R", false, 0, "")
	pdf.Ln(12)
	pdf.CellFormat(190, 10, "_______________________", "0", 1, "R", false, 0, "")
	pdf.Ln(6)
	//pdf.SetXY(160, signY)
	pdf.CellFormat(190, 10, "Pharmacist Signature", "0", 1, "R", false, 0, "")
	pdf.Ln(12)
	pdf.CellFormat(190, 10, "_______________________", "0", 1, "R", false, 0, "")

	currentTime := time.Now()
	formattedTime := currentTime.Format("02-01-2006 15:04")

	// Replace spaces, dashes, and colons with underscores
	safeTime := strings.NewReplacer(" ", "_", "-", "_", ":", "_").Replace(formattedTime)

	filePath := fmt.Sprintf("/uploads/system/shop_invoice/%s__%s.pdf", "INV__", safeTime)

	// Output the PDF (assuming you have a valid pdf object)
	var buffer bytes.Buffer
	err := pdf.Output(&buffer)
	if err != nil {
		return nil, fmt.Errorf(err.Error())
	}

	// Get the PDF content as []byte
	//pdfContent := buffer.Bytes()
	// Create a *multipart.FileHeader from the opened file
	// fileHeader := &multipart.FileHeader{
	// 	Filename: "bank_statement.pdf",
	// }

	// link, err := UploadFile("sakthipharma", filePath, "", "Shop InVoice", pdfContent)
	// if err != nil {
	// 	return nil, Unexpected(err.Error())
	// }
	// filePath = "/uploads/system/shop_invoice/INCV_" + incNo
	// // err := pdf.OutputFileAndClose("invoice.pdf")

	// if err != nil {
	// 	panic(err)
	// }

	// Get the size of the generated PDF file
	// fileInfo, err := os.Stat(filePath)

	// if err != nil {
	// 	panic(err)
	// }

	//fileSize := fileInfo.Size()

	id := uuid.New().String()
	fileName := filepath.Base(filePath)

	apiResponse := bson.M{
		"_id":          id,
		"category":     "shop_invoice",
		"file_name":    ".pdf",
		"storage_name": "__" + safeTime,
		"extn":         filepath.Ext(fileName),
		//"file_path":    link,
		"active": "Y",
	}

	// InsertData(c, orgId, "shop_invoice", apiResponse)
	return apiResponse, nil
}

func GenerateI() error {

	pdf := GoPdf{gofpdf.New("P", "mm", "A4", "")}

	var allRes []map[string]interface{}
	res := map[string]interface{}{
		"type": "Column",
		"children": []map[string]interface{}{
			{
				"key_value_pair": false,
				"field_name":     "checkingdsdsdsdsdsdsdsdsdddddddddddddddddd",
				"field_value":    "ss",
				"position":       "L",
			},
			{
				"key_value_pair": false,
				"field_name":     "texting",
				"field_value":    "ss",
				"position":       "L",
			},
			{
				"key_value_pair": false,
				"field_name":     "texting",
				"field_value":    "ss",
				"position":       "L",
			},
			{
				"key_value_pair": false,
				"field_name":     "texting",
				"field_value":    "ss",
				"position":       "L",
			},
		},
	}
	allRes = append(allRes, res)
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	currentXpostion = pdf.GetX()
	currentYpostion = pdf.GetY()
	lineHeight = 10
	pdf.addLabel(allRes)

	// Save the PDF to a local file
	err := pdf.OutputFileAndClose("output.pdf")
	if err != nil {
		return err
	}
	return nil
}

func (pdf *GoPdf) addLabel(labelData []map[string]interface{}) {
	for _, obj := range labelData {
		structureType := obj["type"].(string)
		children := obj["children"].([]map[string]interface{})
		if structureType == "Column" {

			pdf.ColumnData(children)
		} else if structureType == "Row" {

			pdf.RowData(children)
		}
	}
}
func (pdf *GoPdf) RowData(rowData []map[string]interface{}) {
	// Define initial X position for row data
	xOffset := pdf.GetX() // Use pdf.GetX() to get current X position

	// Iterate through each item in the rowData
	for _, obj := range rowData {
		keyValuePair := obj["key_value_pair"].(bool)
		labelName := obj["field_name"].(string)
		position := obj["position"].(string)

		// Determine cell width based on content
		cellWidth := pdf.GetStringWidth(labelName)
		if keyValuePair {
			labelValue := obj["field_value"].(string)
			fullText := labelName + " : " + labelValue
			cellWidth = pdf.GetStringWidth(fullText)
			if cellWidth > 50 {
				// Handle multi-line case with MultiCell
				//pdf.SetX(xOffset)
				pdf.MultiCell(50, lineHeight, fullText, "", position, false)
			} else {
				//pdf.SetXY(xOffset, pdf.GetY())
				pdf.CellFormat(cellWidth, lineHeight, fullText, "", 0, position, false, 0, "")
			}
		} else {
			if cellWidth > 50 {
				// Handle multi-line case with MultiCell
				// pdf.SetX(xOffset)
				pdf.MultiCell(50, lineHeight, labelName, "", position, false)
			} else {
				// pdf.SetXY(xOffset, pdf.GetY())
				pdf.CellFormat(cellWidth, lineHeight, labelName, "", 0, position, false, 0, "")
			}
		}

		// Update the X position for the next cell in the same row
		xOffset += 60 // 50 for cell width + 10 for spacing

		// Reset X and update Y after MultiCell to ensure correct positioning
		pdf.SetXY(xOffset, 10)
	}
	// Update Y position for the next row
	//currentYpostion = pdf.GetY()
}

// func (pdf *GoPdf) RowData(rowData []map[string]interface{}) {
// 	// Define initial X position for row data
// 	xOffset := currentXpostion

// 	// Iterate through each item in the rowData
// 	for _, obj := range rowData {
// 		keyValuePair := obj["key_value_pair"].(bool)
// 		labelName := obj["field_name"].(string)
// 		position := obj["position"].(string)
// 		cellWidth := 0.0

// 		// Determine cell width based on whether it's a key-value pair
// 		if keyValuePair {
// 			labelValue := obj["field_value"].(string)
// 			cellWidth = pdf.GetStringWidth(labelName + " : " + labelValue)
// 			if cellWidth > 50 {
// 				modifiedLineHeight := getLineHeight(cellWidth)
// 				pdf.MultiCell(50, modifiedLineHeight, labelName, "", "L", false)
// 			} else {
// 				pdf.CellFormat(cellWidth, lineHeight, labelName+" : "+labelValue, "", 0, position, false, 0, "")
// 			}

// 		} else {
// 			cellWidth = pdf.GetStringWidth(labelName)
// 			pdf.CellFormat(cellWidth, lineHeight, labelName, "", 0, position, false, 0, "")
// 			if cellWidth > 50 {
// 				modifiedLineHeight := getLineHeight(cellWidth)
// 				fmt.Println(modifiedLineHeight, cellWidth)
// 				pdf.MultiCell(50, modifiedLineHeight, labelName, "", "L", false)
// 			} else {
// 				pdf.CellFormat(cellWidth, lineHeight, labelName, "", 0, position, false, 0, "")
// 			}
// 		}

// 		// Update the X position for the next cell in the same row
// 		xOffset += cellWidth + 10 // Add some spacing between cells
// 		pdf.SetXY(xOffset, currentYpostion)
// 	}
// 	// Update Y position for the next row
// 	currentYpostion += lineHeight
// }

func getLineHeight(cellWidth float64) float64 {
	noOfLines := cellWidth / 50
	return lineHeight * noOfLines
}

func (pdf *GoPdf) ColumnData(columnData []map[string]interface{}) {
	yPosition := pdf.GetY()
	for _, obj := range columnData {
		keyValuePair := obj["key_value_pair"].(bool)
		labelName := obj["field_name"].(string)
		position := obj["position"].(string)

		// Set position based on currentY and currentX
		pdf.SetXY(currentXpostion, yPosition)
		// Determine cell width based on content
		cellWidth := pdf.GetStringWidth(labelName)
		if keyValuePair {
			labelValue := obj["field_value"].(string)
			fullText := labelName + " : " + labelValue
			cellWidth = pdf.GetStringWidth(fullText)
			if cellWidth > 50 {
				// Handle multi-line case with MultiCell
				//pdf.SetX(xOffset)
				getHeight := cellWidth / 50
				totalLineHeight := getHeight * lineHeight
				yPosition += totalLineHeight
				pdf.MultiCell(50, lineHeight, fullText, "", position, false)
			} else {
				//pdf.SetXY(xOffset, pdf.GetY())
				pdf.CellFormat(cellWidth, lineHeight, fullText, "", 0, position, false, 0, "")
				// Update currentY for the next line
				yPosition += lineHeight
			}
		} else {
			if cellWidth > 50 {
				// Handle multi-line case with MultiCell
				// pdf.SetX(xOffset)
				getHeight := cellWidth / 50
				totalLineHeight := getHeight * lineHeight
				yPosition += totalLineHeight
				pdf.MultiCell(50, lineHeight, labelName, "", position, false)
			} else {
				// pdf.SetXY(xOffset, pdf.GetY())
				pdf.CellFormat(cellWidth, lineHeight, labelName, "", 0, position, false, 0, "")
				// Update currentY for the next line
				yPosition += lineHeight
			}
		}

	}
}

func (pdf *GoPdf) addTable1(headers []map[string]interface{}, data []interface{}) {

	const marginCell = 4.0

	_, pageHeight := pdf.GetPageSize()
	_, _, _, marginBottom := pdf.GetMargins()
	pdf.SetFont("Arial", "B", 13)
	pdf.header(headers)
	pdf.SetFont("Arial", "", 11)
	f := &numfmt.Formatter{}
	//var isEvenRow bool
	// columnWidth := math.Ceil(190 / float64(len(headers)))

	for _, rowInterface := range data {

		curX, y := pdf.GetXY()
		x := curX
		totalHeight := 0.0
		var rowCount = 0
		if row, ok := rowInterface.(primitive.M); ok {

			for _, header := range headers {
				columnWidth := header["width"].(float64)

				str := header["fieldName"].(string)
				allHeaders := header["totalHeaders"].([]string)
				if value, exists := row[str]; exists {
					var valueAlign string
					switch value.(type) {
					case string:
						valueAlign = "L"
					case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
						valueAlign = "R"
					case float32, float64:
						valueAlign = "R"
						value = fmt.Sprintf("%.2f", value)
					default:
						valueAlign = "L"
					}
					stringValue := fmt.Sprintf("%v", value)
					_, lineHeight := pdf.GetFontSize()
					//	lastCellWidth := math.Ceil(pdf.GetStringWidth(stringValue))
					maxHeight := pdf.CreateBorderBasedValue(row, allHeaders, columnWidth)

					totalHeight = math.Max(totalHeight, maxHeight)

					if pdf.GetY()+totalHeight > pageHeight-marginBottom {
						pdf.SetDrawColor(0, 0, 0)
						pdf.AddPage()
						pdf.header(headers)
						pdf.SetFont("Arial", "", 11)
						y = pdf.GetY()
						curX, _ = pdf.GetXY()
						x = curX

					}

					if str == "#" {
						rowCount = rowCount + 1
						stringValue = ToString(rowCount)
					}
					if str == "Lot#" || str == "Date" || str == "Warehouse" {
						valueAlign = "C"
					}
					if str == "Quantity" || str == "Amount" || str == "Weight" || str == "Available Stock" {
						stringValue = f.Format(stringValue)
					}

					if str == "Transaction Type" {

						if value == "Cr" {
							pdf.SetTextColor(0, 128, 0) // Green for Credit
						} else if value == "Dr" {
							pdf.SetTextColor(255, 0, 0) // Red for Debit
						} else {
							pdf.SetTextColor(0, 0, 0) // Reset text color for other cells
						}
					} else {
						pdf.SetTextColor(0, 0, 0) // Reset text color for other cells
					}
					pdf.SetDrawColor(200, 200, 200)
					if stringValue == "" {
						pdf.SetDrawColor(255, 255, 255) // Set draw color to white
						//pdf.SetFillColor(255, 255, 255)
						pdf.Rect(x, y, columnWidth, totalHeight, "D")
					} else {
						//pdf.SetDrawColor(200, 200, 200)
						//pdf.SetFillColor(200, 200, 200)
						pdf.Rect(x, y, columnWidth, totalHeight, "FD")
					}
					// tempColumnWidth := columnWidth
					// if str == "Description" {
					// 	columnWidth = columnWidth + 10
					// }
					// Draw the background color for the entire cell

					pdf.MultiCell(columnWidth, lineHeight+marginCell, stringValue, "", valueAlign, false)
					x += columnWidth
					pdf.SetXY(x, y)
					//columnWidth = tempColumnWidth

				}
			}
		} else {
			fmt.Println("error")
		}

		pdf.SetXY(curX, y+totalHeight)
	}

	pdf.footer()
}

func (pdf *GoPdf) header(hdr []map[string]interface{}) *GoPdf {
	// pdf.SetFont("Times", "B", 16)
	pdf.SetFillColor(240, 240, 240)

	// columnWidth := math.Ceil(190 / float64(len(hdr)))
	cellHeight := 7.0
	for _, strData := range hdr {

		pdf.SetDrawColor(200, 200, 200)
		pdf.SetFillColor(173, 216, 230)
		columnWidth := strData["width"].(float64)
		str := strData["fieldName"].(string)

		lastCellWidth := math.Ceil(pdf.GetStringWidth(str))
		if lastCellWidth > columnWidth {
			cellHeight = cellHeight + 2
		}
		if str == "Amount" {
			str = "Amount (₹)"
			pdf.AddUTF8Font("Arial", "B", "arial.ttf")

			pdf.SetFont("Arial", "B", 14)
		} else if str == "Weight" {

			str = "Weight (kg)"
		} else {
			pdf.SetFont("Arial", "B", 13)
		}
		// tempColumnWidth := columnWidth
		// if str == "Description" {
		// 	columnWidth = columnWidth + 10
		// }

		pdf.CellFormat(columnWidth, cellHeight, str, "1", 0, "C", true, 0, "")
		pdf.SetFillColor(240, 240, 240)
		//columnWidth = tempColumnWidth
	}

	pdf.Ln(-1)
	return pdf
}

func (pdf *GoPdf) footer() {
	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		// pdf.SetFont("Arial", "", 8)
		pdf.CellFormat(0, 0, fmt.Sprintf("Page %d", pdf.PageNo()), "0", 0, "R", false, 0, "")
	})
}

// func (pdf *GoPdf) CreateBorderBasedValue(row map[string]interface{}, headers []string, cellWidth float64) float64 {
// 	maxHeight := 0.0

// 	marginCell := 2.0
// 	for _, header := range headers {
// 		if value, exists := row[header]; exists {
// 			stringValue := fmt.Sprintf("%v", value)
// 			_, lineHeight := pdf.GetFontSize()
// 			lines := pdf.SplitLines([]byte(stringValue), cellWidth)

// 			cellHeight := float64(len(lines))*lineHeight + marginCell*float64(len(lines))

// 			maxHeight = math.Max(maxHeight, cellHeight)

// 		}
// 	}

//		return maxHeight
//	}
func (pdf *GoPdf) CreateBorderBasedValue(row map[string]interface{}, headers []string, cellWidth float64) float64 {
	maxHeight := 0.0

	for _, header := range headers {
		if value, exists := row[header]; exists {
			stringValue := fmt.Sprintf("%v", value)
			stringWidth := pdf.GetStringWidth(stringValue)
			_, lineHeight := pdf.GetFontSize()
			// Calculate the number of lines needed
			numberOfLines := math.Ceil(stringWidth / cellWidth)

			// Calculate the total cell height
			cellHeight := numberOfLines * lineHeight * 1.8

			maxHeight = math.Max(maxHeight, cellHeight)
		}
	}

	return maxHeight
}
func GetWidthForPdfTable(data []map[string]interface{}, totalAllHeaders []string) []map[string]interface{} {
	overallTableWidth := 190.0                 // Total width available for the table
	columnWidths := make([]float64, len(data)) // Array to hold the width of each column
	columnLen := len(data)
	//countedWidth := 0
	// Iterate over the data to calculate the width for each column
	for i, obj := range data {
		value := obj["fieldName"].(string)
		dataType := obj["dataType"].(string)
		multicell := obj["multicell"].(bool)

		switch dataType {
		case "string":
			// Assuming an average character width for string data
			if columnLen > 5 {
				if multicell {
					columnWidths[i] = float64(len(value)) * 2.4
				} else {
					strLen := float64(len(value))
					if strLen > 15 {
						columnWidths[i] = strLen
					} else {
						columnWidths[i] = 18
					}
				}
			} else {
				columnWidths[i] = overallTableWidth / float64(columnLen)
			}
		case "int", "float":
			// Setting a fixed width for numeric data
			if columnLen > 5 {
				columnWidths[i] = 18.0
			} else {
				columnWidths[i] = overallTableWidth / float64(columnLen)
			}
		case "date":
			// Setting a fixed width for date data
			if columnLen > 5 {
				columnWidths[i] = 25.0
			} else {
				columnWidths[i] = overallTableWidth / float64(columnLen)
			}
		default:
			// Default width for unknown data types
			if columnLen > 5 {
				columnWidths[i] = 15.0
			} else {
				columnWidths[i] = overallTableWidth / float64(columnLen)
			}
		}
	}

	// Normalize column widths to fit within the overallTableWidth
	totalWidth := 0.0
	for _, width := range columnWidths {
		totalWidth += width
	}

	if totalWidth > overallTableWidth {
		scaleFactor := overallTableWidth / totalWidth
		for i := range columnWidths {
			columnWidths[i] *= scaleFactor
		}
	}

	// Create the result slice with updated widths
	result := make([]map[string]interface{}, len(data))
	for i, obj := range data {

		result[i] = map[string]interface{}{
			"fieldName":    obj["fieldName"],
			"dataType":     obj["dataType"],
			"multicell":    obj["multicell"],
			"totalHeaders": totalAllHeaders,
			"width":        math.Ceil(columnWidths[i]), // Round up to the nearest integer
		}
	}

	return result
}

func convertHeaders(headers []string, allFloat bool) []map[string]interface{} {
	// Define a map of data types for each header
	dataTypes := map[string]string{
		"Date":      "date",
		"Buyer":     "string",
		"Weight":    "float",
		"Rate":      "float",
		"Amount":    "float",
		"Lot#":      "string",
		"Warehouse": "string",
		"Grade":     "string",
		"Quantity":  "float",
	}

	// Define a map to specify whether each header should use multicell
	multicellMap := map[string]bool{
		"Date":      false,
		"Buyer":     false,
		"Weight":    false,
		"Rate":      false,
		"Price":     false,
		"Qty":       false,
		"Amount":    false,
		"GST%":      false,
		"GST":       false,
		"Total":     false,
		"Lot#":      false,
		"Warehouse": false,
		"Grade":     false,
		"Quantity":  false,
	}

	headersWithDataTypes := make([]map[string]interface{}, len(headers))
	if !allFloat {
		for i, header := range headers {
			headersWithDataTypes[i] = map[string]interface{}{
				"fieldName": header,
				"dataType":  dataTypes[header],
				"multicell": multicellMap[header],
			}
		}
	} else {
		for i, header := range headers {
			headersWithDataTypes[i] = map[string]interface{}{
				"fieldName": header,
				"dataType":  "float",
				"multicell": false,
			}
		}
	}

	return headersWithDataTypes
}

func GenerateSaleReport(data map[string]interface{}, org Organization, userData utils.UserToken) (map[string]interface{}, error) {

	pdf := GoPdf{gofpdf.New("P", "mm", "A4", "")}

	// Add a new page
	pdf.AddPage()

	// Set font and add title
	pdf.SetFont("Arial", "B", 20)
	title := "Stock Report"
	pdf.CellFormat(0, 10, title, "", 1, "C", false, 0, "") // Add the title at the center
	pdf.SetFont("Arial", "", 16)
	pdf.Ln(12)

	// Add key-value pairs dynamically
	originName := data["origin_name"].(string)
	qualityReports := data["quality_reports"].(primitive.M)
	purchaseDate := data["purchased_date"].(string)
	var nutCount int64
	if data["nut_count"] != nil {
		nutCount = data["nut_count"].(int64)
	}

	var outTurn int64
	if data["out_turn"] != nil {
		outTurn = data["out_turn"].(int64)
	}

	supplierName := data["supplier_name"].(string)
	purchaseWeight := data["purchase_weight"].(float64)
	availableWeight := qualityReports["net_weight"].(float64)

	f := &numfmt.Formatter{}

	addKeyValuePairs(pdf.Fpdf, "Origin", originName, "Supplier", supplierName)
	addKeyValuePairs(pdf.Fpdf, "Purchase Date", purchaseDate, "Purchase Weight (kg)", f.Format(purchaseWeight))
	pdf.AddUTF8Font("Arial", "B", "arial.ttf")
	addKeyValuePairs(pdf.Fpdf, "Purchase Amount (₹)", f.Format(data["purchase_price"].(string)), "RCN Available Weight (kg)", f.Format(availableWeight))
	addKeyValuePairs(pdf.Fpdf, "Nut Count", ToString(nutCount), "Out Turn", ToString(outTurn))

	pdf.SetFont("Arial", "B", 15)
	pdf.Ln(12)
	pdf.CellFormat(0, 10, "Sales :", "", 1, "L", false, 0, "")
	headersMap := []string{"Date", "Sale Type", "Buyer", "Weight", "Amount"}
	headersWithDataType := convertHeaders(headersMap, false)
	finalHeadersData := GetWidthForPdfTable(headersWithDataType, headersMap)
	// x := pdf.GetX()
	// y := pdf.GetY()
	// pdf.SetXY(x, y)

	// Sample data for billing products
	// Sample data for billing products using []interface{}
	billingProducts := data["sale_result"].(primitive.A)

	pdf.addTable1(finalHeadersData, billingProducts)
	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 15)
	pdf.Ln(12)
	pdf.CellFormat(0, 10, "Productions :", "", 1, "L", false, 0, "")

	headersMap1 := []string{"Date", "Lot#", "Weight"}
	headersWithDataType1 := convertHeaders(headersMap1, false)
	finalHeadersData1 := GetWidthForPdfTable(headersWithDataType1, headersMap1)
	billingProducts1 := data["lot_result"].(primitive.A)
	// billingProducts1 := []interface{}{
	// 	primitive.M{
	// 		"Date":   "2024-09-27",
	// 		"Lot#":   "ABC Enterprises",
	// 		"Weight": 120.50,
	// 	},
	// 	primitive.M{
	// 		"Date":   "2024-09-28",
	// 		"Lot#":   "XYZ Retailer",
	// 		"Weight": 130.00,
	// 	},
	// 	primitive.M{
	// 		"Date":   "2024-09-29",
	// 		"Lot#":   "DEF Traders",
	// 		"Weight": 125.75,
	// 	},
	// }
	pdf.addTable1(finalHeadersData1, billingProducts1)
	// pdf.Ln(8)
	// pdf.SetFont("Arial", "B", 15)
	// pdf.Ln(12)
	// pdf.CellFormat(0, 10, "Final Output :", "", 1, "L", false, 0, "")

	// headersMap2 := data["finalProducts"].([]string)
	// headersWithDataType2 := convertHeaders(headersMap2, true)
	// finalHeadersData2 := GetWidthForPdfTable(headersWithDataType2, headersMap2)

	// billingProducts2 := []interface{}{
	// 	primitive.M{
	// 		"Date":   "2024-09-27",
	// 		"Lot#":   "ABC Enterprises",
	// 		"Weight": 120.50,
	// 	},
	// 	primitive.M{
	// 		"Date":   "2024-09-28",
	// 		"Lot#":   "XYZ Retailer",
	// 		"Weight": 130.00,
	// 	},
	// 	primitive.M{
	// 		"Date":   "2024-09-29",
	// 		"Lot#":   "DEF Traders",
	// 		"Weight": 125.75,
	// 	},
	// }
	// pdf.addTable1(finalHeadersData2, billingProducts2)
	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 15)
	pdf.Ln(12)
	pdf.CellFormat(0, 10, "RCN Stock In-Hands :", "", 1, "L", false, 0, "")
	headersMap2 := []string{"Warehouse", "Available Stock"}
	headersWithDataType2 := convertHeaders(headersMap2, false)
	finalHeadersData2 := GetWidthForPdfTable(headersWithDataType2, headersMap2)
	billingProducts2 := data["warehouse_result1"].(primitive.M)
	var arrOfObject []interface{}
	arrOfObject = append(arrOfObject, billingProducts2)
	pdf.addTable1(finalHeadersData2, arrOfObject)

	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 15)
	pdf.Ln(12)
	pdf.CellFormat(0, 10, "Final Output :", "", 1, "L", false, 0, "")
	headersMap3 := []string{"Grade", "Weight"}
	headersWithDataType3 := convertHeaders(headersMap3, false)
	finalHeadersData3 := GetWidthForPdfTable(headersWithDataType3, headersMap3)
	billingProducts3 := data["grade"].(primitive.A)
	//var arrOfObject1 []interface{}
	//arrOfObject = append(arrOfObject, billingProducts2)
	pdf.addTable1(finalHeadersData3, billingProducts3)

	currentTime := time.Now()
	formattedTime := currentTime.Format("02-01-2006 15:04")

	// Replace spaces, dashes, and colons with underscores
	safeTime := strings.NewReplacer(" ", "_", "-", "_", ":", "_").Replace(formattedTime)

	filePath := fmt.Sprintf("stockReport_%s.pdf", safeTime)

	// Output the PDF (assuming you have a valid pdf object)
	var buffer bytes.Buffer
	err := pdf.Output(&buffer)
	if err != nil {
		return nil, fmt.Errorf(err.Error())
	}

	// Get the PDF content as []byte
	pdfContent := buffer.Bytes()

	// Create a *multipart.FileHeader from the opened file
	fileHeader := &multipart.FileHeader{
		Filename: filePath,
	}

	filePath, err = UploadGeneratedFile(pdfContent, fileHeader.Filename, org.Id, userData.UserId)
	if err != nil {
		return nil, fmt.Errorf(err.Error())
	}
	//filePath = "/uploads/system/shop_invoice/INCV_" + incNo
	// err := pdf.OutputFileAndClose("invoice.pdf")

	// if err != nil {
	// 	panic(err)
	// }

	// Get the size of the generated PDF file
	// fileInfo, err := os.Stat(filePath)

	// if err != nil {
	// 	panic(err)
	// }

	//fileSize := fileInfo.Size()

	id := uuid.New().String()
	fileName := filepath.Base(filePath)

	apiResponse := bson.M{
		"_id":          id,
		"category":     "shop_invoice",
		"file_name":    ".pdf",
		"storage_name": "__" + safeTime,
		"extn":         filepath.Ext(fileName),
		//"file_path":    link,
		"active": "Y",
	}

	// InsertData(c, orgId, "shop_invoice", apiResponse)
	return apiResponse, nil
}

// func addKeyValuePairs(pdf *gofpdf.Fpdf, leftKey, leftValue, rightKey, rightValue string) {
// 	// Get page width to calculate the position for the right side
// 	pageWidth, _ := pdf.GetPageSize()
// 	margin := 10.0 // Margin on the left and right sides
// 	usableWidth := pageWidth - 2*margin

// 	// Left side key-value pair
// 	pdf.SetX(margin)
// 	pdf.CellFormat(usableWidth/2, 10, leftKey+": "+leftValue, "", 0, "L", false, 0, "")

// 	// Right side key-value pair
// 	pdf.SetX(margin + usableWidth/2)
// 	pdf.CellFormat(usableWidth/2, 10, rightKey+": "+rightValue, "", 1, "R", false, 0, "")
// }

// func addKeyValuePairs(pdf *gofpdf.Fpdf, leftKey, leftValue, rightKey, rightValue string) {
// 	// Set the page margins
// 	margin := 10.0
// 	pageWidth, _ := pdf.GetPageSize()
// 	usableWidth := pageWidth - 2*margin

// 	// Define cell height
// 	cellHeight := 10.0

// 	// Calculate the width for the key and value cells
// 	keyWidth := 30.0                       // Width allocated for the key
// 	valueWidth := usableWidth/2 - keyWidth // Width allocated for the value

// 	// Left side key-value pair
// 	pdf.SetX(margin) // Set position for left side

// 	pdf.SetFont("Arial", "B", 12) // Bold for key
// 	pdf.CellFormat(keyWidth, cellHeight, leftKey+" :", "", 0, "L", false, 0, "")

// 	pdf.SetFont("Arial", "", 12) // Regular for value
// 	pdf.CellFormat(valueWidth, cellHeight, leftValue, "", 0, "L", false, 0, "")

// 	// Right side key-value pair
// 	pdf.SetX(margin + usableWidth/1.5) // Set position for right side

// 	pdf.SetFont("Arial", "B", 12) // Bold for key
// 	pdf.CellFormat(keyWidth, cellHeight, rightKey+" :", "", 0, "L", false, 0, "")

// 	pdf.SetFont("Arial", "", 12) // Regular for value
// 	pdf.CellFormat(valueWidth, cellHeight, rightValue, "", 1, "L", false, 0, "")
// }

// func addKeyValuePairs(pdf *gofpdf.Fpdf, leftKey, leftValue, rightKey, rightValue string) {  // working
// 	// Set the page margins
// 	margin := 10.0
// 	pageWidth, _ := pdf.GetPageSize()
// 	usableWidth := pageWidth - 2*margin

// 	// Define cell height and key-width adjustments
// 	cellHeight := 10.0
// 	keyWidth := 35.0                       // Reduce key width to avoid extra space
// 	valueWidth := usableWidth/2 - keyWidth // Calculate value width

// 	// Left side key-value pair
// 	pdf.SetX(margin) // Set position for left side

// 	pdf.SetFont("Arial", "B", 11) // Bold for key
// 	pdf.CellFormat(keyWidth, cellHeight, leftKey+":", "", 0, "L", false, 0, "")

// 	pdf.SetFont("Arial", "", 11) // Regular for value
// 	pdf.CellFormat(valueWidth, cellHeight, leftValue, "", 0, "L", false, 0, "")

// 	// Right side key-value pair
// 	pdf.SetX(margin + usableWidth/1.5) // Set position for right side

// 	pdf.SetFont("Arial", "B", 11) // Bold for key
// 	pdf.CellFormat(keyWidth, cellHeight, rightKey+":", "", 0, "L", false, 0, "")

// 	pdf.SetFont("Arial", "", 11) // Regular for value
// 	pdf.CellFormat(valueWidth, cellHeight, rightValue, "", 1, "L", false, 0, "")
// }

// func addKeyValuePairs(pdf *gofpdf.Fpdf, leftKey, leftValue, rightKey, rightValue string) { //working
// 	// Set the page margins
// 	margin := 10.0
// 	pageWidth, _ := pdf.GetPageSize()
// 	usableWidth := pageWidth - 2*margin
// 	cellHeight := 10.0 // Define cell height

// 	// Calculate widths dynamically based on text length
// 	//keyPadding := 5.0   // Padding around the key text
// 	//valuePadding := 3.0 // Padding around the value text

// 	// Get the widths for the keys and values
// 	leftKeyWidth := pdf.GetStringWidth(leftKey + " : ")
// 	leftValueWidth := pdf.GetStringWidth(leftValue)

// 	rightKeyWidth := pdf.GetStringWidth(rightKey + " : ")
// 	rightValueWidth := pdf.GetStringWidth(rightValue)

// 	// Determine max widths to maintain alignment
// 	// maxKeyWidth := leftKeyWidth
// 	// if rightKeyWidth > maxKeyWidth {
// 	// 	maxKeyWidth = rightKeyWidth
// 	// }

// 	// Determine available width for values
// 	// availableValueWidth := usableWidth/2 - maxKeyWidth

// 	// // Ensure values fit within the remaining space
// 	// if leftValueWidth > availableValueWidth {

// 	// 	leftValueWidth = availableValueWidth
// 	// }

// 	// if rightValueWidth > availableValueWidth {

// 	// 	rightValueWidth = availableValueWidth
// 	// }

// 	LeftmaxKeyWidth := 30.0
// 	rightmaxKeyWidth := 30.0
// 	leftavailableValueWidth := 25.0
// 	rightavailableValueWidth := 25.0

// 	if leftKeyWidth > LeftmaxKeyWidth {
// 		LeftmaxKeyWidth = leftKeyWidth
// 	}
// 	if rightValueWidth > rightmaxKeyWidth {
// 		rightavailableValueWidth = rightValueWidth
// 	}
// 	if rightKeyWidth > rightmaxKeyWidth {
// 		rightmaxKeyWidth = rightKeyWidth
// 	}
// 	if leftValueWidth > leftavailableValueWidth {
// 		leftavailableValueWidth = leftValueWidth
// 	}

// 	// Left side key-value pair
// 	pdf.SetX(margin) // Set position for left side

// 	pdf.SetFont("Arial", "B", 12) // Bold for key
// 	pdf.CellFormat(LeftmaxKeyWidth, cellHeight, leftKey+" : ", "", 0, "L", false, 0, "")

// 	pdf.SetFont("Arial", "", 12) // Regular for value
// 	pdf.CellFormat(leftavailableValueWidth, cellHeight, leftValue, "", 0, "L", false, 0, "")

// 	// Right side key-value pair
// 	pdf.SetX(margin + usableWidth/2) // Set position for right side

// 	pdf.SetFont("Arial", "B", 12) // Bold for key
// 	pdf.CellFormat(rightmaxKeyWidth, cellHeight, rightKey+" : ", "", 0, "L", false, 0, "")

// 	pdf.SetFont("Arial", "", 12) // Regular for value
// 	pdf.CellFormat(rightavailableValueWidth, cellHeight, rightValue, "", 1, "L", false, 0, "")
// }

// func addKeyValuePairs(pdf *gofpdf.Fpdf, leftKey, leftValue, rightKey, rightValue string) { need to remove space
// 	// Set the page margins
// 	margin := 10.0
// 	pageWidth, _ := pdf.GetPageSize()
// 	usableWidth := pageWidth - 2*margin
// 	cellHeight := 10.0 // Define cell height

// 	// Define max widths for keys and available widths for values
// 	maxKeyWidth := 40.0 // Adjusted max key width for alignment
// 	leftAvailableValueWidth := usableWidth/2 - maxKeyWidth
// 	rightAvailableValueWidth := usableWidth/2 - maxKeyWidth

// 	// Get the widths for the keys and values
// 	leftKeyWidth := pdf.GetStringWidth(leftKey + " : ")
// 	leftValueWidth := pdf.GetStringWidth(leftValue)

// 	rightKeyWidth := pdf.GetStringWidth(rightKey + " : ")
// 	rightValueWidth := pdf.GetStringWidth(rightValue)

// 	// Determine the actual widths based on the max limits and available widths
// 	if leftKeyWidth > maxKeyWidth {
// 		maxKeyWidth = leftKeyWidth
// 	}
// 	if rightKeyWidth > maxKeyWidth {
// 		maxKeyWidth = rightKeyWidth
// 	}

// 	// Ensure values fit within the remaining space
// 	if leftValueWidth > leftAvailableValueWidth {
// 		leftValueWidth = leftAvailableValueWidth
// 	}
// 	if rightValueWidth > rightAvailableValueWidth {
// 		rightValueWidth = rightAvailableValueWidth
// 	}

// 	// Left side key-value pair
// 	pdf.SetX(margin)              // Set position for left side
// 	pdf.SetFont("Arial", "B", 12) // Bold for key
// 	pdf.CellFormat(maxKeyWidth, cellHeight, leftKey+" : ", "", 0, "L", false, 0, "")
// 	pdf.SetFont("Arial", "", 12) // Regular for value
// 	pdf.CellFormat(leftValueWidth, cellHeight, leftValue, "", 0, "L", false, 0, "")

// 	// Move to the right side
// 	pdf.SetX(margin + usableWidth/2) // Set position for right side
// 	pdf.SetFont("Arial", "B", 12)    // Bold for key
// 	pdf.CellFormat(maxKeyWidth, cellHeight, rightKey+" : ", "", 0, "L", false, 0, "")
// 	pdf.SetFont("Arial", "", 12) // Regular for value
// 	pdf.CellFormat(rightValueWidth, cellHeight, rightValue, "", 1, "L", false, 0, "")
// }

// addKeyValuePairs adds key-value pairs to the PDF, aligning them in two columns
func addKeyValuePairs(pdf *gofpdf.Fpdf, leftKey, leftValue, rightKey, rightValue string) {
	// Set the page margins
	margin := 10.0
	pageWidth, _ := pdf.GetPageSize()
	usableWidth := pageWidth - 2*margin
	cellHeight := 10.0 // Define cell height

	// Get the widths for the keys and values
	leftKeyWidth := pdf.GetStringWidth(leftKey + " : ")
	leftValueWidth := pdf.GetStringWidth(leftValue)

	rightKeyWidth := pdf.GetStringWidth(rightKey + " : ")
	rightValueWidth := pdf.GetStringWidth(rightValue)

	// Define max key width for better alignment and a slight padding
	maxKeyWidth := 30.0 // You can adjust this value based on your needs

	// Set the maximum widths if the calculated widths exceed them
	if maxKeyWidth < leftValueWidth {
		maxKeyWidth = leftKeyWidth
	}

	// Calculate the available space for the values, ensuring no extra space
	// leftValueWidth = leftKeyWidth + 5 // Reduce width for value

	// Use the smaller available width for the values to avoid overflow
	// if leftValueWidth > leftAvailableValueWidth {
	// 	leftValueWidth = leftAvailableValueWidth
	// }
	// if rightValueWidth > rightAvailableValueWidth {
	// 	rightValueWidth = rightAvailableValueWidth
	// }

	// Adjust the position for the left side key-value pair
	pdf.SetX(margin)              // Set position for left side
	pdf.SetFont("Arial", "B", 12) // Bold for key
	if leftKey == "Origin" {

		leftKeyWidth = 15
	}
	if rightKey == "Purchase Weight (kg)" {

		rightKeyWidth = 46
	} // Supplier

	if rightKey == "Supplier" {

		rightKeyWidth = 20
	}
	if leftKey == "Purchase Amount (₹)" {
		leftKeyWidth = leftKeyWidth - 3
	}
	if rightKey == "RCN Available Weight (kg)" {

		rightKeyWidth = rightKeyWidth - 1
	}

	pdf.CellFormat(leftKeyWidth, cellHeight, leftKey+" : ", "", 0, "L", false, 0, "")
	pdf.SetFont("Arial", "", 12) // Regular for value
	pdf.CellFormat(maxKeyWidth, cellHeight, " "+leftValue, "", 0, "L", false, 0, "")

	if maxKeyWidth < rightValueWidth {

		maxKeyWidth = rightKeyWidth
	}

	// Move to the right side
	marginRightValue := margin + usableWidth/2
	pdf.SetX(marginRightValue) // Set position for right side

	pdf.SetFont("Arial", "B", 12) // Bold for key
	pdf.CellFormat(rightKeyWidth, cellHeight, rightKey+" : ", "", 0, "L", false, 0, "")
	//pdf.SetX(marginRightValue + maxKeyWidth + 5)

	pdf.SetFont("Arial", "", 12) // Regular for value
	pdf.CellFormat(maxKeyWidth, cellHeight, " "+rightValue, "", 1, "L", false, 0, "")
}
