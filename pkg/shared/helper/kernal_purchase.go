package helper

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

// ✅ ParseSerialRange — supports both "-" and "/" ranges
func ParseSerialRange(input string) ([]int, error) {
	var result []int
	parts := strings.Split(input, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Accept both "-" and "/" as range separators
		separator := ""
		if strings.Contains(part, "-") {
			separator = "-"
		} else if strings.Contains(part, "/") {
			separator = "/"
		}

		if separator != "" {
			rangeParts := strings.Split(part, separator)
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}

			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil || start > end {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			for i := start; i <= end; i++ {
				result = append(result, i)
			}
		} else {
			num, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid number: %s", part)
			}
			result = append(result, num)
		}
	}

	return result, nil
}

func FormatSerialRange(serialString string) ([]int, error) {
	if serialString == "" {
		return []int{}, nil
	}

	var numbers []int
	parts := strings.Split(serialString, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		separator := ""
		if strings.Contains(part, "-") {
			separator = "-"
		} else if strings.Contains(part, "/") {
			separator = "/"
		}

		if separator != "" {
			rangeParts := strings.Split(part, separator)
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}

			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil || start > end {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			for i := start; i <= end; i++ {
				numbers = append(numbers, i)
			}
		} else {
			num, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid number: %s", err)
			}
			numbers = append(numbers, num)
		}
	}

	if len(numbers) == 0 {
		return []int{}, nil
	}

	sort.Ints(numbers)
	uniqueNumbers := make([]int, 0, len(numbers))
	uniqueNumbers = append(uniqueNumbers, numbers[0])
	for i := 1; i < len(numbers); i++ {
		if numbers[i] != numbers[i-1] {
			uniqueNumbers = append(uniqueNumbers, numbers[i])
		}
	}
	return uniqueNumbers, nil
}
// GenerateQuantities returns a slice of ints from 1 to (total/per)
func GenerateQuantities(total, per int) []int {
    if per == 0 {
        return []int{} // avoid divide-by-zero
    }

    limit := total / per
    quantities := make([]int, limit)

    for i := 1; i <= limit; i++ {
        quantities[i-1] = i
    }

    return quantities
}

func InventoryCreation(kernalData map[string]interface{}, KernalInventoryId string, userToken utils.UserToken, col *mongo.Collection) error {
	ctx := context.Background()

	serialStr, _ := kernalData["serial_number"].(string)
	containsSerialNo, _ := kernalData["contains_serial_no"].(bool)

	packingType := strings.ToLower(ToString(kernalData["product_packing_type"]))
fmt.Println("packingType =", packingType)
fmt.Printf("%#v\n", kernalData["product_packing_type"])
	var inventoryList []interface{}


	if !containsSerialNo && packingType != "bag" {


		quantity := ToInt(kernalData["quantity"])
		itemCapacity := ToInt(kernalData["item_capacity"])

		qtyList := GenerateQuantities(quantity, itemCapacity) 

		for _, qty := range qtyList {
			item := map[string]interface{}{
				"_id":                uuid.New().String(),
				"s_no":               qty,
				"purchase_id":        kernalData["purchase_template_id"],
				"product_id":         kernalData["product_id"],
				"quantity":           itemCapacity,
				"kernal_purchase_id": KernalInventoryId,
				"stock_from":         "purchase",
				"origin_id":          ToString(kernalData["origin_id"]),
				"warehouse_id":       ToString(kernalData["warehouse_id"]),
				"status":             "packed",
			}
			inventoryList = append(inventoryList, item)
		}

		_, err := col.InsertMany(ctx, inventoryList)
		if err != nil {
			return fmt.Errorf("failed to insert inventory records: %v", err)
		}

		return nil
	}

	// ===================================================
	// CASE 2: SERIAL NUMBER PRESENT
	// ===================================================
	serials, err := ParseSerialRange(serialStr)
	if err != nil {
		return fmt.Errorf("invalid serial range: %v", err)
	}

	itemCapacity := ToInt(kernalData["item_capacity"])

	for _, serial := range serials {

		item := map[string]interface{}{
			"_id":                uuid.New().String(),
			"s_no":               serial,
			"purchase_id":        kernalData["purchase_template_id"],
			"product_id":         kernalData["product_id"],
			"quantity":           itemCapacity,
			"kernal_purchase_id": KernalInventoryId,
			"stock_from":         "purchase",
			"origin_id":          ToString(kernalData["origin_id"]),
			"warehouse_id":       ToString(kernalData["warehouse_id"]),
			"status":             "packed",
		}

		inventoryList = append(inventoryList, item)
	}

	_, err = col.InsertMany(ctx, inventoryList)
	if err != nil {
		return fmt.Errorf("failed to insert inventory records: %v", err)
	}

	return nil
}


func DeleteById(db *mongo.Database, collectionName string, filter interface{}) error {
	ctx := context.Background()

	switch collectionName {

	case "purchase":
		// delete related kernal purchase data
		if _, err := db.Collection("kernal_purchase_data").DeleteMany(ctx, filter); err != nil {
			return err
		}
		// delete related kernal inventory
		if _, err := db.Collection("kernal_inventory").DeleteMany(ctx, filter); err != nil {
			return err
		}

	case "kernal_purchase_data":
		// delete related inventory only
		if _, err := db.Collection("kernal_inventory").DeleteMany(ctx, filter); err != nil {
			return err
		}
	}

	return nil
}

func DeleteByCollAndFilter(coll *mongo.Collection, filter interface{}) error {
	ctx := context.Background()
	_, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return err
	}
	return nil
}
