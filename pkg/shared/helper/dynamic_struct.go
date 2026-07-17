package helper

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/gofiber/fiber/v2/log"
	"go.mongodb.org/mongo-driver/bson"
	"kriyatec.com/pms-api/pkg/shared/database"
)

// TypeMap holds references to primitive types.
var TypeMap = map[string]interface{}{

	"string":    new(string),
	"time":      new(time.Time),
	"bool":      new(bool),
	"int":       new(int),
	"int32":     new(int32),
	"int64":     new(int64),
	"float64":   new(float64),
	"float32":   new(float32),
	"time.Time": new(*time.Time),
	"[]string":  new([]string),

	"[]int":       new([]int),
	"[]int32":     new([]int32),
	"[]int64":     new([]int64),
	"[]float64":   new(float64),
	"[]float32":   new([]float32),
	"interface{}": new(interface{}),
}

type Location struct {
	Type        string         `json:"type" validate:"required"`
	Coordinates [2]json.Number `json:"coordinates" validate:"required"`
}

type Config struct {
	ModelName       string `json:"model_name" bson:"model_name"`
	ColumnName      string `json:"column_name" bson:"column_name"`
	Header          string `json:"header" bson:"header"`
	Type            string `json:"type" bson:"type"`
	Description     string `json:"description" bson:"description"`
	Tag             string `json:"tag" bson:"tag"`
	Status          string `json:"status" bson:"status"`
	Is_reference    bool   `json:"is_reference" bson:"is_reference"`
	Field           string `json:"field" bson:"field"`
	Collection_name string `json:"collection_name" bson:"collection_name"`
	Json_field      string `json:"json_field" bson:"json_field"`
}

type AggregateResult struct {
	ID     string   `bson:"_id"`
	Fields []Config `bson:"fields"`
}

func LoadDataModelFromDB(orgID string) map[string][]Config {
	var query = []bson.M{
		{
			"$match": bson.M{"status": "A"},
		},
		{
			"$group": bson.M{
				"_id": "$model_name",
				"fields": bson.M{
					"$push": bson.M{
						"model_name":  "$model_name",
						"column_name": "$column_name",
						"type":        "$type",
						"tag":         "$tag",
						"json_field":  "$json_field",
					},
				},
			},
		},
	}

	var result []AggregateResult
	cur, err := database.GetConnection(orgID).Collection("data_model").Aggregate(ctx, query)
	if err != nil {
		log.Errorf("Error aggregating data: %s", err.Error())
		return nil
	}
	defer cur.Close(ctx)

	if err = cur.All(ctx, &result); err != nil {
		log.Errorf("Error retrieving aggregation result: %s", err.Error())
		return nil
	}
	resultMap := make(map[string][]Config)
	for _, res := range result {
		resultMap[res.ID] = res.Fields
	}

	return resultMap
}
func loadModels(models map[string][]Config, key string) interface{} {
	// ! if Uncomment the following line  it created duplicate models
	// defer func() {
	// 	if r := recover(); r != nil {
	// 		// Handle the panic here. You can log the error, return a default value,
	// 		fmt.Println("Panic recovered:", r)
	// 		shared.BadRequest(r)
	// 		return
	// 	}
	// }()

	// Check if the dynamic type for the model already exists in TypeMap.
	if model, exists := TypeMap[key]; exists {
		return model
	}

	// Create struct fields for the dynamic struct.
	var dynamicStruct []reflect.StructField
	for _, field := range models[key] {
		// fmt.Println(TypeMap)
		fieldName := field.ColumnName

		fieldType := TypeMap[field.Type]
		// if len(field.Type) >= 2 {
		// 	if field.Type[:2] == "[]" {
		if len(field.Type) >= 2 && field.Type[:2] == "[]" {
			// Get the type name without the array brackets
			typeName := field.Type[2:]
			// Check if the type is already available in TypeMap.
			if _, exists := TypeMap[field.Type]; !exists {
				// Recursively load dependent models and their types. and append the struct to the TypeMap
				fieldType = NestedArrayStruct(models, typeName)
			}

		} else {
			// Check if the type is already available in TypeMap.
			if _, exists := TypeMap[field.Type]; !exists {
				// Recursively load dependent models and their types.
				fieldType = loadModels(models, field.Type)

			}
		}

		// Append the field definition to the dynamic struct.
		dynamicStruct = append(dynamicStruct,
			reflect.StructField{
				Name: fieldName,
				Type: reflect.TypeOf(fieldType),
				Tag:  reflect.StructTag(field.Tag),
			},
		)
	}

	// Create a struct type using the collected struct fields.
	obj := reflect.StructOf(dynamicStruct)
	objIns := reflect.New(obj).Interface()

	// Store the dynamically created struct type in TypeMap.
	TypeMap[key] = objIns
	// Return an instance of the dynamically created struct type.
	return objIns
}

func createDynamicTypes(data map[string][]Config) {
	for key := range data {
		// Load and create dynamic types for the models associated with the key.
		loadModels(data, key)
	}

}

func ServerInitstruct(orgID string) {

	data := LoadDataModelFromDB(orgID)
	// Create dynamic schema types based on the data model configuration.
	createDynamicTypes(data)

}

func CreateInstanceForCollection(collectionName string) (interface{}, map[string]string) {
	var validationErrors = make(map[string]string)

	exist := false
	for key := range TypeMap {
		if key == collectionName {
			exist = true
			break
		}
	}
	if !exist {
		validationErrors["error"] = fmt.Sprintf("INVALID COLLECTION NAME: %s", collectionName)
		return nil, validationErrors
	}
	objIns := reflect.New(reflect.TypeOf(TypeMap[collectionName])).Interface()
	// fmt.Println(validationErrors)
	// fmt.Println(TypeMap)

	return objIns, validationErrors
}
