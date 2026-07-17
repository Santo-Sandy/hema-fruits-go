package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
)

// toSlice converts BSON primitive.A or []interface{} to []interface{}
func toSlice(v interface{}) ([]interface{}, bool) {
	switch val := v.(type) {
	case []interface{}:
		return val, true
	case primitive.A:
		return []interface{}(val), true
	default:
		return nil, false
	}
}

// toMap converts BSON primitive.M or map[string]interface{} to map[string]interface{}
func toMap(v interface{}) (map[string]interface{}, bool) {
	switch val := v.(type) {
	case map[string]interface{}:
		return val, true
	case primitive.M:
		return map[string]interface{}(val), true
	default:
		return nil, false
	}
}

// NodePDFExportRequest - Request structure for Node.js PDF service
type NodePDFExportRequest struct {
	TemplateID string                 `json:"templateId"`
	Params     map[string]interface{} `json:"params,omitempty"`
}

// ExportPDFViaNodeHandler handles PDF export requests via Node.js service
// POST /api/pdf/export
func ExportPDFViaNodeHandler(c *fiber.Ctx) error {
	org, exists := GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	var request NodePDFExportRequest
	if err := c.BodyParser(&request); err != nil {
		return shared.BadRequest("Invalid request body: " + err.Error())
	}

	if request.TemplateID == "" {
		return shared.BadRequest("templateId is required")
	}

	// Fetch template from MongoDB
	template, err := fetchPDFTemplateFromDB(org.Id, request.TemplateID)
	if err != nil {
		return shared.BadRequest("Template not found: " + err.Error())
	}

	// Update template with request params
	if len(request.Params) > 0 {
		UpdateTemplateParams(template, request.Params, org.Id)
	}

	// Prepare payload for Node.js PDF service
	nodePDFPayload := CleanTemplateForExport(template)

	// Call Node.js PDF service
	nodePDFURL := os.Getenv("NODE_PDF_SERVICE_URL")
	if nodePDFURL == "" {
		nodePDFURL = "http://localhost:3002/api/pdf/export"
	}

	pdfBytes, err := CallNodePDFService(nodePDFURL, nodePDFPayload)
	if err != nil {
		return shared.InternalServerError("Failed to generate PDF: " + err.Error())
	}

	// Set response headers
	fileName := "document"
	if name, ok := template["name"].(string); ok && name != "" {
		fileName = name
	}

	c.Set("Content-Type", "application/pdf")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.pdf", fileName))

	return c.Send(pdfBytes)
}

// fetchPDFTemplateFromDB fetches template from MongoDB by ID
func fetchPDFTemplateFromDB(orgId, templateId string) (map[string]interface{}, error) {
	db := database.GetConnection(orgId)

	var template map[string]interface{}
	err := db.Collection("pdf-templates").FindOne(
		context.Background(),
		bson.M{"_id": templateId},
	).Decode(&template)

	if err != nil {
		return nil, err
	}

	return template, nil
}

// CleanTemplateForExport removes metadata fields from template
func CleanTemplateForExport(template map[string]interface{}) map[string]interface{} {
	fieldsToRemove := []string{"_id", "createdAt", "created_by", "created_on", "status", "update_by", "update_on", "updatedAt"}
	for _, field := range fieldsToRemove {
		delete(template, field)
	}
	return template
}

// CallNodePDFService calls the Node.js PDF generation service
func CallNodePDFService(url string, payload interface{}) ([]byte, error) {
	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Log the request payload
	// fmt.Printf("\n=== PDF Export Request Payload ===\n%s\n=================================\n\n", string(jsonData))

	// // Log sections count
	// var payloadMap map[string]interface{}
	// if err := json.Unmarshal(jsonData, &payloadMap); err == nil {
	// 	if sections, ok := toSlice(payloadMap["sections"]); ok {
	// 		fmt.Printf("Payload contains %d sections\n", len(sections))
	// 	}
	// 	if widgets, ok := toSlice(payloadMap["widgets"]); ok {
	// 		fmt.Printf("Payload contains %d top-level widgets\n", len(widgets))
	// 	}
	// }

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Node.js PDF service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Node.js PDF service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read response body
	pdfBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return pdfBytes, nil
}

// UpdateTemplateParams updates commonDatasets params with request params and re-fetches data
func UpdateTemplateParams(template map[string]interface{}, params map[string]interface{}, orgId string) {

	// Update commonDatasets params and re-fetch data
	if commonDatasets, ok := toSlice(template["commonDatasets"]); ok {
		for i, dataset := range commonDatasets {
			if ds, ok := toMap(dataset); ok {
				fmt.Printf("Dataset %d keys: %v\n", i, getMapKeys(ds))

				// Try multiple possible field names for params
				paramFieldNames := []string{"params", "FilterParams", "filterParams", "Params"}
				for _, fieldName := range paramFieldNames {
					if dsParams, found := toSlice(ds[fieldName]); found {
						for j, param := range dsParams {
							if p, ok := toMap(param); ok {
								fmt.Printf("Param %d keys: %v\n", j, getMapKeys(p))
								// Check multiple possible field names for param name
								var paramName string
								nameFields := []string{"parmasName", "ParmasName", "paramsName", "ParamsName", "name", "Name"}
								for _, nf := range nameFields {
									if name, ok := p[nf].(string); ok && name != "" {
										paramName = name
										fmt.Printf("Found paramName '%s' in field '%s'\n", paramName, nf)
										break
									}
								}
								if paramName != "" {
									if value, exists := params[paramName]; exists {
										// Only update Paramsvalue field (matching the template structure)
										p["Paramsvalue"] = value
										fmt.Printf("✓ Updated param '%s' with value: %v\n", paramName, value)
									} else {
										fmt.Printf("✗ No value provided for param '%s'\n", paramName)
									}
								}
							}
						}
					}
				}

				// Re-fetch data for this dataset using updated params
				refreshDatasetData(ds, orgId)
			}
		}
	} else {
		fmt.Printf("No commonDatasets found in template\n")
	}

	// Populate widget previewData from commonDatasets
	if widgets, ok := toSlice(template["widgets"]); ok {
		fmt.Printf("Processing %d top-level widgets\n", len(widgets))
		populateWidgetPreviewData(widgets, template, -1, -1)
	}

	// Process pageLayouts if they exist
	if pageLayouts, ok := toSlice(template["pageLayouts"]); ok {
		fmt.Printf("\n=== Processing %d pageLayouts ===\n", len(pageLayouts))
		for pIdx, pageLayout := range pageLayouts {
			if layoutMap, ok := toMap(pageLayout); ok {
				if layoutWidgets, ok := toSlice(layoutMap["widgets"]); ok {
					fmt.Printf("PageLayout %d has %d widgets\n", pIdx, len(layoutWidgets))
					populateWidgetPreviewData(layoutWidgets, template, pIdx, -1)
				}
			}
		}
	}

	// Process sections if they exist
	if sections, ok := template["sections"]; ok {
		if sectionsArray, ok := toSlice(sections); ok {
			fmt.Printf("\n=== Processing %d sections ===\n", len(sectionsArray))
			for sIdx, section := range sectionsArray {
				if sectionMap, ok := toMap(section); ok {
					if sectionWidgets, ok := toSlice(sectionMap["widgets"]); ok {
						fmt.Printf("Section %d has %d widgets\n", sIdx, len(sectionWidgets))
						populateWidgetPreviewData(sectionWidgets, template, -1, sIdx)
					}
				}
			}
		}
	}
}

// getMapKeys returns all keys from a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// populateWidgetPreviewData populates previewData for widgets from commonDatasets
func populateWidgetPreviewData(widgets []interface{}, template map[string]interface{}, pageLayoutIdx, sectionIdx int) {
	for i := range widgets {
		widgetMap, ok := widgets[i].(map[string]interface{})
		if !ok {
			if pm, ok := widgets[i].(primitive.M); ok {
				widgetMap = map[string]interface{}(pm)
				widgets[i] = widgetMap
			}
		}
		if widgetMap != nil {
			configMap, ok := widgetMap["config"].(map[string]interface{})
			if !ok {
				if pm, ok := widgetMap["config"].(primitive.M); ok {
					configMap = map[string]interface{}(pm)
					widgetMap["config"] = configMap
				}
			}
			if configMap != nil {
				if datasetId, ok := configMap["commonDatasetId"].(string); ok && datasetId != "" {
					if commonDatasets, ok := toSlice(template["commonDatasets"]); ok {
						for _, dataset := range commonDatasets {
							if ds, ok := toMap(dataset); ok {
								if id, ok := ds["id"].(string); ok && id == datasetId {
									if data, ok := ds["data"]; ok {
										if path, ok := configMap["commonDatasetPath"].(string); ok && path != "" {
											configMap["previewData"] = extractDataFromPath(data, path)
										} else {
											configMap["previewData"] = ensureArray(data)
										}
										if pageLayoutIdx >= 0 {
											fmt.Printf("[PageLayout %d Widget %d] Added previewData\n", pageLayoutIdx, i)
										} else if sectionIdx >= 0 {
											fmt.Printf("[Section %d Widget %d] Added previewData\n", sectionIdx, i)
										} else {
											fmt.Printf("[Widget %d] Added previewData\n", i)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// refreshDatasetData re-fetches data for a dataset using its params
func refreshDatasetData(ds map[string]interface{}, orgId string) {
	datasetName, _ := ds["datasetName"].(string)
	if datasetName == "" {
		fmt.Printf("No datasetName found, skipping data refresh\n")
		return
	}

	fmt.Printf("Refreshing data for dataset: %s\n", datasetName)

	// Build FilterParam from dataset params
	var filterParams []FilterParam
	paramFieldNames := []string{"params", "FilterParams", "filterParams", "Params"}
	for _, fieldName := range paramFieldNames {
		if dsParams, found := toSlice(ds[fieldName]); found {
			for _, param := range dsParams {
				if p, ok := toMap(param); ok {
					var paramName string
					var paramValue interface{}
					var paramDataType string

					// Get param name
					nameFields := []string{"parmasName", "ParmasName", "paramsName", "ParamsName"}
					for _, nf := range nameFields {
						if name, ok := p[nf].(string); ok && name != "" {
							paramName = name
							break
						}
					}

					// Get param value
					valueFields := []string{"Paramsvalue", "paramsvalue", "value", "Value"}
					for _, vf := range valueFields {
						if val, ok := p[vf]; ok && val != nil && val != "" {
							paramValue = val
							break
						}
					}

					// Get param data type
					typeFields := []string{"ParmsDataType", "parmsDataType", "dataType", "type"}
					for _, tf := range typeFields {
						if dt, ok := p[tf].(string); ok && dt != "" {
							paramDataType = dt
							break
						}
					}

					if paramName != "" && paramValue != nil {
						filterParams = append(filterParams, FilterParam{
							ParamsName:     paramName,
							Paramsvalue:    paramValue,
							ParamsDataType: paramDataType,
						})
						fmt.Printf("Added FilterParam: %s = %v (type: %s)\n", paramName, paramValue, paramDataType)
					}
				}
			}
			break
		}
	}

	if len(filterParams) == 0 {
		fmt.Printf("No filter params found, skipping data refresh\n")
		return
	}

	// Fetch dataset config from database
	db := database.GetConnection(orgId)
	var datasetConfig map[string]interface{}
	err := db.Collection("dataset_config").FindOne(
		context.Background(),
		bson.M{"dataSetName": datasetName},
	).Decode(&datasetConfig)

	if err != nil {
		fmt.Printf("Dataset config not found for '%s': %v\n", datasetName, err)
		return
	}

	// Get collection name and reference pipeline
	collectionName, _ := datasetConfig["dataSetBaseCollection"].(string)
	referencePipeline, _ := datasetConfig["Reference_pipeline"].(string)

	if collectionName == "" {
		fmt.Printf("No base collection found for dataset: %s\n", datasetName)
		return
	}

	// Build pipeline from reference pipeline with filter params
	var pipeline []bson.M
	if referencePipeline != "" {
		pipelineStr := createFilterParams(filterParams, referencePipeline)
		var pipes []primitive.M
		if err := json.Unmarshal([]byte(pipelineStr), &pipes); err != nil {
			fmt.Printf("Error parsing pipeline: %v\n", err)
			return
		}
		for _, p := range pipes {
			pipeline = append(pipeline, bson.M(p))
		}
		pipeline = UpdateDatatypes(pipeline)
	} else {
		// Fallback: use simple match if no reference pipeline
		fmt.Printf("No reference pipeline, using simple match\n")
		matchFilter := bson.M{}
		for _, fp := range filterParams {
			matchFilter[fp.ParamsName] = fp.Paramsvalue
		}
		pipeline = []bson.M{{"$match": matchFilter}}
	}

	// Execute aggregation
	cursor, err := db.Collection(collectionName).Aggregate(context.Background(), pipeline)
	if err != nil {
		fmt.Printf("Error executing aggregation: %v\n", err)
		return
	}
	defer cursor.Close(context.Background())

	var results []bson.M
	if err := cursor.All(context.Background(), &results); err != nil {
		return
	}

	// Update the data field in the dataset - convert to []interface{} for proper JSON serialization
	if len(results) > 0 {
		// Convert []bson.M to []interface{} with map[string]interface{} for proper JSON output
		convertedResults := make([]interface{}, len(results))
		for i, doc := range results {
			convertedResults[i] = convertBsonToMap(doc)
		}
		ds["data"] = convertedResults
	} else {
		fmt.Printf("No data found for the given params\n")
	}
}

// convertBsonToMap recursively converts bson types to standard Go maps for proper JSON serialization
func convertBsonToMap(v interface{}) interface{} {
	switch val := v.(type) {
	case bson.M:
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = convertBsonToMap(v)
		}
		return result
	case bson.D:
		result := make(map[string]interface{})
		for _, elem := range val {
			result[elem.Key] = convertBsonToMap(elem.Value)
		}
		return result
	case primitive.A:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = convertBsonToMap(item)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = convertBsonToMap(item)
		}
		return result
	case []bson.M:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = convertBsonToMap(item)
		}
		return result
	default:
		return val
	}
}

// extractDataFromPath extracts nested data using dot notation path
func extractDataFromPath(data interface{}, path string) interface{} {
	// Handle array of results - extract first item
	if arr, ok := toSlice(data); ok && len(arr) > 0 {
		data = arr[0]
	}

	// Navigate to nested path
	if path != "" {
		keys := splitPath(path)
		current := data
		for _, key := range keys {
			if m, ok := toMap(current); ok {
				if val, exists := m[key]; exists {
					current = val
				} else {
					return []interface{}{}
				}
			} else {
				return []interface{}{}
			}
		}
		return ensureArray(current)
	}
	return ensureArray(data)
}

// ensureArray ensures the data is always returned as an array
func ensureArray(data interface{}) interface{} {
	if arr, ok := toSlice(data); ok {
		return arr
	}
	return []interface{}{data}
}

// splitPath splits a dot-notation path into keys
func splitPath(path string) []string {
	if path == "" {
		return []string{}
	}
	keys := []string{}
	current := ""
	for _, char := range path {
		if char == '.' {
			if current != "" {
				keys = append(keys, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		keys = append(keys, current)
	}
	return keys
}
