package entities

import (
	"context"
	"regexp"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kriyatec.com/pms-api/pkg/shared"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

func GetDatasetHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := "dataset_config"
	db := database.GetConnection(org.Id)

	var documents []bson.M
	filter := bson.M{} // your query
	// include Reference_pipeline so we can extract filter placeholders
	projection := bson.M{"dataSetName": 1, "Reference_pipeline": 1, "_id": 0}

	opts := options.Find().SetProjection(projection).SetSort(bson.M{"dataSetName": 1})

	cursor, err := db.Collection(collectionName).Find(context.Background(), filter, opts)
	if err != nil {
		return shared.BadRequest("Failed to get datasets: " + err.Error())
	}
	defer cursor.Close(context.Background())

	// regex to find placeholders like {"ParamsName":"sale_id","parmsDataType":"string"}
	re := regexp.MustCompile(`\{"ParamsName"\s*:\s*"([^\"]+)"\s*,\s*"parmsDataType"\s*:\s*"([^\"]+)"\s*\}`)
	for cursor.Next(context.Background()) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return shared.BadRequest("Failed to decode document: " + err.Error())
		}

		dsName := ""
		if v, ok := doc["dataSetName"].(string); ok {
			dsName = v
		}

		filters := make([]bson.M, 0)
		if ref, ok := doc["Reference_pipeline"].(string); ok && ref != "" {
			matches := re.FindAllStringSubmatch(ref, -1)
			for _, m := range matches {
				if len(m) >= 3 {
					filters = append(filters, bson.M{"key": m[1], "type": m[2]})
				}
			}
		}

		documents = append(documents, bson.M{"dataSetName": dsName, "filters": filters})
	}

	return c.Status(fiber.StatusOK).JSON(documents)
}
func GetCollectionsHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	db := database.GetConnection(org.Id)
	if db == nil {
		return shared.BadRequest("Database connection failed")
	}

	collections, err := db.ListCollectionNames(context.Background(), bson.M{})
	if err != nil {
		return shared.BadRequest("Failed to get collections: " + err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(collections)
}

func GetCollectionFieldsHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collectionName")
	db := database.GetConnection(org.Id)

	// Get a sample document to extract field names
	var sampleDoc bson.M
	err := db.Collection(collectionName).FindOne(context.Background(), bson.M{}).Decode(&sampleDoc)
	if err != nil {
		return shared.BadRequest("Failed to get sample document: " + err.Error())
	}

	// Extract field names with sample values
	fields := make(map[string]interface{})
	for field, value := range sampleDoc {
		fields[field] = value
	}

	return c.Status(fiber.StatusOK).JSON(fields)
}

func AggregateHandler(c *fiber.Ctx) error {
	org, exists := helper.GetOrg(c)
	if !exists {
		return shared.BadRequest("Organization Id missing")
	}

	collectionName := c.Params("collectionName")
	db := database.GetConnection(org.Id)

	var requestBody struct {
		Pipeline []bson.M `json:"pipeline"`
	}

	if err := c.BodyParser(&requestBody); err != nil {
		return shared.BadRequest("Invalid request body: " + err.Error())
	}

	cursor, err := db.Collection(collectionName).Aggregate(context.Background(), requestBody.Pipeline)
	if err != nil {
		return shared.BadRequest("Aggregation failed: " + err.Error())
	}
	defer cursor.Close(context.Background())

	var results []bson.M
	if err := cursor.All(context.Background(), &results); err != nil {
		return shared.BadRequest("Failed to decode results: " + err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(results)
}
