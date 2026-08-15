package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/middleware"
	"hema-fruits-go/pkg/models"
)

// GetDocByIdHandler fetches a single document by string _id
func GetDocByIdHandler(c *fiber.Ctx) error {
	colName := c.Params("collectionName")
	id := c.Params("id")

	db := config.GetDB()
	var doc bson.M
	err := db.Collection(colName).FindOne(context.Background(), bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"status":  "error",
			"message": "Document not found",
		})
	}

	return c.Status(fiber.StatusOK).JSON(doc)
}

// PostDocHandler creates a new document dynamically
func PostDocHandler(c *fiber.Ctx) error {
	colName := c.Params("model_name")
	userToken := middleware.GetUserTokenValue(c)

	var doc bson.M
	if err := c.BodyParser(&doc); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if doc == nil {
		doc = bson.M{}
	}

	// Generate ID if missing
	if _, ok := doc["_id"]; !ok || doc["_id"] == "" {
		doc["_id"] = GenerateUniqueKey()
	}

	// Set audit fields
	doc["created_on"] = time.Now().UTC()
	doc["created_by"] = userToken.UserId

	if _, ok := doc["status"]; !ok {
		doc["status"] = "Active"
	}

	db := config.GetDB()
	_, err := db.Collection(colName).InsertOne(context.Background(), doc)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(doc)
}

// PutDocByIDHandlers updates a document dynamically
func PutDocByIDHandlers(c *fiber.Ctx) error {
	colName := c.Params("model_name")
	id := c.Params("id")
	userToken := middleware.GetUserTokenValue(c)

	var doc bson.M
	if err := c.BodyParser(&doc); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Remove immutable fields
	delete(doc, "_id")
	delete(doc, "created_on")
	delete(doc, "created_by")

	doc["updated_on"] = time.Now().UTC()
	doc["updated_by"] = userToken.UserId

	db := config.GetDB()
	_, err := db.Collection(colName).UpdateOne(
		context.Background(),
		bson.M{"_id": id},
		bson.M{"$set": doc},
	)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Return updated document
	var updated bson.M
	db.Collection(colName).FindOne(context.Background(), bson.M{"_id": id}).Decode(&updated)
	return c.Status(fiber.StatusOK).JSON(updated)
}

// DeleteById deletes a document by ID
func DeleteById(c *fiber.Ctx) error {
	colName := c.Params("collectionName")
	id := c.Params("id")

	db := config.GetDB()
	_, err := db.Collection(colName).DeleteOne(context.Background(), bson.M{"_id": id})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Document deleted successfully"})
}

// DeleteByAll clears a collection (normally restricted or stubbed safely)
func DeleteByAll(c *fiber.Ctx) error {
	colName := c.Params("collectionName")
	db := config.GetDB()

	// Only allow deleting temporary tables or logs in this manner for safety
	if colName == "temporary_user" {
		db.Collection(colName).DeleteMany(context.Background(), bson.M{})
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Collection cleared"})
	}

	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Action not allowed"})
}

// GetDocsHandler handles advanced dynamic queries and filters
func GetDocsHandler(c *fiber.Ctx) error {
	colName := c.Params("collectionName")

	var req models.PaginationRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Build MongoDB Query
	query := BuildFilterQuery(req.Filter)

	// Inject MultiFieldSearchFilter if present
	if len(req.MultiFieldSearchFilter) > 0 {
		var orConditions []bson.M
		for _, f := range req.MultiFieldSearchFilter {
			if f.Operator == "CONTAINS" {
				prefix, ok := f.Value.(string)
				if ok {
					orConditions = append(orConditions, bson.M{
						f.Column: bson.M{
							"$regex":   "^" + prefix,
							"$options": "i",
						},
					})
				}
			}
		}
		if len(orConditions) > 0 {
			if len(query) > 0 {
				query = bson.M{"$and": []bson.M{query, {"$or": orConditions}}}
			} else {
				query = bson.M{"$or": orConditions}
			}
		}
	}

	// Default Active filter unless specified
	if _, ok := query["status"]; !ok {
		query["status"] = "Active"
	}

	// Pagination parameters
	findOptions := options.Find()
	if req.End > req.Start {
		limit := int64(req.End - req.Start)
		findOptions.SetLimit(limit)
	} else {
		findOptions.SetLimit(200) // Default limit
	}
	findOptions.SetSkip(int64(req.Start))

	// Sorting
	sortOpts := bson.D{}
	if len(req.Sort) > 0 {
		for _, s := range req.Sort {
			dir := 1
			if strings.ToUpper(s.Sort) == "DESC" {
				dir = -1
			}
			sortOpts = append(sortOpts, bson.E{Key: s.ColID, Value: dir})
		}
		findOptions.SetSort(sortOpts)
	} else {
		// Default sort by created_on descending
		findOptions.SetSort(bson.D{{Key: "created_on", Value: -1}})
	}

	db := config.GetDB()
	cursor, err := db.Collection(colName).Find(context.Background(), query, findOptions)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer cursor.Close(context.Background())

	var results []bson.M = []bson.M{}
	if err := cursor.All(context.Background(), &results); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// Wrapper format that matches original shared.SuccessResponse
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": 200,
		"data":   results,
	})
}

// BuildFilterQuery builds a MongoDB match filter from nested client FilterConditions
func BuildFilterQuery(filters []models.FilterCondition) bson.M {
	if len(filters) == 0 {
		return bson.M{}
	}

	var matchConditions []bson.M
	for _, f := range filters {
		var conds []bson.M
		for _, c := range f.Conditions {
			if len(c.Conditions) > 0 {
				nested := BuildFilterQuery([]models.FilterCondition{{Clause: c.Clause, Conditions: c.Conditions}})
				conds = append(conds, nested)
				continue
			}

			col := c.Column
			op := c.Operator
			val := c.Value

			switch op {
			case "EQUALS":
				conds = append(conds, bson.M{col: val})
			case "NOTEQUAL":
				conds = append(conds, bson.M{col: bson.M{"$ne": val}})
			case "CONTAINS":
				conds = append(conds, bson.M{col: bson.M{"$regex": val, "$options": "i"}})
			case "STARTSWITH":
				conds = append(conds, bson.M{col: bson.M{"$regex": "^" + fmt.Sprintf("%v", val), "$options": "i"}})
			case "LESSTHAN":
				conds = append(conds, bson.M{col: bson.M{"$lt": val}})
			case "GREATERTHAN":
				conds = append(conds, bson.M{col: bson.M{"$gt": val}})
			case "LESSTHANOREQUAL":
				conds = append(conds, bson.M{col: bson.M{"$lte": val}})
			case "GREATERTHANOREQUAL":
				conds = append(conds, bson.M{col: bson.M{"$gte": val}})
			case "IN":
				conds = append(conds, bson.M{col: bson.M{"$in": val}})
			case "EXISTS":
				conds = append(conds, bson.M{col: bson.M{"$exists": val}})
			}
		}

		if len(conds) > 0 {
			if f.Clause == "OR" {
				matchConditions = append(matchConditions, bson.M{"$or": conds})
			} else {
				matchConditions = append(matchConditions, bson.M{"$and": conds})
			}
		}
	}

	if len(matchConditions) == 0 {
		return bson.M{}
	}
	if len(matchConditions) == 1 {
		return matchConditions[0]
	}
	return bson.M{"$and": matchConditions}
}
