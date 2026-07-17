package helper

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"

	// "go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
)

var updateOpts = options.Update().SetUpsert(true)
var findUpdateOpts = options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

func GetAggregateQueryResult(orgId string, collectionName string, query interface{}) ([]bson.M, error) {
	response, err := ExecuteAggregateQuery(orgId, collectionName, query)
	if err != nil {
		return nil, err
	}
	var result []bson.M
	//var result map[string][]Config
	if err = response.All(ctx, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func ExecuteAggregateQuery(orgId string, collectionName string, query interface{}) (*mongo.Cursor, error) {
	// Soft Deleted
	// pipeline := make([]bson.M, 0)

	// matchStage := bson.M{
	// 	"$match": bson.M{
	// 		"is_delete": bson.M{
	// 			"$ne": true,
	// 		},
	// 	},
	// }

	// pipeline = append(pipeline, matchStage)

	// err := appendQueryToPipeline(&pipeline, query)
	// if err != nil {
	// 	return nil, err
	// }

	cur, err := database.GetConnection(orgId).Collection(collectionName).Aggregate(ctx, query, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, err
	}

	return cur, nil
}

func GetQueryResult(orgId string, collectionName string, query interface{}, page int64, limit int64, sort interface{}) ([]bson.M, error) {
	response, err := ExecuteQuery(orgId, collectionName, query, page, limit, sort)
	if err != nil {
		return nil, err
	}

	var result []bson.M
	//var result map[string][]Config
	if err = response.All(ctx, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func GetQueryInBetweenId(orgId string, collectionName string, options *options.FindOptions, startId string, endId string) ([]bson.M, error) {
	var result []bson.M
	// Construct a filter to find documents with IDs between abc123 and xyz789
	filter := bson.M{
		"_id": bson.M{
			"$gte": startId,
			"$lte": endId,
		},
	}
	cur, err := database.GetConnection(orgId).Collection(collectionName).Find(ctx, filter, options)
	if err != nil {
		return nil, err
	}
	if err = cur.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func ExecuteHistoryInsertMany(orgId string, collectionName string, docs []interface{}) (*mongo.InsertManyResult, error) {
	result, err := database.GetConnection(orgId).Collection(collectionName+"_history").InsertMany(ctx, docs)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ExecuteDeleteManyByIds(orgId string, collectionName string, filter bson.M) (*mongo.DeleteResult, error) {
	result, err := database.GetConnection(orgId).Collection(collectionName).DeleteMany(ctx, filter)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ExecuteQuery(orgId string, collectionName string, query interface{}, page int64, limit int64, sort interface{}) (*mongo.Cursor, error) {
	pageOptions := options.Find()
	pageOptions.SetSkip(page)   //0-i
	pageOptions.SetLimit(limit) // number of records to return
	if sort != nil {
		pageOptions.SetSort(sort)
	}
	response, err := database.GetConnection(orgId).Collection(collectionName).Find(ctx, query, pageOptions)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func ExecuteFindAndModifyQuery(orgId string, collectionName string, filter interface{}, data interface{}) (bson.M, error) {
	var result bson.M

	err := database.GetConnection(orgId).Collection(collectionName).FindOneAndUpdate(ctx, filter, data, findUpdateOpts).Decode(&result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func CollectionNameGet(model_name, orgId string) (string, error) {

	filter := bson.M{
		"model_name":    model_name,
		"is_collection": "Yes",
	}
	Response, _ := FindOneDocument(orgId, "model_config", filter)
	if Response == nil {
		return "", shared.BadRequest("Invalid Model Config")
	}

	return Response["collection_name"].(string), nil
}

func Updateformodel(c *fiber.Ctx) error {

	org, exists := GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collectionName")

	// If the ID is not a valid ObjectID, search using the ID as a string
	filter := DocIdFilter(c.Params("id"))

	var inputData map[string]interface{}
	if err := c.BodyParser(&inputData); err != nil {
		log.Println(err)
		return c.Status(fiber.StatusBadRequest).JSON(shared.Response{
			Status:   fiber.StatusBadRequest,
			ErrorMsg: "Error parsing request body",
		})
	}
	update := bson.M{
		"$set": inputData,
	}
	// Update data in the collection
	_, err := database.GetConnection(org.Id).Collection(collectionName).UpdateOne(ctx, filter, update)
	if err != nil {
		response := shared.Response{
			Status:   fiber.StatusInternalServerError,
			ErrorMsg: err.Error(),
		}
		return c.Status(fiber.StatusInternalServerError).JSON(response)
	}

	response := fiber.Map{
		"status":    200,
		"data":      []map[string]interface{}{inputData},
		"error_msg": "",
	}
	return c.Status(fiber.StatusOK).JSON(response)
}

// BuildAggregationPipeline --METHOD constructs a MongoDB aggregation pipeline based on input filter conditions.
// It generates match conditions using GenerateAggregationPipeline for each condition and combines them based on the logical clause.
// The resulting pipeline includes a $match stage with the combined match conditions.
func BuildAggregationPipeline(inputData []FilterCondition, BasecollectionName string) bson.M {
	// Initialize an empty array to store individual match conditions.
	var matchConditions []bson.M

	// Iterate over each filter condition in the input data.
	for _, filter := range inputData {
		// Iterate over each condition within the filter.
		for _, condition := range filter.Conditions {
			// Generate an aggregation pipeline for the current condition.
			// The GenerateAggregationPipeline function is assumed to return a slice of bson.M representing pipeline stages.
			conditionPipeline := GenerateAggregationPipeline(condition, BasecollectionName)

			// Append the generated pipeline stages to the match conditions array.
			matchConditions = append(matchConditions, conditionPipeline...)
		}
	}

	// Initialize an empty bson.M to represent the final match clause.
	var clause bson.M

	// Check if there are any match conditions.
	if len(matchConditions) > 0 {
		// Combine match conditions based on the logical clause (OR or AND).
		if inputData[0].Clause == "OR" {
			clause = bson.M{"$or": matchConditions}
		} else if inputData[0].Clause == "AND" {
			clause = bson.M{"$and": matchConditions}
		}
	}

	// Return the final aggregation pipeline with the $match stage.
	return bson.M{"$match": clause}
}

// GenerateAggregationPipeline  -- METHOD build the Aggregation
func GenerateAggregationPipeline(condition ConditionGroup, basecollection string) []bson.M {
	conditions := []bson.M{}

	// If there are nested conditions, recursively generate pipelines for each nested condition.
	if len(condition.Conditions) > 0 {
		// nestedConditions := []bson.M{}
		for _, nestedCondition := range condition.Conditions {
			Nested := GenerateAggregationPipeline(nestedCondition, basecollection)

			conditions = append(conditions, Nested...)
		}

	}

	column := condition.Column
	value := condition.Value

	reference := condition.ParentCollectionName

	// Handle the case where basecollection is empty or ParentCollectionName is empty.
	if basecollection == "" {
		column = condition.Column
	} else if condition.ParentCollectionName == "" { //If ParentCollectioName is  empty we use directly use columnName
		column = condition.Column
	} else if basecollection != condition.ParentCollectionName { //If basecollection and ParentCollectionName is not equal that time suse refence variable for DOT
		column = reference + "." + fmt.Sprint(column)
	}

	//What are the Opertor is here mention that  map
	operatorMap := map[string]string{
		"EQUALS":             "$eq",
		"NOTEQUAL":           "$ne",
		"CONTAINS":           "$regex",
		"NOTCONTAINS":        "$regex",
		"STARTSWITH":         "$regex",
		"ENDSWITH":           "$regex",
		"LESSTHAN":           "$lt",
		"GREATERTHAN":        "$gt",
		"LESSTHANOREQUAL":    "$lte",
		"GREATERTHANOREQUAL": "$gte",
		"INRANGE":            "$gte",
		"BLANK":              "$exists",
		"NOTBLANK":           "$exists",
		"EXISTS":             "$exists",
		"IN":                 "$in",
	}

	// Check if the specified operator exists in the operator map.
	if operator, exists := operatorMap[condition.Operator]; exists {
		conditionValue := ConvertToDataType(value, condition.Type)

		if condition.Operator == "INRANGE" || condition.Operator == "IN_BETWEEN" {
			if condition.Type == "date" || condition.Type == "time.Time" {
				dateValues, isDate := value.([]interface{})
				if isDate && len(dateValues) == 2 {
					startDateValue, startOK := dateValues[0].(string)
					endDateValue, endOK := dateValues[1].(string)
					if startOK && endOK {
						startDate, startErr := time.Parse(time.RFC3339, startDateValue)
						endDate, endErr := time.Parse(time.RFC3339, endDateValue)
						if startErr == nil && endErr == nil {
							startOfDay := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, time.UTC)
							endOfDay := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 999999999, time.UTC)
							conditions = append(conditions, bson.M{column: bson.M{
								"$gte": startOfDay,
								"$lte": endOfDay,
							}})
						}
					}
				}
			} else {
				rangeValues, ok := value.([]interface{})
				if ok && len(rangeValues) == 2 {
					minValue := rangeValues[0]
					maxValue := rangeValues[1]
					conditions = append(conditions, bson.M{column: bson.M{"$gte": minValue, "$lte": maxValue}})
				}
			}
		}

		if condition.Operator == "BLANK" {
			conditions = append(conditions, bson.M{column: bson.M{operator: false}})
		} else if condition.Operator == "EXISTS" {
			conditions = append(conditions, bson.M{column: bson.M{operator: conditionValue}})
		} else if condition.Operator == "CONTAINS" {
			conditions = append(conditions, bson.M{column: bson.M{operator: conditionValue, "$options": "i"}})
		} else if condition.Operator == "NOTCONTAINS" {
			pattern := fmt.Sprintf("^(?!.*%s)", condition.Value)
			conditions = append(conditions, bson.M{condition.Column: bson.M{operator: pattern}})
		} else if condition.Operator == "NOTBLANK" {
			conditions = append(conditions, bson.M{column: bson.M{operator: true, "$ne": nil}})
		} else if condition.Operator == "IN" {
			conditions = append(conditions, bson.M{column: bson.M{operator: value.([]interface{})}})
		} else if condition.Operator == "EQUALS" && condition.Type == "time.Time" {

			t, _ := conditionValue.(time.Time)
			StartedDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			endDate := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC)

			filter := bson.M{
				column: bson.M{
					"$gte": StartedDay,
					"$lte": endDate,
				},
			}

			conditions = append(conditions, filter)

		} else {

			conditions = append(conditions, bson.M{column: bson.M{operator: conditionValue}})
		}
	}

	// Handle logical clauses (AND or OR).
	if condition.Clause == "AND" {
		conditions = append(conditions, bson.M{"$and": conditions})
	} else if condition.Clause == "OR" {
		conditions = append(conditions, bson.M{"$or": conditions})
	}

	return conditions
}

// PagiantionPipeline -- METHOD Pagination return set of Limit data return
func PagiantionPipeline(start, end int) bson.M {
	// Get the Default value from env file
	startValue, _ := strconv.Atoi(os.Getenv("DEFAULT_START_VALUE"))
	endValue, _ := strconv.Atoi(os.Getenv("DEFAULT_LIMIT_VALUE"))

	//param is empty set the Default value

	if start == 0 || end == 0 {
		start = startValue
		if end == 0 {
			end = endValue
		}
	}
	fmt.Println(start, end, "start end")
	// fmt.Println(end)
	// return the bson for pagination
	pipeline := bson.M{
		"$facet": bson.M{
			"response": []bson.M{
				{"$skip": start},
				{"$limit": end - start},
			},
			"pagination": []bson.M{
				{"$count": "totalDocs"},
			},
		},
		// "$addFields": bson.M{
		// 	"pagination.datalength": bson.M{
		// 		"$size": "$response",
		// 	},
		// },
	}
	return pipeline

}

func GenerateOrgIdFilter(OrgId string) bson.M {
	pipeline := bson.M{
		"$match": bson.M{"org_id": OrgId},
	}
	return pipeline
}

// ConvertToDataType converts the given value to the specified data type based on the provided DataType.
func ConvertToDataType(value interface{}, DataType string) interface{} {
	// Check the data type and perform the corresponding conversion.
	if DataType == "time.Time" || DataType == "date" {
		// If the data type is time.Time, attempt to parse the value as a string in RFC3339 format.
		if valStr, ok := value.(string); ok {
			t, err := time.Parse(time.RFC3339, valStr)
			if err == nil {
				// If parsing is successful, return a time.Time value with truncated seconds.
				StartedDay := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
				return StartedDay
			}
		}
	} else if DataType == "string" || DataType == "text" {
		// If the data type is string or text, attempt to parse the value as a string.
		if valStr, ok := value.(string); ok {
			t, err := time.Parse(time.RFC3339, valStr)
			// If parsing as time is successful, return a time.Time value with truncated seconds.
			if err == nil {
				StartedDay := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
				return StartedDay
			} else {
				// If parsing as time fails, return the original string value.
				return valStr
			}
		}
	} else if DataType == "boolean" || DataType == "bool" {
		// If the data type is boolean or bool, attempt to cast the value to a boolean.
		if boolValue, ok := value.(bool); ok {
			return boolValue
		}
	}
	// If the data type is not recognized or conversion is not possible, return the original value.
	return value
}

// UpdateDatasetConfig -- METHOD update the Data  to Db from filter and Data and collectionName from Param
func UpdateDataToDb(orgId string, filter interface{}, Data interface{}, collectionName string) (fiber.Map, error) {

	update := bson.M{
		"$set": Data,
	}

	res, err := database.GetConnection(orgId).Collection(collectionName).UpdateOne(ctx, filter, update, updateOpts)
	if err != nil {
		return nil, shared.InternalServerError(err.Error())
	}
	UpdatetResponse := fiber.Map{
		"status":  "success",
		"message": "Update Successfully",
		"Data":    res.UpsertedID,
	}

	return UpdatetResponse, err
}

func InsertData(c *fiber.Ctx, orgId string, collectionName string, data interface{}) error {

	response, err := database.GetConnection(orgId).Collection(collectionName).InsertOne(ctx, data)
	if err != nil {
		return shared.BadRequest(err.Error())
	}
	return shared.SuccessResponse(c, response)
}

// func UpdateDateObject(input map[string]interface{}) error {
// 	for k, v := range input {
// 		if v == nil {
// 			continue
// 		}
// 		ty := reflect.TypeOf(v).Kind().String()
// 		if ty == "string" {
// 			val := reflect.ValueOf(v).String()
// 			t, err := time.Parse(time.RFC3339, val)
// 			if err == nil {
// 				input[k] = t.UTC()
// 			}
// 		} else if ty == "map" {
// 			return UpdateDateObject(v.(map[string]interface{}))
// 		} else if ty == "slice" {
// 			for _, e := range v.([]interface{}) {
// 				if reflect.TypeOf(e).Kind().String() == "map" {
// 					return UpdateDateObject(e.(map[string]interface{}))
// 				}
// 			}
// 		}
// 	}
// 	return nil
// }

func UpdateDateObject(input interface{}) {
	switch val := input.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if v == nil {
				continue
			}

			switch innerVal := v.(type) {
			case string:
				t, err := time.Parse(time.RFC3339, innerVal)
				if err == nil {
					val[k] = t.UTC()
				}
			case map[string]interface{}:
				UpdateDateObject(innerVal) // Recursively process nested map
			case []interface{}:
				for _, e := range innerVal {
					if m, isMap := e.(map[string]interface{}); isMap {
						UpdateDateObject(m) // Recursively process nested map in the slice
					} else {
						UpdateDateObject(e)
					}
				}
			}
		}
	case []interface{}:
		for _, e := range val {
			if m, isMap := e.(map[string]interface{}); isMap {
				UpdateDateObject(m) // Recursively process nested map in the slice
			} else {
				UpdateDateObject(e)
			}
		}
	}
}

func HandlerCollectionCount(orgId, collection string, filter interface{}) (int64, error) {
	count, err := database.GetConnection(orgId).Collection(collection).CountDocuments(ctx, filter)
	if err != nil {
		return 0, nil
	}

	return count, nil

}
func FindOneDocument(orgId, collection string, filter interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}

	err := database.GetConnection(orgId).Collection(collection).FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {

			return nil, nil
		}

		return nil, err
	}

	return result, nil
}

// Generateuniquekey --METHOD create a uniquekey
func Generateuniquekey() string {

	return regexp.MustCompile(`[^a-zA-Z0-9 ]+`).ReplaceAllString(uuid.New().String(), "")

}

// HandleSequenceOrder -- METHOD extracts the sequence identifier from a key string.
func HandleSequenceOrder(key, OrgID string, collectionName string) (string, error) {
	var TempId string
	parts := strings.Split(key, "SEQ|")
	if len(parts) > 1 {
		ID := parts[1]
		ID = strings.ReplaceAll(ID, " ", "_")
		TempId = ID
		if collectionName == "purchase" {
			ID = OrgID + "-" + "purchase"
		} else if collectionName == "sale" {
			ID = OrgID + "-" + "sale"
		}
		updateData := bson.M{
			"$set": bson.M{
				"endvalue": 999,
				"suffix":   "-",
				"_id":      ID,
			},
			"$inc": bson.M{
				"start_value": 1,
			},
		}

		query := bson.M{"_id": ID}
		result, _ := ExecuteFindAndModifyQuery(OrgID, "sequence", query, updateData)

		startValue, ok := result["start_value"].(int32)
		if !ok {
			return "", fmt.Errorf("start_value is not of type int")
		}
		if collectionName == "purchase" {
			return fmt.Sprintf("%s%03d", TempId, startValue), nil
		} else if collectionName == "sale" {
			return fmt.Sprintf("%s%03d", TempId, startValue), nil
		}
		return fmt.Sprintf("%s%03d", ID, startValue), nil

	} else {
		return key, nil
	}
}
