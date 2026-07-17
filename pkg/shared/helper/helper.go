package helper

import (
	"context"
	// "crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	// "sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"go.mongodb.org/mongo-driver/mongo"

	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

// var (
// 	requestCache = make(map[string]interface{})
// 	cacheMutex   = sync.RWMutex{}
// 	cacheExpiry  = make(map[string]time.Time)
// )

var ctx = context.Background()

type Leave struct {
	ID       string    `bson:"_id"`
	Date     time.Time `bson:"date"`
	Name     string    `bson:"name"`
	Status   string    `bson:"status"`
	ParentID string    `bson:"parent_id"`

	Leave map[string]interface{} `bson:"leave"`
}

type HolidayResult struct {
	HolidayDates []time.Time `bson:"holidayDates" json:"holidayDates"`
}

func Page(s string) int64 {
	return Toint64(s)
}

func Limit(s string) int64 {
	if s == "" {
		s = utils.GetenvStr("DEFAULT_FETCH_ROWS")
	}
	return Toint64(s)
}
func Toint64(s string) int64 {
	if s == "" {
		return int64(0)
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
func ToFloat64(s interface{}) float64 {
	switch v := s.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case string:
		if v == "" {
			return 0
		}
		val, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0
		}
		return val
	default:
		return 0
	}
}

func BoolPtr(b bool) *bool {
	return &b
}

func ToInt32(s interface{}) int32 {
	switch v := s.(type) {
	case int:
		return int32(v)
	case int8:
		return int32(v)
	case int16:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	case float32:
		return int32(v)
	case float64:
		return int32(v)
	case string:
		if v == "" {
			return 0
		}
		val, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return 0
		}
		return int32(val)
	default:
		return 0
	}
}
func ToInt(s interface{}) int {
	switch v := s.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if v == "" {
			return 0
		}
		val, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return 0
		}
		return int(val)
	default:
		return 0
	}
}

func InterfaceToInt64(s interface{}) int64 {
	switch v := s.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int64:
		return v
	case int32:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		if v == "" {
			return 0
		}
		val, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return 0
		}
		return int64(val)
	default:
		return 0
	}
}
func ToString(input interface{}) string {
	return fmt.Sprintf("%v", input)
}
func FormatWithComma(value float64) string {
	return fmt.Sprintf("%,.2f", value)
}

// DocIdFilter generates a MongoDB filter for the given ID.
func DocIdFilter(id string) bson.M {
	// If the ID is empty, return an empty filter.
	if id == "" {
		return bson.M{}
	}

	id, err := url.QueryUnescape(id)
	if err != nil {
		// fmt.Println("Error decoding:", err)
	}
	return bson.M{"_id": id}

}

func ObjectIdToString(id interface{}) string {
	return id.(primitive.ObjectID).Hex()
}

// UpdateUserPasswordAndremoveTempData   --METHOD user password update with Hashing for user collection
func UpdateUserPasswordandremoveTempData(c *fiber.Ctx) error {
	// to value bind the from body
	org, exists := GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}
	var inputData map[string]interface{}
	err := c.BodyParser(&inputData)
	if err != nil {
		return shared.BadRequest("Error parsing request body: " + err.Error())
	}

	// accesKey from params
	access_key := c.Params("access_key")

	query := bson.M{"access_key": access_key}

	response, err := GetQueryResult(org.Id, "temporary_user", query, int64(0), int64(1), nil)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	// get he _id from response temporary_user collection
	ID, idExists := response[0]["_id"].(string)
	if !idExists {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Invalid response format"})
	}

	// to compare the password and confirm password is same only   Generate the Hased password
	if inputData["password"].(string) != inputData["confirm_password"].(string) {
		shared.BadRequest("Verify that the password and confirm password are the same.")
	}

	// if password marched to create the Hash Password
	passwordHash, _ := GeneratePasswordHash(inputData["password"].(string))
	inputData["pwd"] = passwordHash
	// remove the password and confirm password
	delete(inputData, "password")
	delete(inputData, "confirm_password")
	// to set the new hashing pasword
	update := bson.M{"$set": bson.M{"pwd": passwordHash}}
	filter := bson.M{"_id": ID}

	result, err := ExecuteFindAndModifyQuery(org.Id, "user", filter, update)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"message": "Failed to update document", "error": err.Error()})
	}

	_, err = ExecuteDeleteManyByIds(org.Id, "temporary_user", filter)
	if err != nil {
		return shared.BadRequest("Failed to Delete data into the database: " + err.Error())

	}

	return shared.SuccessResponse(c, result)
}

// RetrieveTemporaryUserDataByAccessKey  --METHOD Get the data without token
func RetrieveTemporaryUserDataByAccessKey(c *fiber.Ctx) error {
	org, exists := GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}
	filter :=
		bson.M{"access_key": c.Params("access_key")}

	response, err := GetQueryResult(org.Id, "temporary_user", filter, int64(0), int64(2), nil)

	if err != nil {
		return shared.BadRequest(err.Error())
	}

	return shared.SuccessResponse(c, response)

}

func handleUnmarshalError(inputJsonString string, newStructValue interface{}) map[string]string {
	validationErrMsg := make(map[string]string)

	err := json.Unmarshal([]byte(inputJsonString), newStructValue)
	if err != nil {
		if unmarshalErr, ok := err.(*json.UnmarshalTypeError); ok {
			expectedType := unmarshalErr.Type.String()
			dataType := strings.TrimPrefix(expectedType, "*")
			fieldName := unmarshalErr.Field
			if strings.Contains(dataType, "struct") {
				err := RemoveNestedArrayStructValue(fieldName, []byte(inputJsonString), newStructValue)
				if err != nil {
					return err
				}
			} else {
				validationErrMsg["field"] = fieldName
				validationErrMsg["Expected DataType"] = expectedType
				return validationErrMsg
			}

		}
	}

	return nil
}

func RemoveNestedArrayStructValue(fieldName string, inputJsonString []byte, newStructValue interface{}) map[string]string {
	validationErrMsg := make(map[string]string)

	var payload map[string]interface{}
	if err := json.Unmarshal(inputJsonString, &payload); err != nil {
		validationErrMsg["error"] = err.Error()
		return validationErrMsg
	}

	delete(payload, fieldName)

	cleanedPayload, err := json.Marshal(payload)
	if err != nil {
		validationErrMsg["error"] = err.Error()
		return validationErrMsg
	}

	if newStructValue != nil {
		if err := json.Unmarshal(cleanedPayload, newStructValue); err != nil {
			if unmarshalErr, ok := err.(*json.UnmarshalTypeError); ok {
				expectedType := unmarshalErr.Type.String()
				fieldNames := unmarshalErr.Field
				dataType := strings.TrimPrefix(expectedType, "*")

				if strings.Contains(dataType, "struct") {
					delete(payload, fieldNames)
					cleanedPayload, _ = json.Marshal(payload)
					return RemoveNestedArrayStructValue(fieldNames, cleanedPayload, newStructValue)
				} else {
					validationErrMsg["field"] = fieldNames
					validationErrMsg["Expected DataType"] = expectedType
					return validationErrMsg
				}
			}
		}
	}
	return validationErrMsg
}
func GenerateRandomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	rand.Seed(time.Now().UnixNano())

	result := make([]rune, n)
	for i := range result {
		result[i] = letters[rand.Intn(len(letters))]
	}
	return string(result)
}

func InsertValidateInDatamodel(collectionName, inputJsonString, orgId string) (map[string]interface{}, map[string]string) {
	var validationErrors = make(map[string]string)

	newStructValue, errorMessage := CreateInstanceForCollection(collectionName)
	if len(errorMessage) > 0 {
		return nil, errorMessage
	}

	json.Unmarshal([]byte(inputJsonString), newStructValue)
	// err := handleUnmarshalError(inputJsonString, newStructValue)
	// if len(err) > 0 {
	// 	return nil, err
	// }

	// loop through pointer to get the actual struct
	rv := reflect.ValueOf(newStructValue)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}

	var inputMap map[string]interface{}
	if err := json.Unmarshal([]byte(inputJsonString), &inputMap); err != nil {
		return nil, map[string]string{"error": "Invalid JSON data: " + err.Error()}
	}
	if collectionName != "job_work" {
		//Check the field any extra field is here
		if err := verifyInputStruct(rv, inputMap, validationErrors); err != nil {
			return nil, validationErrors
		}
	}

	validationErr := validate.Struct(rv)
	if validationErr != nil { // validation failed
		_, errorFields := GetSchemValidationError(validationErr)
		return nil, errorFields
	}

	return inputMap, nil

}

func UpdateValidateInDatamodel(collectionName string, inputJsonString, orgId string) (map[string]interface{}, map[string]string) {
	// newStructValue := DynamicallyBindStructOnDataModel(collectionName, orgId)
	newStructValue, errorMessage := CreateInstanceForCollection(collectionName)
	if len(errorMessage) > 0 {
		return nil, errorMessage
	}

	// err := handleUnmarshalError(inputJsonString, newStructValue)
	// if err != nil {
	// 	return nil, err
	// }
	fmt.Println("\033[33m" + inputJsonString + "\033[0m")
	fmt.Println("\033[38;5;200m", newStructValue, "\033[0m")
	json.Unmarshal([]byte(inputJsonString), newStructValue)
	// loop through pointer to get the actual struct
	rv := reflect.ValueOf(newStructValue)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}

	var inputMap map[string]interface{}
	if err := json.Unmarshal([]byte(inputJsonString), &inputMap); err != nil {
		return nil, map[string]string{"error": "Invalid JSON data: " + err.Error()}
	}

	matchedFields := FilterStructFieldsByJSON(rv, inputMap)
	newStructType := reflect.StructOf(matchedFields)

	// Create a new struct instance with the matched fields
	newStruct := reflect.New(newStructType).Interface()

	json.Unmarshal([]byte(inputJsonString), &newStruct)

	validationErrors := ValidateStruct(newStruct)
	if len(validationErrors) > 0 {
		return nil, validationErrors
	}

	var cleanedData map[string]interface{}
	inputByte, _ := json.Marshal(newStruct)
	json.Unmarshal(inputByte, &cleanedData)
	return cleanedData, nil
}

func mergeJSONDataIntoStruct(jsonData []byte, structInstance interface{}) error {
	// Unmarshal JSON data into a map
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return err
	}

	// Get struct value using reflection
	structValue := reflect.ValueOf(structInstance).Elem()

	// Iterate over the map and update struct fields accordingly
	for key, value := range data {
		field := structValue.FieldByName(key)
		if field.IsValid() && field.CanSet() {
			// Set the value of the struct field using reflection
			switch value := value.(type) {
			case float64:
				field.SetFloat(value)
			case string:
				field.SetString(value)
			// Handle other types as needed
			default:
				// Do nothing or return an error if needed
			}
		}
	}

	return nil
}

func MasterAggregationPipeline(request PaginationRequest, c *fiber.Ctx) []bson.M {
	Pipeline := []bson.M{}

	if len(request.Filter) > 0 {
		FilterConditions := BuildAggregationPipeline(request.Filter, "")
		Pipeline = append(Pipeline, FilterConditions)
	}

	if len(request.ArrayFilterColumns) > 0 {
		FilterConditions := BuildArrayAggregationPipeline(request.ArrayFilterColumns, request.ArrayFilterFieldName)
		Pipeline = append(Pipeline, FilterConditions)
	}

	// if len(request.Sort) > 0 {
	// 	sortConditions := buildSortConditions(request.Sort)
	// 	Pipeline = append(Pipeline, sortConditions)
	// }

	return Pipeline
}

func BuildArrayAggregationPipeline(filter []FieldValuePair, arrayFieldName string) primitive.M {
	var regex bson.D
	var allRegexContainer bson.A

	for _, obj := range filter {
		value := ToString(obj.FieldValue)
		regex = bson.D{
			{"$regexMatch",
				bson.D{
					{"input", bson.D{{"$toString", "$$item." + obj.FieldName}}},
					{"regex", primitive.Regex{Pattern: value}},
					{"options", "i"},
				},
			},
		}
		allRegexContainer = append(allRegexContainer, regex)
	}

	pipeline := primitive.M{
		"$set": bson.D{
			{"matchingItems",
				bson.D{
					{"$filter",
						bson.D{
							{"input", "$" + arrayFieldName},
							{"as", "item"},
							{"cond",
								bson.D{
									{"$and", allRegexContainer},
								},
							},
						},
					},
				},
			},
		},
	}
	return pipeline
}

// DatasetsConfig -- METHOD PURPOSE handle requests related to dataset configuration, including building aggregation pipelines
func DatasetsConfig(c *fiber.Ctx) error {
	///Get the orgId from Header
	org, exists := GetOrg(c)
	if !exists {

		return shared.BadRequest("Organization Id missing")
	}
	//TO Bind the Value from Body
	var inputData DataSetConfiguration
	if err := c.BodyParser(&inputData); err != nil {
		if cmdErr, ok := err.(mongo.CommandError); ok {
			return shared.BadRequest(cmdErr.Message)
		}

	}

	var Response fiber.Map
	// BuildPipeline -- Create a Filter Pipeline from Body Content
	Data, Response := BuildPipeline(org.Id, inputData)
	// Set the DatasetName to _id for unique
	Data.Id = inputData.DataSetName
	// Params options -- options is  insert the data to Db
	//if options empty is preview the data
	if c.Params("options") == "Insert" {
		var err error
		Response, err = InsertDataDb(org.Id, Data, "dataset_config")
		if err != nil {
			return shared.BadRequest(err.Error())

		}

	}
	return c.JSON(Response)

}

// BuildPipeline    -- METHOD PURPOSE  build a comprehensive MongoDB aggregation pipeline for querying and aggregating data
func BuildPipeline(orgId string, inputData DataSetConfiguration) (DataSetConfiguration, fiber.Map) {
	//append the pipelien from child Pipeline
	Pipeline := []bson.M{}
	//Every If condtion for if Data is here that time only that func work
	if len(inputData.DataSetBaseCollectionFilter) > 0 {
		Pipelines := BuildAggregationPipeline(inputData.DataSetBaseCollectionFilter, inputData.DataSetBaseCollection)
		Pipeline = append(Pipeline, Pipelines)

	}

	if len(inputData.DataSetJoinCollection) > 0 {
		lookupData := ExecuteLookupQueryData(inputData.DataSetJoinCollection, inputData.DataSetBaseCollection)
		Pipeline = append(Pipeline, lookupData...)
	}

	if len(inputData.CustomColumn) > 0 {
		createCustomColumns := CreateCusotmColumns(Pipeline, inputData.CustomColumn, inputData.DataSetBaseCollection)
		Pipeline = append(Pipeline, createCustomColumns...)
	}

	if len(inputData.Aggregation) > 0 {
		AggregationData := BuildDynamicAggregationPipelineFromSpecifications(inputData.Aggregation)
		Pipeline = append(Pipeline, AggregationData...)
	}
	if len(inputData.Filter) > 0 {
		filterPipelines := BuildAggregationPipeline(inputData.Filter, inputData.DataSetBaseCollection)
		Pipeline = append(Pipeline, filterPipelines)

	}

	// Projection
	if len(inputData.SelectedList) > 0 {
		selectedColumns := CreateSelectedColumn(inputData.SelectedList, inputData.DataSetBaseCollection)
		Pipeline = append(Pipeline, selectedColumns...)
	}
	// if len(inputData.UnsetFieldList) > 0 {
	// 	selectedColumns := CreateUnsetColumn(inputData.UnsetFieldList, inputData.DataSetBaseCollection)
	// 	Pipeline = append(Pipeline, selectedColumns...)
	// }

	// {
	// 	$unset: '_id'
	//   }
	// Grouping
	// if len(inputData.SelectedList) > 0 {
	// 	GroupedColumns := CreateGroupAggregationStage(inputData.SelectedList, inputData.DataSetBaseCollection)
	// 	Pipeline = append(Pipeline, GroupedColumns...)
	// }
	// filter pipeline convert the byte
	marshaldata, err := json.Marshal(Pipeline)
	if err != nil {
		return DataSetConfiguration{}, nil
	}
	// marshaldata  variable -- filter byte  convert the string
	pipelinestring := string(marshaldata)
	// set the inputData.Pipeline  -- store the data form converted string pipeine
	inputData.Pipeline = pipelinestring

	// Filter Params for to replace the string to convert to pipeline again
	if len(inputData.FilterParams) > 0 {

		inputData.Reference_pipeline = pipelinestring
		pipelinestring := createFilterParams(inputData.FilterParams, pipelinestring)
		// if filter params here that time to replace the old pipeline
		// Parse the provided string into a slice of BSON documents for the pipeline.
		pipelines := []primitive.M{}
		err = json.Unmarshal([]byte(pipelinestring), &pipelines)
		if err != nil {
			fmt.Println("Cannot Find the String")
		}

		//finalpipeline -- Build the Final append filter pipeline
		var finalpipeline []bson.M
		//UpdateDatatypes -- To build the Pipeline from pipeline variable
		Updatedpipeline := UpdateDatatypes(pipelines)

		finalpipeline = append(finalpipeline, Updatedpipeline...)
		Pipeline = finalpipeline
		a := pipelinestring

		inputData.Pipeline = a

	}
	// .
	//final pagination TO add the Filter
	PagiantionPipeline := PagiantionPipeline(inputData.Start, inputData.End)
	Pipeline = append(Pipeline, PagiantionPipeline)
	Response, err := GetAggregateQueryResult(orgId, inputData.DataSetBaseCollection, Pipeline)
	if err != nil {
		fmt.Println("Err",
			err.Error(),
		)
		return DataSetConfiguration{}, nil
	}

	// this PreviewResponse
	PreviewResponse := fiber.Map{
		"status": "success",
		"data":   Response,
	}

	return inputData, PreviewResponse

}

// Insert the Data and return map
func InsertDataDb(orgId string, inputData interface{}, collectionName string) (fiber.Map, error) {

	res, err := database.GetConnection(orgId).Collection(collectionName).InsertOne(ctx, inputData)
	if err != nil {
		return nil, err
	}
	InsertResponse := fiber.Map{
		"status":  "success",
		"message": "Data Added Successfully",
		"data": fiber.Map{
			"InsertedID": res.InsertedID,
		},
	}
	return InsertResponse, err
}
func DatasetsRetrieve(c *fiber.Ctx) error {
	// Get org
	org, exists := GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	// Params
	datasetname := c.Params("datasetname")
	filter := bson.M{"dataSetName": datasetname}
	response, err := GetQueryResult(org.Id, "dataset_config", filter, int64(0), int64(200), nil)
	// response, err := GetQueryResult(org.Id, "dataset_claude_respose", filter, int64(0), int64(200), nil)
	if err != nil {
		return shared.BadRequest("Invalid Params value")
	}
	if len(response) == 0 {
		return shared.BadRequest("Invalid Params value")
	}
	ResponseData := response[0]

	// Collection name
	CollectionName := ResponseData["dataSetBaseCollection"].(string)

	// Parse request body
	var requestBody PaginationRequest
	if err := c.BodyParser(&requestBody); err != nil {
		return shared.BadRequest(err.Error())
	}

	// Normalize start/end (support both StartRow/EndRow and Start/End)
	start := requestBody.StartRow
	end := requestBody.EndRow
	if start == 0 && end == 0 {
		start = requestBody.Start
		end = requestBody.End
	}
	limit := 0
	if end > start {
		limit = end - start
	}

	// Helper: build base pipeline from dataset config or Reference_pipeline
	buildBasePipeline := func() ([]bson.M, error) {
		basePipeline := []bson.M{}
		if requestBody.FilterParam == nil || len(requestBody.FilterParam) == 0 {
			// Use configured pipeline
			if ResponseData["pipeline"] != nil && ResponseData["pipeline"] != "" {
				pipelineStr := ResponseData["pipeline"].(string)
				var pipelines []bson.M
				if err := json.Unmarshal([]byte(pipelineStr), &pipelines); err != nil {
					return nil, err
				}
				basePipeline = UpdateDatatypes(pipelines)
			}
		} else {
			// Build from reference pipeline + FilterParam
			referencePipeline := ResponseData["Reference_pipeline"].(string)
			pipelinestring := createFilterParams(requestBody.FilterParam, referencePipeline)
			var pipes []primitive.M
			if err := json.Unmarshal([]byte(pipelinestring), &pipes); err != nil {
				return nil, err
			}
			// convert primitive.M -> bson.M for UpdateDatatypes if necessary
			converted := make([]bson.M, 0, len(pipes))
			for _, p := range pipes {
				converted = append(converted, bson.M(p))
			}
			converted = UpdateDatatypes(converted)
			basePipeline = append(basePipeline, converted...)
		}
		return basePipeline, nil
	}

	// Build master aggregation filter pipeline (from request)
	filterpipeline := MasterAggregationPipeline(requestBody, c)

	// If grouping requested, handle the grouping paths
	if requestBody.IsGrouping && len(requestBody.RowGroupCols) > 0 {
		// groupLevel = number of keys already applied (which indicates which level AG Grid is asking for)
		groupLevel := len(requestBody.GroupKeys) // 0 -> root groups; 1 -> children of first group; etc.

		// If groupLevel < len(RowGroupCols) -> we must return group nodes for next level
		if groupLevel < len(requestBody.RowGroupCols) {
			// next grouping column
			nextGroupCol := requestBody.RowGroupCols[groupLevel].FieldName // e.g. 'frequency'
			projectField := requestBody.RowGroupCols[groupLevel].FieldName // same as FieldName in our shape

			// Build base pipeline
			basePipeline, err := buildBasePipeline()
			if err != nil {
				return shared.InternalServerError(err.Error())
			}

			// Add matches for parent group keys (case-insensitive) if any
			for i, key := range requestBody.GroupKeys {
				if i < len(requestBody.RowGroupCols) {
					parentField := requestBody.RowGroupCols[i].FieldName
					basePipeline = append(basePipeline, bson.M{
						"$match": bson.M{
							parentField: bson.M{
								"$regex":   "^" + regexp.QuoteMeta(key) + "$",
								"$options": "i",
							},
						},
					})
				}
			}

			// Append filter pipeline (your existing filtering)
			basePipeline = append(basePipeline, filterpipeline...)

			// Group stage: group by nextGroupCol, count children
			groupStage := bson.M{
				"$group": bson.M{
					"_id":   "$" + nextGroupCol,
					"count": bson.M{"$sum": 1},
				},
			}
			projectStage := bson.M{
				"$project": bson.M{
					"key":        "$_id",
					"childCount": "$count",
					"_id":        0,
				},
			}
			sortStage := bson.M{"$sort": bson.M{"key": 1}}

			groupPipeline := append(basePipeline, groupStage, projectStage, sortStage)

			// Execute aggregation
			groupedData, err := GetAggregateQueryResult(org.Id, CollectionName, groupPipeline)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}

			// total groups
			totalGroups := len(groupedData)

			// apply pagination on groups if requested via start/limit
			pagStart := start
			if pagStart < 0 {
				pagStart = 0
			}
			if pagStart > totalGroups {
				pagStart = totalGroups
			}
			pagEnd := totalGroups
			if limit > 0 && pagStart+limit < totalGroups {
				pagEnd = pagStart + limit
			}
			paginatedGroups := groupedData[pagStart:pagEnd]

			// prepare rows in ag-grid group node shape
			var rows []bson.M
			for _, g := range paginatedGroups {
				keyVal := g["key"]
				keyStr := fmt.Sprint(keyVal)
				// include the grouped field name in the row so ag-grid shows the group value in the column
				rows = append(rows, bson.M{
					projectField: keyVal,
					"group":      true,
					"childCount": g["childCount"],
					"_id":        fmt.Sprintf("group_%d_%s", groupLevel, keyStr),
				})
			}

			// response format consistent with other handlers in your app
			resp := []bson.M{
				{
					"response":   rows,
					"pagination": []bson.M{{"totalDocs": totalGroups}},
				},
			}
			return shared.SuccessResponse(c, resp)
		}

		// If groupLevel == len(RowGroupCols) -> leaf data (actual documents for the specific group key chain)
		if groupLevel == len(requestBody.RowGroupCols) {
			// Build base pipeline
			basePipeline, err := buildBasePipeline()
			if err != nil {
				return shared.InternalServerError(err.Error())
			}

			// Add matches for each group key in order to filter the documents down to that group
			for i, key := range requestBody.GroupKeys {
				if i < len(requestBody.RowGroupCols) {
					groupField := requestBody.RowGroupCols[i].FieldName
					basePipeline = append(basePipeline, bson.M{
						"$match": bson.M{
							groupField: bson.M{
								"$regex":   "^" + regexp.QuoteMeta(key) + "$",
								"$options": "i",
							},
						},
					})
				}
			}

			// Add the standard filter pipeline
			basePipeline = append(basePipeline, filterpipeline...)

			// Add sort if provided in requestBody.Sort
			if len(requestBody.Sort) > 0 {
				sortConditions := BuildSortConditions(requestBody.Sort)
				basePipeline = append(basePipeline, sortConditions)
			}

			// Pagination using start/limit
			if start > 0 {
				basePipeline = append(basePipeline, bson.M{"$skip": start})
			}
			if limit > 0 {
				basePipeline = append(basePipeline, bson.M{"$limit": limit})
			}

			leafData, err := GetAggregateQueryResult(org.Id, CollectionName, basePipeline)
			if err != nil {
				return shared.InternalServerError(err.Error())
			}

			resp := []bson.M{
				{
					"response":   leafData,
					"pagination": []bson.M{{"totalDocs": len(leafData)}},
				},
			}
			return shared.SuccessResponse(c, resp)
		}
	}

	// If not grouping or grouping not requested, return normal flat rows
	// Build base pipeline (from config or reference)
	var finalpipeline []bson.M
	if requestBody.FilterParam == nil || len(requestBody.FilterParam) == 0 {
		if ResponseData["pipeline"] != nil && ResponseData["pipeline"] != "" {
			pipeline := ResponseData["pipeline"].(string)
			var pipelines []bson.M
			if err := json.Unmarshal([]byte(pipeline), &pipelines); err != nil {
				return shared.InternalServerError(err.Error())
			}
			Updatedpipeline := UpdateDatatypes(pipelines)
			finalpipeline = append(finalpipeline, Updatedpipeline...)
		}
	} else {
		reference_pipeline := ResponseData["Reference_pipeline"].(string)
		pipelinestring := createFilterParams(requestBody.FilterParam, reference_pipeline)
		var pipes []primitive.M
		if err := json.Unmarshal([]byte(pipelinestring), &pipes); err != nil {
			return shared.InternalServerError(err.Error())
		}
		converted := make([]bson.M, 0, len(pipes))
		for _, p := range pipes {
			converted = append(converted, bson.M(p))
		}
		converted = UpdateDatatypes(converted)
		finalpipeline = append(finalpipeline, converted...)
	}

	// Combine with MasterAggregationPipeline filters
	filterpipeline = MasterAggregationPipeline(requestBody, c)
	if !requestBody.FilterAppendFirst {
		finalpipeline = append(finalpipeline, filterpipeline...)
	} else {
		// if FilterAppendFirst is true, apply filter first then base pipeline
		temp := append(filterpipeline, finalpipeline...)
		finalpipeline = temp
	}

	// Multi-field search (already in your code)
	if len(requestBody.MultiFieldSearchFilter) > 0 {
		var orConditions []bson.M
		for _, filter := range requestBody.MultiFieldSearchFilter {
			if filter.Operator == "CONTAINS" {
				prefix, ok := filter.Value.(string)
				if !ok {
					continue
				}
				orConditions = append(orConditions, bson.M{
					filter.Column: bson.M{
						"$regex":   "^" + regexp.QuoteMeta(prefix),
						"$options": "i",
					},
				})
			}
		}
		if len(orConditions) > 0 {
			finalpipeline = append(finalpipeline, bson.M{
				"$match": bson.M{"$or": orConditions},
			})
		}
	}

	// Sorting
	if len(requestBody.Sort) > 0 {
		sortConditions := BuildSortConditions(requestBody.Sort)
		finalpipeline = append(finalpipeline, sortConditions)
	}

	// Pagination
	PagiantionPipeline := PagiantionPipeline(start, end)
	finalpipeline = append(finalpipeline, PagiantionPipeline)

	// Execute the final (flat) aggregation
	Response, err := GetAggregateQueryResult(org.Id, CollectionName, finalpipeline)
	if err != nil {
		return shared.InternalServerError(err.Error())
	}

	return shared.SuccessResponse(c, Response)
}

// DatasetsRetrieve  -- METHOD PURPOSE Get the Filter pipeline in Db to show the data
// func DatasetsRetrieve(c *fiber.Ctx) error {
// 	///Get the orgId from Header
// 	org, exists := GetOrg(c)
// 	if !exists {

// 		return shared.BadRequest("Organization Id missing")
// 	}
// 	//userToken := utils.GetUserTokenValue(c)
// 	//Params
// 	datasetname := c.Params("datasetname")

// 	filter := bson.M{"dataSetName": datasetname}
// 	response, err := GetQueryResult(org.Id, "dataset_config", filter, int64(0), int64(200), nil)
// 	if err != nil {
// 		return shared.BadRequest("Invalid  Params value")
// 	}
// 	if len(response) == 0 {

// 		return shared.BadRequest("Invalid  Params value")

// 	}
// 	ResponseData := response[0]
// 	//Get the Collection Name in Database
// 	CollectionName := ResponseData["dataSetBaseCollection"].(string)
// 	//Body Filter storing to struct
// 	var finalpipeline []bson.M
// 	var requestBody PaginationRequest
// 	if err := c.BodyParser(&requestBody); err != nil {
// 		return shared.BadRequest(err.Error())
// 	}
// 	// Check if grouping is requested and we're not fetching children of an expanded group
// 	// fmt.Printf("[DEBUG] IsGrouping: %v, RowGroupCols: %d, GroupKeys: %d\n", requestBody.IsGrouping, len(requestBody.RowGroupCols), len(requestBody.GroupKeys))
// 	// if requestBody.IsGrouping && len(requestBody.RowGroupCols) > 0 && len(requestBody.RowGroupCols) > len(requestBody.GroupKeys) {
// 	// 	fmt.Printf("[DEBUG] Taking GROUP path\n")
// 	// 	// Create cache key from request body JSON including pagination
// 	// 	requestJSON, _ := json.Marshal(requestBody)
// 	// 	requestHash := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s-%s-%d-%d", CollectionName, string(requestJSON), requestBody.StartRow, requestBody.EndRow))))
// 	// 	fmt.Printf("[CACHE] Hash: %s (StartRow: %d, EndRow: %d)\n", requestHash, requestBody.StartRow, requestBody.EndRow)

// 	// 	// Check cache
// 	// 	cacheMutex.RLock()
// 	// 	if cachedResponse, exists := requestCache[requestHash]; exists {
// 	// 		if expiry, ok := cacheExpiry[requestHash]; ok && time.Now().Before(expiry) {
// 	// 			cacheMutex.RUnlock()
// 	// 			fmt.Printf("[CACHE] HIT - returning cached response\n")
// 	// 			return c.JSON(cachedResponse)
// 	// 		}
// 	// 	}
// 	// 	cacheMutex.RUnlock()
// 	// 	fmt.Printf("[CACHE] MISS - processing request\n")
// 	// 	groupCol := requestBody.RowGroupCols[len(requestBody.GroupKeys)]
// 	// 	groupField := groupCol.Field // e.g. "product" - used in $group as "$product"
// 	// 	projectField := groupCol.Id  // e.g. "product" or whatever your frontend expects as the field name

// 	// 	// base pipeline from dataset config (unchanged)
// 	// 	basePipeline := []bson.M{}
// 	// 	if ResponseData["pipeline"] != nil {
// 	// 		pipelineStr := ResponseData["pipeline"].(string)
// 	// 		var pipelines []bson.M
// 	// 		if err := json.Unmarshal([]byte(pipelineStr), &pipelines); err == nil {
// 	// 			basePipeline = UpdateDatatypes(pipelines)
// 	// 		}
// 	// 	}

// 	// 	// Add WHERE conditions for existing group keys (case-insensitive)
// 	// 	for i, key := range requestBody.GroupKeys {
// 	// 		if i < len(requestBody.RowGroupCols) {
// 	// 			groupColId := requestBody.RowGroupCols[i].Id
// 	// 			basePipeline = append(basePipeline, bson.M{
// 	// 				"$match": bson.M{groupColId: bson.M{
// 	// 					"$regex": "^" + regexp.QuoteMeta(key) + "$",
// 	// 					"$options": "i",
// 	// 				}},
// 	// 			})
// 	// 		}
// 	// 	}

// 	// 	// Add request filters (your existing filter pipeline)
// 	// 	filterpipeline := MasterAggregationPipeline(requestBody, c)
// 	// 	basePipeline = append(basePipeline, filterpipeline...)

// 	// 	// Group stage: group by the actual field name
// 	// 	basePipeline = append(basePipeline,
// 	// 		bson.M{"$group": bson.M{
// 	// 			"_id":   "$" + groupField,
// 	// 			"count": bson.M{"$sum": 1},
// 	// 		}},
// 	// 		// Project to a consistent shape: key + count
// 	// 		bson.M{"$project": bson.M{
// 	// 			"key":        "$_id",
// 	// 			"childCount": "$count",
// 	// 			"_id":        0,
// 	// 		}},
// 	// 	)

// 	// 	// Add sorting
// 	// 	basePipeline = append(basePipeline, bson.M{"$sort": bson.M{"key": 1}})

// 	// 	groupedData, err := GetAggregateQueryResult(org.Id, CollectionName, basePipeline)
// 	// 	if err != nil {
// 	// 		return shared.InternalServerError(err.Error())
// 	// 	}

// 	// 	// Calculate total count from grouped data
// 	// 	totalCount := 0
// 	// 	for _, group := range groupedData {
// 	// 		if childCount, ok := group["childCount"]; ok {
// 	// 			switch v := childCount.(type) {
// 	// 			case int32:
// 	// 				totalCount += int(v)
// 	// 			case int64:
// 	// 				totalCount += int(v)
// 	// 			case int:
// 	// 				totalCount += v
// 	// 			}
// 	// 		}
// 	// 	}

// 	// 	// Apply pagination to grouped data
// 	// 	start := requestBody.StartRow
// 	// 	end := len(groupedData)
// 	// 	if requestBody.EndRow > start && requestBody.EndRow < end {
// 	// 		end = requestBody.EndRow
// 	// 	}
// 	// 	if start > len(groupedData) {
// 	// 		start = len(groupedData)
// 	// 	}
// 	// 	paginatedGroups := groupedData[start:end]

// 	// 	// Map groupedData to ag-Grid friendly rows
// 	// 	var rows []bson.M
// 	// 	for _, g := range paginatedGroups {
// 	// 		keyVal := g["key"]
// 	// 		keyStr := fmt.Sprint(keyVal)
// 	// 		rows = append(rows, bson.M{
// 	// 			projectField: keyVal,
// 	// 			"group":      true,
// 	// 			"expanded":   true,
// 	// 			"childCount": g["childCount"],
// 	// 			"_id":        fmt.Sprintf("group_%s_%s", projectField, keyStr),
// 	// 		})
// 	// 	}

// 	// 	response := []bson.M{
// 	// 		{
// 	// 			"response":   rows,
// 	// 			"pagination": []bson.M{{"totalDocs": len(rows)}},
// 	// 		},
// 	// 	}

// 	// 	// Cache response for 2 seconds
// 	// 	cacheMutex.Lock()
// 	// 	requestCache[requestHash] = response
// 	// 	cacheExpiry[requestHash] = time.Now().Add(2 * time.Second)
// 	// 	cacheMutex.Unlock()
// 	// 	fmt.Printf("[CACHE] STORED response for hash: %s\n", requestHash)

// 	// 	return shared.SuccessResponse(c, response)
// 	// } else if requestBody.IsGrouping && len(requestBody.RowGroupCols) >= 2 && len(requestBody.RowGroupCols) == len(requestBody.GroupKeys) {
// 	// 	fmt.Printf("[DEBUG] Taking LEAF path (multi-level)\n")
// 	// 	// Return leaf data - actual records for the specific group combination
// 	// 	basePipeline := []bson.M{}
// 	// 	if ResponseData["pipeline"] != nil {
// 	// 		pipelineStr := ResponseData["pipeline"].(string)
// 	// 		var pipelines []bson.M
// 	// 		if err := json.Unmarshal([]byte(pipelineStr), &pipelines); err == nil {
// 	// 			basePipeline = UpdateDatatypes(pipelines)
// 	// 		}
// 	// 	}

// 	// 	// Add filters for each group key
// 	// 	for i, key := range requestBody.GroupKeys {
// 	// 		if i < len(requestBody.RowGroupCols) {
// 	// 			groupColId := requestBody.RowGroupCols[i].Id
// 	// 			basePipeline = append(basePipeline, bson.M{
// 	// 				"$match": bson.M{groupColId: bson.M{
// 	// 					"$regex": "^" + regexp.QuoteMeta(key) + "$",
// 	// 					"$options": "i",
// 	// 				}},
// 	// 			})
// 	// 		}
// 	// 	}

// 	// 	// Add request filters
// 	// 	filterpipeline := MasterAggregationPipeline(requestBody, c)
// 	// 	basePipeline = append(basePipeline, filterpipeline...)

// 	// 	// Add pagination using Start/End
// 	// 	if requestBody.Start > 0 {
// 	// 		basePipeline = append(basePipeline, bson.M{"$skip": requestBody.Start})
// 	// 	}
// 	// 	if requestBody.End > requestBody.Start {
// 	// 		basePipeline = append(basePipeline, bson.M{"$limit": requestBody.End - requestBody.Start})
// 	// 	}

// 	// 	fmt.Printf("[LEAF] Pipeline: %+v\n", basePipeline)
// 	// 	leafData, err := GetAggregateQueryResult(org.Id, CollectionName, basePipeline)
// 	// 	if err != nil {
// 	// 		fmt.Printf("[LEAF] Error: %v\n", err)
// 	// 		return shared.InternalServerError(err.Error())
// 	// 	}

// 	// 	fmt.Printf("[LEAF] Found %d records\n", len(leafData))
// 	// 	// Format response for ag-Grid
// 	// 	response := []bson.M{
// 	// 		{
// 	// 			"response":   leafData,
// 	// 			"pagination": []bson.M{{"totalDocs": len(leafData)}},
// 	// 		},
// 	// 	}
// 	// 	return shared.SuccessResponse(c, response)
// 	// } else if requestBody.IsGrouping && len(requestBody.RowGroupCols) == 1 && len(requestBody.GroupKeys) == 1 {
// 	// 	fmt.Printf("[DEBUG] Taking SINGLE path\n")
// 	// 	// Single-level grouping with group key - return individual records for that group
// 	// 	basePipeline := []bson.M{}
// 	// 	if ResponseData["pipeline"] != nil {
// 	// 		pipelineStr := ResponseData["pipeline"].(string)
// 	// 		var pipelines []bson.M
// 	// 		if err := json.Unmarshal([]byte(pipelineStr), &pipelines); err == nil {
// 	// 			basePipeline = UpdateDatatypes(pipelines)
// 	// 		}
// 	// 	}

// 	// 	// Add filter for the group key
// 	// 	groupColId := requestBody.RowGroupCols[0].Id
// 	// 	key := requestBody.GroupKeys[0]
// 	// 	basePipeline = append(basePipeline, bson.M{
// 	// 		"$match": bson.M{groupColId: bson.M{
// 	// 			"$regex": "^" + regexp.QuoteMeta(key) + "$",
// 	// 			"$options": "i",
// 	// 		}},
// 	// 	})

// 	// 	// Add request filters
// 	// 	filterpipeline := MasterAggregationPipeline(requestBody, c)
// 	// 	basePipeline = append(basePipeline, filterpipeline...)

// 	// 	// Add pagination
// 	// 	if requestBody.StartRow > 0 {
// 	// 		basePipeline = append(basePipeline, bson.M{"$skip": requestBody.StartRow})
// 	// 	}
// 	// 	if requestBody.EndRow > requestBody.StartRow {
// 	// 		basePipeline = append(basePipeline, bson.M{"$limit": requestBody.EndRow - requestBody.StartRow})
// 	// 	}

// 	// 	leafData, err := GetAggregateQueryResult(org.Id, CollectionName, basePipeline)
// 	// 	if err != nil {
// 	// 		return shared.InternalServerError(err.Error())
// 	// 	}

// 	// 	fmt.Printf("[SINGLE] Found %d records\n", len(leafData))
// 	// 	// Format response for ag-Grid
// 	// 	response := []bson.M{
// 	// 		{
// 	// 			"response":   leafData,
// 	// 			"pagination": []bson.M{{"totalDocs": len(leafData)}},
// 	// 		},
// 	// 	}
// 	// 	return shared.SuccessResponse(c, response)
// 	// }

// 	if requestBody.FilterParam == nil && len(requestBody.FilterParam) == 0 {
// 		// Assuming ResponseData["pipeline"] contains the JSON-like string
// 		pipeline := ResponseData["pipeline"].(string)

// 		// Convert the modified JSON-like string into a byte slice
// 		data := []byte(pipeline)

// 		// Define a slice to hold the BSON documents
// 		var pipelines []bson.M

// 		// Unmarshal the modified JSON-like string into BSON documents
// 		if err := json.Unmarshal(data, &pipelines); err != nil {
// 			return shared.InternalServerError(err.Error())
// 		}
// 		// UpdateDatatypes -- To build the Pipeline from pipeline variable
// 		Updatedpipeline := UpdateDatatypes(pipelines)
// 		finalpipeline = append(finalpipeline, Updatedpipeline...)

// 	} else {

// 		reference_pipeline := ResponseData["Reference_pipeline"].(string)
// 		fmt.Printf("\033[1;31m%+v\033[0m\n", reference_pipeline)
// 		var pipelinestring string
// 		fmt.Printf("\033[1;31m%+v\033[0m\n", requestBody.FilterParam)

// 		pipelinestring = createFilterParams(requestBody.FilterParam, reference_pipeline)
// 		fmt.Printf("\033[1;33m%+v\033[0m\n", pipelinestring)
// 		pipelines := []primitive.M{}
// 		err = json.Unmarshal([]byte(pipelinestring), &pipelines)
// 		if err != nil {
// 			return shared.InternalServerError(err.Error())
// 		}
// 		pipelines = UpdateDatatypes(pipelines)
// 		finalpipeline = append(finalpipeline, pipelines...)

// 	}

// 	//Body Filter Pipeline making
// 	filterpipeline := MasterAggregationPipeline(requestBody, c)
// 	// if userToken.UserRole != "SA" {
// 	// 	OrgPipeline := GenerateOrgIdFilter(userToken.OrgId)

// 	// 	finalpipeline = append(finalpipeline, OrgPipeline)
// 	// 	//fmt.Println(OrgPipeline)
// 	// }
// 	//To combine the pipeline filter and basefilter
// 	if !requestBody.FilterAppendFirst {
// 		finalpipeline = append(finalpipeline, filterpipeline...)
// 	} else {
// 		filterpipeline = append(filterpipeline, finalpipeline...)
// 		finalpipeline = filterpipeline
// 	}

// 	if len(requestBody.MultiFieldSearchFilter) > 0 {
// 		var orConditions []bson.M

// 		for _, filter := range requestBody.MultiFieldSearchFilter {
// 			// if filter.Operator == "CONTAINS" {
// 			// 	orConditions = append(orConditions, bson.M{
// 			// 		filter.Column: bson.M{
// 			// 			"$regex":   filter.Value.(string),
// 			// 			"$options": "i",
// 			// 		},
// 			// 	})
// 			// }
// 			if filter.Operator == "CONTAINS" {
// 				prefix := filter.Value.(string)
// 				orConditions = append(orConditions, bson.M{
// 					filter.Column: bson.M{
// 						"$regex":   "^" + regexp.QuoteMeta(prefix),
// 						"$options": "i", // case-insensitive
// 					},
// 				})
// 			}

// 		}

// 		if len(orConditions) > 0 {

// 			finalpipeline = append(finalpipeline, bson.M{
// 				"$match": bson.M{"$or": orConditions},
// 			})

// 		}
// 	}

// 	if len(requestBody.Sort) > 0 {
// 		sortConditions := BuildSortConditions(requestBody.Sort)
// 		finalpipeline = append(finalpipeline, sortConditions)
// 	}

// 	//final pagination TO add the Filter
// 	PagiantionPipeline := PagiantionPipeline(requestBody.Start, requestBody.End)
// 	finalpipeline = append(finalpipeline, PagiantionPipeline)
// 	// fmt.Println(finalpipeline)
// 	// To Get the Data from Db
// 	// fmt.Printf("\033[1;36m%+v\033[0m\n", CollectionName)

// 	Response, err := GetAggregateQueryResult(org.Id, CollectionName, finalpipeline)
// 	if err != nil {
// 		shared.InternalServerError(err.Error())
// 	}

// 	return shared.SuccessResponse(c, Response)
// }

// // UpdateDatatypes    --METHOD  Get the match object and to build the mongo Query
// func UpdateDatatypes(pipeline []bson.M) []bson.M {
// 	output := []bson.M{}
// 	for _, stage := range pipeline {
// 		if matchStage, ok := stage["$match"]; ok {
// 			// To Pass the interface{} to $match data for datatype convertion
// 			matchedPipeline := createQueryPipeline(matchStage)
// 			output = append(output, bson.M{"$match": matchedPipeline})
// 		} else {
// 			output = append(output, stage)
// 		}
// 	}

//			return output
//		}
//	 ? Adding the Date Type for Subobject also with recurvise calling
func UpdateDatatypes(pipeline []bson.M) []bson.M {
	output := []bson.M{}

	for _, stage := range pipeline {
		// Handle $match
		if stage["$match"] != nil {
			matchStage := stage["$match"]
			converted := createQueryPipeline(matchStage)
			output = append(output, bson.M{"$match": converted})
			continue
		}

		// Handle $lookup
		if stage["$lookup"] != nil {
			lookupRaw := stage["$lookup"]
			lookupStage, ok := lookupRaw.(map[string]interface{})
			if !ok {
				output = append(output, stage)
				continue
			}

			if innerPipeline, ok := lookupStage["pipeline"].([]interface{}); ok {
				convertedPipeline := []interface{}{}
				for _, innerStage := range innerPipeline {
					if innerMap, ok := innerStage.(map[string]interface{}); ok {
						convertedStages := UpdateDatatypes([]bson.M{innerMap})
						for _, converted := range convertedStages {
							convertedPipeline = append(convertedPipeline, converted)
						}
					} else {
						convertedPipeline = append(convertedPipeline, innerStage)
					}
				}
				lookupStage["pipeline"] = convertedPipeline
				stage["$lookup"] = lookupStage
			}

			output = append(output, stage)
			continue
		}

		// Handle $facet
		if stage["$facet"] != nil {
			facetRaw := stage["$facet"]
			facetMap, ok := facetRaw.(map[string]interface{})
			if !ok {
				output = append(output, stage)
				continue
			}

			convertedFacet := bson.M{}
			for key, val := range facetMap {
				if facetStages, ok := val.([]interface{}); ok {
					subPipeline := []bson.M{}
					for _, item := range facetStages {
						// Convert each stage to bson.M if possible
						if stageMap, ok := item.(map[string]interface{}); ok {
							subPipeline = append(subPipeline, bson.M(stageMap))
						}
					}
					convertedFacet[key] = UpdateDatatypes(subPipeline)
				} else {
					convertedFacet[key] = val
				}
			}

			output = append(output, bson.M{"$facet": convertedFacet})
			continue
		}

		// Default: append stage as-is
		output = append(output, stage)
	}

	return output
}

// createQueryPipeline -- METHOD To change the value Datatype and return the pipeline format
// Recusively call the  Method for Datatype converntiuon
func createQueryPipeline(data interface{}) interface{} {
	// Check the Every DataType to incoming
	switch dataType := data.(type) {
	case map[string][]interface{}:
		var outputArray []interface{}
		for _, value := range dataType {
			for _, item := range value {
				outputArray = append(outputArray, createQueryPipeline(item))
			}
		}
		return outputArray
	case map[string]interface{}:
		valueMap := dataType
		for k := range valueMap {
			valueMap[k] = createQueryPipeline(valueMap[k])
		}
		return valueMap
	case []interface{}:
		var outputArray []interface{}
		for _, i := range dataType {
			outputArray = append(outputArray, createQueryPipeline(i))
		}
		return outputArray
	default:
		if data != nil {
			return ConvertToDataType(data, reflect.TypeOf(data).String())
		}
		return data

	}

}

// UpdateDataset  --METHOD Update the Dataset_config collection to store the data with pipeline
func UpdateDataset(c *fiber.Ctx) error {
	org, exists := GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}
	datasetname := c.Params("datasetname")
	//Params
	//Update body to bind the  DataSetConfiguration
	var inputData DataSetConfiguration
	if err := c.BodyParser(&inputData); err != nil {
		return shared.BadRequest("Invalid Body Content")
	}
	// Global Variable set the For response
	var Response fiber.Map
	//Build the Pipeline
	Data, Response := BuildPipeline(org.Id, inputData)
	var err error
	Response, err = UpdateDataToDb(org.Id, bson.M{"_id": datasetname}, Data, "dataset_config")
	if err != nil {
		return shared.BadRequest("Failed to insert data into the database")

	}

	return shared.SuccessResponse(c, Response)
}

func HandleIDGeneration(inputData bson.M, orgID string, collectionName string) {
	if inputData["_id"] != nil {
		if collectionName != "lots" {

			result, err := HandleSequenceOrder(inputData["_id"].(string), orgID, collectionName)
			if err == nil {
				inputData["_id"] = result
			}
		} else if collectionName == "lots" {

			result, err := GetNextSeqNumber(inputData["_id"].(string)+inputData["factory_id"].(string), orgID)
			if err != nil {
				fmt.Println(err.Error())
			}

			inputData["_id"] = ToString(result)

		}
	} else {
		// fmt.Println("sdagsd")

		inputData["_id"] = Generateuniquekey()

	}
}

func InsertOnDb(orgId string, collectionName string, inputData map[string]interface{}) (*mongo.InsertOneResult, error) {
	res, err := database.GetConnection(orgId).Collection(collectionName).InsertOne(ctx, inputData)

	return res, err
}

func GenerateErrorMessage(err map[string]string) string {
	errMsgMap := make(map[string][]interface{})
	for key, value := range err {
		errMsgMap[value] = append(errMsgMap[value], key)
	}

	var errMsg [][]interface{}
	for key, value := range errMsgMap {
		errMsg = append(errMsg, []interface{}{value, key})
	}
	var formattedErrMsg string
	for _, group := range errMsg {
		keys := group[0].([]interface{})
		errorMsg := group[1].(string)
		formattedKeys := fmt.Sprintf("[%s]", strings.Join(keysToStrings(keys), ","))
		formattedErrMsg += fmt.Sprintf("%s: %s", formattedKeys, errorMsg)

	}
	return formattedErrMsg
}

func keysToStrings(keys []interface{}) []string {
	var stringKeys []string
	for _, key := range keys {
		stringKeys = append(stringKeys, fmt.Sprintf("%v", key))
	}
	return stringKeys
}

func appendQueryToPipeline(pipeline *[]bson.M, query interface{}) error {
	switch q := query.(type) {
	case bson.M:
		*pipeline = append(*pipeline, q)
	case []bson.M:
		*pipeline = append(*pipeline, q...)
	case primitive.A:
		bsonArray, err := primitiveAtoBsonMArray(q)
		if err != nil {
			return fmt.Errorf("cannot convert primitive.A to []bson.M: %v", err)
		}
		*pipeline = append(*pipeline, bsonArray...)
	case []primitive.A:
		for _, pa := range q {
			bsonArray, err := primitiveAtoBsonMArray(pa)
			if err != nil {
				return fmt.Errorf("cannot convert primitive.A to []bson.M: %v", err)
			}
			*pipeline = append(*pipeline, bsonArray...)
		}
	default:
		return fmt.Errorf("query is not a valid BSON document or array of BSON documents: type is %T", query)
	}
	return nil
}

func primitiveAtoBsonMArray(pa primitive.A) ([]bson.M, error) {
	var bsonArray []bson.M
	for _, elem := range pa {
		if bm, ok := elem.(bson.M); ok {
			bsonArray = append(bsonArray, bm)
		} else if doc, err := bson.Marshal(elem); err == nil {
			var bm bson.M
			if err := bson.Unmarshal(doc, &bm); err == nil {
				bsonArray = append(bsonArray, bm)
			} else {
				return nil, fmt.Errorf("failed to convert element to BSON document: %v", err)
			}
		} else {
			return nil, fmt.Errorf("element in primitive.A is not a valid BSON document")
		}
	}
	return bsonArray, nil
}

func GetNextSeqNumber(key string, orgId string) (int32, error) {
	//update to database
	filter := bson.M{"_id": key}
	updateData := bson.M{
		"$inc": bson.M{"value": 1},
	}
	result, err := ExecuteFindAndModifyQuery(orgId, "sequence", filter, updateData)
	if err != nil {
		return 0, err
	}

	return result["value"].(int32), nil
}

func ConvertPrimitiveAToStringArray(input primitive.A) []string {
	var result []string
	for _, v := range input {
		// Check if the value is of type string before adding it to the result slice
		if str, ok := v.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

func ToStringSlice(input interface{}) []string {
	var result []string

	switch v := input.(type) {
	case primitive.A: // MongoDB array
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}

	case []interface{}: // Generic interface array
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}

	case []string: // Already []string
		return v

	default:
		return result
	}

	return result
}

func GetLastDateOfCurrentMonth() time.Time {
	now := time.Now()
	currentYear, currentMonth, _ := now.Date()
	location := now.Location()

	firstOfNextMonth := time.Date(currentYear, currentMonth+1, 1, 0, 0, 0, 0, location)
	lastDate := firstOfNextMonth.AddDate(0, 0, -1)
	return lastDate
}

// CreateFilterParams is an exported wrapper for createFilterParams
func CreateFilterParams(filterParams []FilterParam, pipeline string) string {
	return createFilterParams(filterParams, pipeline)
}

func CalculateLeavesAndUpsert(leaveMap map[string]interface{}, parentId string, orgId string) error {
	var leaves []map[string]interface{}

	err, validFrom := StringToDateConverter(leaveMap["valid_from"])
	if err {
		fmt.Println()
		return fmt.Errorf("Valid From Date is Invalid")
	}
	err, validTo := StringToDateConverter(leaveMap["valid_to"])
	if err {
		return fmt.Errorf("Valid From To is Invalid")
	}
	validFromString := validFrom.Format(time.RFC3339)
	validToString := validTo.Format(time.RFC3339)
	occurrence := leaveMap["occurrence"].(string)
	name := leaveMap["name"].(string)
	status := "Active"
	CollectionName := "holiday_configuration"
	if occurrence == "Repeated" {
		occurrenceType := leaveMap["occurrence_type"].(string)

		if occurrenceType == "Fixed Date" {

			isAnnuallyRecurring, _ := leaveMap["isannuallyrecurring"].(bool)

			errFind, fixedDate := StringToDateConverter(leaveMap["fixeddate"])
			if errFind {
				return fmt.Errorf("Fixed Date is Invalid or in wrong format")
			}

			if isAnnuallyRecurring {
				// Iterate each calendar year between validFrom and validTo (inclusive)
				startYear := validFrom.Year()
				endYear := validTo.Year()

				if endYear < startYear {
					return fmt.Errorf("valid_to must be after valid_from")
				}

				for y := startYear; y <= endYear; y++ {
					nextOccurrence := time.Date(y, fixedDate.Month(), fixedDate.Day(), 0, 0, 0, 0, time.UTC)

					// Only include occurrences that fall inside valid range
					occurrenceDate := time.Date(nextOccurrence.Year(), nextOccurrence.Month(), nextOccurrence.Day(), 0, 0, 0, 0, time.UTC)
					validFromDate := time.Date(validFrom.Year(), validFrom.Month(), validFrom.Day(), 0, 0, 0, 0, time.UTC)
					validToDate := time.Date(validTo.Year(), validTo.Month(), validTo.Day(), 0, 0, 0, 0, time.UTC)

					if (occurrenceDate.Equal(validFromDate) || occurrenceDate.After(validFromDate)) &&
						(occurrenceDate.Equal(validToDate) || occurrenceDate.Before(validToDate)) {

						startDate := time.Date(nextOccurrence.Year(), nextOccurrence.Month(), nextOccurrence.Day(), 1, 1, 0, 0, time.UTC)
						endDate := time.Date(nextOccurrence.Year(), nextOccurrence.Month(), nextOccurrence.Day(), 23, 59, 59, 0, time.UTC)

						// Ensure date is a Mongo BSON datetime to avoid nulls during encoding
						leave := map[string]interface{}{
							"name":       name,
							"status":     status,
							"date":       primitive.NewDateTimeFromTime(nextOccurrence.UTC()),
							"start_date": startDate,
							"end_date":   endDate,
							"valid_from": validFrom,
							"valid_to":   validTo,
							"parent_id":  parentId,
						}

						fmt.Printf("=== FIXED DATE DEBUG (RECURRING) year=%d ===\n", y)
						fmt.Printf("Before HandleIDGeneration - date: %v\n", leave["date"])

						// Ensure we pass a bson.M to match the function signature and avoid any
						// type-conversion surprises when encoding to BSON later.
						HandleIDGeneration(bson.M(leave), orgId, CollectionName)

						fmt.Printf("After HandleIDGeneration - date: %v\n", leave["date"])
						// restore UTC normalized date as BSON datetime
						leave["date"] = primitive.NewDateTimeFromTime(nextOccurrence.UTC())
						fmt.Printf("After restore - date: %v\n", leave["date"])

						leaves = append(leaves, leave)
					}
				}

				// If no occurrences were created, return an error
				if len(leaves) == 0 {
					return fmt.Errorf("no valid occurrences found between %v and %v for date %v",
						validFrom.Format("2006-01-02"),
						validTo.Format("2006-01-02"),
						fixedDate.Format("2006-01-02"))
				}

			} else {
				// Non-recurring fixed date
				if fixedDate.Before(validFrom) || fixedDate.After(validTo) {
					return fmt.Errorf("fixed date %v must be between valid_from %v and valid_to %v",
						fixedDate.Format("2006-01-02"),
						validFrom.Format("2006-01-02"),
						validTo.Format("2006-01-02"))
				}

				startDate := time.Date(fixedDate.Year(), fixedDate.Month(), fixedDate.Day(), 1, 1, 0, 0, time.UTC)
				endDate := time.Date(fixedDate.Year(), fixedDate.Month(), fixedDate.Day(), 23, 59, 59, 0, time.UTC)

				// Ensure fixedDate is stored as Mongo BSON datetime
				leave := map[string]interface{}{
					"name":       name,
					"status":     status,
					"date":       primitive.NewDateTimeFromTime(fixedDate.UTC()),
					"start_date": startDate,
					"end_date":   endDate,
					"valid_from": validFrom,
					"valid_to":   validTo,
					"parent_id":  parentId,
				}

				fmt.Printf("=== FIXED DATE DEBUG (NON-RECURRING) ===\n")
				fmt.Printf("Before HandleIDGeneration - date: %v\n", leave["date"])

				// Convert to bson.M explicitly to match HandleIDGeneration signature
				HandleIDGeneration(bson.M(leave), orgId, CollectionName)

				fmt.Printf("After HandleIDGeneration - date: %v\n", leave["date"])
				// restore UTC normalized date as BSON datetime
				leave["date"] = primitive.NewDateTimeFromTime(fixedDate.UTC())
				fmt.Printf("After restore - date: %v\n", leave["date"])

				leaves = append(leaves, leave)
			}
		} else if occurrenceType == "Week" {
			// weekdayMap := map[time.Weekday]int{
			// 	time.Sunday:    7,
			// 	time.Monday:    1,
			// 	time.Tuesday:   2,
			// 	time.Wednesday: 3,
			// 	time.Thursday:  4,
			// 	time.Friday:    5,
			// 	time.Saturday:  6,
			// }
			daysOfOccurrence := leaveMap["daysOfOccurrence"].(float64)
			// currentDate := validFrom

			// ADD THIS DEBUG
			fmt.Printf("=== WEEK CALCULATION ===\n")
			fmt.Printf("Valid From: %v\n", validFrom)
			fmt.Printf("Valid To: %v\n", validTo)
			fmt.Printf("Looking for weekday: %d\n", int(daysOfOccurrence))

			db := database.GetConnection(orgId).Collection(CollectionName)
			res, err := db.Aggregate(ctx, bson.A{
				bson.D{
					{"$project",
						bson.D{
							{"startDate", bson.D{{"$toDate", validFromString}}},
							{"endDate", bson.D{{"$toDate", validToString}}},
						},
					},
				},
				bson.D{
					{"$addFields",
						bson.D{
							{"daysBetween",
								bson.D{
									{"$dateDiff",
										bson.D{
											{"startDate", "$startDate"},
											{"endDate", "$endDate"},
											{"unit", "day"},
										},
									},
								},
							},
						},
					},
				},
				bson.D{
					{"$project",
						bson.D{
							{"holidays",
								bson.D{
									{"$filter",
										bson.D{
											{"input",
												bson.D{
													{"$map",
														bson.D{
															{"input",
																bson.D{
																	{"$range",
																		bson.A{
																			0,
																			bson.D{
																				{"$add",
																					bson.A{
																						"$daysBetween",
																						1,
																					},
																				},
																			},
																		},
																	},
																},
															},
															{"as", "offset"},
															{"in",
																bson.D{
																	{"date",
																		bson.D{
																			{"$dateAdd",
																				bson.D{
																					{"startDate", "$startDate"},
																					{"unit", "day"},
																					{"amount", "$$offset"},
																				},
																			},
																		},
																	},
																	{"dow",
																		bson.D{
																			{"$dayOfWeek",
																				bson.D{
																					{"$dateAdd",
																						bson.D{
																							{"startDate", "$startDate"},
																							{"unit", "day"},
																							{"amount", "$$offset"},
																						},
																					},
																				},
																			},
																		},
																	},
																},
															},
														},
													},
												},
											},
											{"as", "dayInfo"},
											{"cond",
												bson.D{
													{"$or",
														bson.D{
															{"$eq",
																bson.A{
																	"$$dayInfo.dow",
																	1,
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				bson.D{
					{"$project",
						bson.D{
							{"holidayDates",
								bson.D{
									{"$map",
										bson.D{
											{"input", "$holidays"},
											{"as", "s"},
											{"in", "$$s.date"},
										},
									},
								},
							},
							{"_id", 0},
						},
					},
				},
			})
			if err != nil {
				return fmt.Errorf("aggregation failed: %v", err)
			}

			var result []HolidayResult
			if err := res.All(ctx, &result); err != nil {
				return fmt.Errorf("failed to decode result: %v", err)
			}

			for _, date := range result[0].HolidayDates {
				leaves = append(leaves, map[string]interface{}{
					"name":      name,
					"status":    "Active",
					"date":      date,
					"parent_id": parentId,
					"_id":       uuid.New().String(),
				})
			}
			// Loop through ENTIRE valid period (5 years, not 1 year)
			// for currentDate.Before(validTo) {
			// 	// Check if currentDate matches the specified day of the week
			// 	if int(weekdayMap[currentDate.Weekday()]) == int(daysOfOccurrence) {
			// 		leave := map[string]interface{}{
			// 			"name":       name,
			// 			"status":     status,
			// 			"start_date": time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 1, 1, 0, 0, time.UTC),
			// 			"end_date":   time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 23, 59, 59, 0, time.UTC),
			// 			"valid_from": validFrom.UTC().Format(time.RFC3339),
			// 			"valid_to":   validTo.UTC().Format(time.RFC3339),
			// 			"parent_id":  parentId,
			// 		}

			// 		leaves = append(leaves, leave)
			// 		count++
			// 	}
			// 	// Move to next day
			// 	currentDate = currentDate.AddDate(0, 0, 1)
			// }

			// HandleIDGeneration(leave, orgId, CollectionName)
			fmt.Printf("Total leaves generated: %d\n", len(leaves))
			fmt.Printf("========================\n")
		}
	} else {
		errFind, date := StringToDateConverter(leaveMap["fixeddate"])
		if errFind {
			return fmt.Errorf("Fixed Date is Invalid for non-repeated occurrence")
		}

		fmt.Printf("=== NON-REPEATED DATE DEBUG ===\n")
		fmt.Printf("Original fixeddate: %v\n", leaveMap["fixeddate"])
		fmt.Printf("Parsed date: %v\n", date)
		fmt.Printf("Date UTC: %v\n", date.UTC())

		leave := map[string]interface{}{
			"name":       name,
			"status":     status,
			"date":       date.UTC(),
			"start_date": time.Date(date.Year(), date.Month(), date.Day(), 1, 1, 0, 0, time.UTC),
			"end_date":   time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, time.UTC),
			"valid_from": validFrom.UTC(),
			"valid_to":   validTo.UTC(),
			"parent_id":  parentId,
		}

		fmt.Printf("Leave object date field: %v\n", leave["date"])
		fmt.Printf("===============================\n")

		// Convert to bson.M explicitly to match HandleIDGeneration signature
		HandleIDGeneration(bson.M(leave), orgId, CollectionName)
		leaves = append(leaves, leave)
	}

	// Perform bulk upsert
	var models []mongo.WriteModel
	for i, leave := range leaves {
		fmt.Printf("=== BULK UPSERT DEBUG - Record %d ===\n", i)
		fmt.Printf("Leave date before filter: %v\n", leave["date"])
		fmt.Printf("Leave date type: %T\n", leave["date"])

		// Normalize the date field to time.Time (UTC). If it's missing or invalid,
		// skip this record to avoid inserting a null date into MongoDB.
		var normalizedDate interface{}
		switch d := leave["date"].(type) {
		case time.Time:
			// convert to Mongo primitive.DateTime
			normalizedDate = primitive.NewDateTimeFromTime(d.UTC())
		case *time.Time:
			if d != nil {
				normalizedDate = primitive.NewDateTimeFromTime(d.UTC())
			} else {
				normalizedDate = nil
			}
		case string:
			if ok, parsed := StringToDateConverter(d); !ok {
				// parsing failed, set to nil
				normalizedDate = nil
			} else {
				normalizedDate = primitive.NewDateTimeFromTime(parsed.UTC())
			}
		default:
			// leave as-is (could be already a primitive.DateTime or nil)
			normalizedDate = d
		}

		if normalizedDate == nil {
			fmt.Printf("Skipping record %d: date is nil or invalid\n", i)
			continue
		}

		filter := bson.M{
			"parent_id": leave["parent_id"],
			"date":      normalizedDate,
		}

		// Ensure the leave map has the normalized date value for the upsert
		leave["date"] = normalizedDate

		// Dump BSON extJSON for the leave map and the update to inspect what will be sent to MongoDB
		if b, err := bson.MarshalExtJSON(leave, true, true); err == nil {
			fmt.Printf("Leave BSON JSON: %s\n", string(b))
		} else {
			fmt.Printf("Failed to marshal leave to extjson: %v\n", err)
		}

		update := bson.M{"$set": leave}
		if b2, err := bson.MarshalExtJSON(update, true, true); err == nil {
			fmt.Printf("Update BSON JSON: %s\n", string(b2))
		} else {
			fmt.Printf("Failed to marshal update to extjson: %v\n", err)
		}

		fmt.Printf("Filter date: %v\n", filter["date"])
		fmt.Printf("Update leave date: %v\n", leave["date"])

		model := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)
		models = append(models, model)
		fmt.Printf("=====================================\n")
	}

	// Execute bulk write
	// Perform bulk upsert
	fmt.Printf("=== BULK UPSERT ===\n")
	fmt.Printf("Leaves array length: %d\n", len(leaves))

	// for _, leave := range leaves {

	// 	filter := bson.M{
	// 		"parent_id":  leave["parent_id"],
	// 		"start_date": leave["start_date"],
	// 		"end_date":   leave["end_date"],
	// 	}

	// 	UpdateDateObject(leave)

	// 	update := bson.M{"$set": leave}
	// 	model := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)
	// 	models = append(models, model)

	// }

	fmt.Printf("Models created: %d\n", len(models))

	// Execute bulk write
	if len(models) > 0 {
		result, err := BulkWriteOnDb(orgId, "leave", models)
		if err != nil {
			return fmt.Errorf("bulk upsert failed: %v", err)
		}
		fmt.Printf("Bulk write completed. Result: %+v\n", result)
	}
	fmt.Printf("===================\n")

	return nil
}

/*
?  For Every time we query the Database for checking leave is present or not
	possibility
	   data present it return true i.e count > 0
	   data Not present it return false i.e count == 0
*/

func IsSchedulePresent(collection *mongo.Collection, scheduledDate time.Time) bool {
	startTime := time.Date(scheduledDate.Year(), scheduledDate.Month(), scheduledDate.Day(), 0, 0, 0, 0, time.UTC)
	endTime := time.Date(scheduledDate.Year(), scheduledDate.Month(), scheduledDate.Day(), 23, 59, 59, 999999999, time.UTC)

	filter := bson.M{
		"start_date": bson.D{
			{"$gte", startTime},
			{"$lte", endTime},
		},
		"end_date": bson.D{
			{"$gte", startTime},
			{"$lte", endTime},
		},
	}
	count, err := collection.CountDocuments(context.TODO(), filter)
	if err != nil {
		return false
	}
	return !(count == 0)
}

func BulkWriteOnDb(orgId string, collectionName string, inputData []mongo.WriteModel) (*mongo.BulkWriteResult, error) {
	res, err := database.GetConnection(orgId).Collection(collectionName).BulkWrite(context.TODO(), inputData)

	return res, err
}

// StringToDateConverter checks if the input is a valid date or converts a string to time.Time

func StringToDateConverter(date interface{}) (bool, time.Time) {
	switch v := date.(type) {
	case time.Time:
		return false, v
	case string:
		layouts := []string{
			time.RFC3339Nano, // 2025-10-02T00:00:00.000Z or with fractional seconds
			time.RFC3339,     // 2025-10-02T00:00:00Z
			"2006-01-02",     // 2025-10-02
			"02-01-2006",     // 02-10-2025
			"2006/01/02",     // 2025/10/02
			"02-Jan-2006",    // 02-Oct-2025
		}

		for _, layout := range layouts {
			if parsedDate, err := time.Parse(layout, v); err == nil {
				return false, parsedDate
			}
		}
	}
	return true, time.Time{}
}
func splitBatches(models []mongo.WriteModel, batchSize int) [][]mongo.WriteModel {
	var batches [][]mongo.WriteModel
	for batchSize < len(models) {
		models, batches = models[batchSize:], append(batches, models[0:batchSize:batchSize])
	}
	batches = append(batches, models)
	return batches
}
func PrepareAttendanceModels(employees []bson.M, startDate time.Time, totalDays int) []mongo.WriteModel {
	var models []mongo.WriteModel

	for _, emp := range employees {
		empID := emp["_id"].(string)

		for i := 0; i < totalDays; i++ {
			attDate := startDate.AddDate(0, 0, i)
			dateStr := attDate.Format("2006-01-02")            // convert date to string
			uniqueID := fmt.Sprintf("%s-D-%s", empID, dateStr) // EMPID-DATE format

			// document to insert or update
			doc := bson.D{
				{"_id", uniqueID},
				{"createdby", "Server"},
				{"employee_id", empID},
				{"status", "Active"},
				{"present", false},
				{"date", attDate},
			}

			// use UpdateOneModel with upsert=true
			model := mongo.NewUpdateOneModel().
				SetFilter(bson.M{"_id": uniqueID}).
				SetUpdate(bson.D{{"$setOnInsert", doc}}).
				SetUpsert(true)

			models = append(models, model)
		}
	}

	return models
}

func InsertAttendanceRecords(ctx context.Context, db *mongo.Database, models []mongo.WriteModel) error {
	if len(models) == 0 {
		return nil
	}

	batches := splitBatches(models, 500)
	for idx, batch := range batches {
		_, err := db.Collection("attendance_info").BulkWrite(ctx, batch)
		if err != nil {
			log.Printf("Bulk insert failed for batch %d: %v", idx+1, err)
			return fmt.Errorf("Failed to insert batch %d", idx+1)
		}
		log.Printf("Batch %d inserted successfully (%d records)", idx+1, len(batch))
	}

	return nil
}

// ParseDate tries to convert any interface{} into time.Time
func ParseDate(value interface{}) time.Time {
	// default: now
	now := time.Now()

	switch v := value.(type) {

	// Already a time.Time
	case time.Time:
		return v

	// MongoDB primitive.DateTime
	case primitive.DateTime:
		return v.Time()

	// UNIX timestamps (sec)
	case int64:
		return time.Unix(v, 0)

	// UNIX timestamps (sec) as float
	case float64:
		return time.Unix(int64(v), 0)

	// String formats
	case string:
		// Try RFC3339
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		// Try common format (2025-01-02 15:04:05)
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			return t
		}
		// Try date only (2025-01-02)
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t
		}
		return now

	default:
		return now
	}
}

func ConvertToUTC(input string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000-07:00", input)
	if err != nil {
		return time.Now().UTC()
	}
	return t.UTC()
}

func GenerateMultipleMaintenanceData(data map[string]interface{}, orgId string, parentId string, oldData []bson.M, isPost bool) error {
	if data["frequency"] == nil || data["duration"] == nil {
		return nil
	}
	var startTime time.Time
	var startTimeIncluded bool
	var newStartTimeString string
	var oldStartTimeString string
	var err error
	frequency := data["frequency"].(string)
	duration := ToInt(data["duration"])
	data["status"] = "Not Done"
	data["parent_id"] = parentId
	data["maintenance_type"] = "Scheduled"
	data["type"] = "Machine Maintenance"

	if data["start_time"] != nil {
		startTimeStr := data["start_time"].(string)
		newStartTimeString = data["start_time"].(string)
		startTime, err = time.Parse("03:04 PM", startTimeStr)
		if err != nil {
			fmt.Println(err.Error(), "git")
			return fmt.Errorf("invalid start_time format: %v", err)
		} else {
			fmt.Println("No error")
		}
		startTimeIncluded = true

	}

	// Start scheduling from the current date
	startDate := time.Now()

	if !isPost {
		res := oldData[0]
		oldDuration := ToInt(res["duration"])
		oldFrequency := res["frequency"]
		if res["start_time"] != nil {
			oldStartTimeString = res["start_time"].(string)
		}
		if duration != oldDuration {
			deleteFilter := bson.M{"parent_id": parentId}
			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").DeleteMany(ctx, deleteFilter)
			if err != nil {
				return shared.InternalServerError("Error Deleting Data")
			}
		} else if oldFrequency != frequency {
			deleteFilter := bson.M{"parent_id": parentId}
			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").DeleteMany(ctx, deleteFilter)
			if err != nil {
				return shared.InternalServerError("Error Deleting Data")
			}
		} else if oldStartTimeString != newStartTimeString {
			deleteFilter := bson.M{"parent_id": parentId}
			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").DeleteMany(ctx, deleteFilter)
			if err != nil {
				return shared.InternalServerError("Error Deleting Data")
			}
		}

		if (oldDuration == duration) && (oldFrequency == frequency) && (oldStartTimeString == newStartTimeString) {
			return nil
		}
	}

	// lastDateOfCurrentMonth := GetLastDateOfCurrentMonth()
	// lastDate := lastDateOfCurrentMonth.Day()
	// currentDate := time.Now().Day()
	// loopDate := lastDate - currentDate
	// fmt.Println(loopDate)

	var startHour int
	var startMinute int

	if startTimeIncluded {
		startHour = startTime.Hour()
		startMinute = startTime.Minute()
	} else {
		startHour = 9
		startMinute = 0
	}

	leaveCollection := database.GetConnection(orgId).Collection("leave")

	if frequency == "Hourly" {
		durationHours := int(duration) // Dynamic duration (e.g., 2 hours)

		for day := 1; day <= 7; day++ {
			// Calculate the start time for the current day
			dailyStartTime := time.Date(
				startDate.Year(), startDate.Month(), startDate.Day()+day,
				startHour, startMinute, 0, 0, startDate.Location(),
			)

			// Skip Sundays
			// if dailyStartTime.Weekday() == time.Sunday {
			// 	continue
			// }

			// Set the current time to the start of the day
			currentTime := dailyStartTime

			// Ensure the start time is valid (adjust to 9:00 AM if outside the valid range)
			if currentTime.Hour() < 9 || currentTime.Hour() >= 22 {
				currentTime = time.Date(
					currentTime.Year(), currentTime.Month(), currentTime.Day(),
					9, 0, 0, 0, currentTime.Location(),
				)
			}
			present := IsSchedulePresent(leaveCollection, currentTime)
			if present {
				continue
			}
			// Generate occurrences for the day
			for currentTime.Hour() < 22 && currentTime.Hour() >= 9 {
				// Add the occurrence to the database
				delete(data, "_id")
				data["scheduled_date"] = currentTime
				update := bson.M{"$set": data}

				uniqueId := uuid.New().String()
				_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
					ctx, DocIdFilter(uniqueId), update, updateOpts,
				)
				if err != nil {
					return shared.BadRequest(err.Error())
				}

				// Increment the time by durationHours
				currentTime = currentTime.Add(time.Duration(durationHours) * time.Hour)

				// Stop if the next occurrence goes beyond 10:00 PM
				if currentTime.Hour() > 22 {
					break
				}
			}
		}

		return nil
	} else if frequency == "Daily" {
		for day := 1; day <= 30; day += int(duration) {
			// Calculate the start time for the current day
			dailyStartTime := time.Date(
				startDate.Year(), startDate.Month(), startDate.Day()+day,
				startHour, startMinute, 0, 0, startDate.Location(),
			)

			// Skip Sundays
			// if dailyStartTime.Weekday() == time.Sunday {
			// 	continue
			// }
			present := IsSchedulePresent(leaveCollection, dailyStartTime)
			if present {
				continue
			}

			// Prepare the occurrence
			delete(data, "_id")
			data["scheduled_date"] = dailyStartTime
			update := bson.M{"$set": data}

			uniqueId := uuid.New().String()
			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
				ctx, DocIdFilter(uniqueId), update, updateOpts,
			)
			if err != nil {
				return shared.BadRequest(err.Error())
			}
		}
		return nil
	} else if frequency == "Monthly" {
		for month := 0; month <= 12; month += duration {
			// Add the number of months to startDate
			occurrenceDate := startDate.AddDate(0, month, 0)

			// Adjust the day to match startDate's day, handle months with fewer days
			day := startDate.Day()
			daysInMonth := daysIn(occurrenceDate.Year(), occurrenceDate.Month())

			// If the start day exceeds the days in the current month, set to the last day
			if day > daysInMonth {
				day = daysInMonth
			}

			// Create the occurrence date with the adjusted day
			occurrenceDate = time.Date(
				occurrenceDate.Year(),
				occurrenceDate.Month(),
				day,
				startHour,
				startMinute,
				0,
				0,
				occurrenceDate.Location(),
			)

			// Skip Sundays
			// if occurrenceDate.Weekday() == time.Sunday {
			// 	continue
			// }
			present := IsSchedulePresent(leaveCollection, occurrenceDate)
			if present {
				continue
			}

			// Prepare the occurrence
			delete(data, "_id")
			data["scheduled_date"] = occurrenceDate
			update := bson.M{"$set": data}

			uniqueId := uuid.New().String()
			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
				ctx, DocIdFilter(uniqueId), update, updateOpts,
			)
			if err != nil {
				return shared.BadRequest(err.Error())
			}
		}

	} else if frequency == "Weekly" {
		// Define the number of weeks to generate in the next month
		daysInMonth := daysIn(startDate.Year(), startDate.Month()) // Get the days in the current month
		weeksToGenerate := daysInMonth / 7                         // Calculate the number of full weeks in the month

		// Adjust for any partial week at the end of the month if needed
		for week := 1; week <= weeksToGenerate; week++ {
			// Calculate the occurrence date for the current week (7 days apart)
			occurrenceDate := startDate.AddDate(0, 0, week*7*int(duration))

			// Ensure the scheduled time is within the daily bounds (set start time)
			occurrenceDate = time.Date(
				occurrenceDate.Year(),
				occurrenceDate.Month(),
				occurrenceDate.Day(),
				startHour,
				startMinute,
				0,
				0,
				occurrenceDate.Location(),
			)

			// Skip Sundays
			// if occurrenceDate.Weekday() == time.Sunday {
			// 	continue
			// }

			present := IsSchedulePresent(leaveCollection, occurrenceDate)
			if present {
				continue
			}
			// Prepare the occurrence
			delete(data, "_id")
			data["scheduled_date"] = occurrenceDate
			update := bson.M{"$set": data}

			uniqueId := uuid.New().String()
			_, err := database.GetConnection(orgId).Collection("equipment_maintenance_data").UpdateOne(
				ctx, DocIdFilter(uniqueId), update, updateOpts,
			)
			if err != nil {
				return shared.BadRequest(err.Error())
			}
		}
	}

	return nil
}

func daysIn(year int, month time.Month) int {
	t := time.Date(year, month, 0, 0, 0, 0, 0, time.UTC)
	return t.Day()
}
