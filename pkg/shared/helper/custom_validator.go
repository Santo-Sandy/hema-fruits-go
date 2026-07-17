package helper

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)




//TODO DATE IS LESS THAN TODAY
func DateLessThan(fl validator.FieldLevel) bool {
	switch v := fl.Field().Interface().(type) {
	case time.Time:
		return v.Before(time.Now())
	default:
		return false
	}
}

//TODO DATE IS GREATER THAN TODAY
func DateGreaterThan(fl validator.FieldLevel) bool {
	switch v := fl.Field().Interface().(type) {
	case time.Time:
		return v.After(time.Now())
	default:
		return false
	}
}

//TODO DATE IS LESS THAN NOW
func DateTimeLessThanNow(fl validator.FieldLevel) bool {
	switch v := fl.Field().Interface().(type) {
	case time.Time:
		return v.Before(time.Now())
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return false
		}
		return t.Before(time.Now()) && t.Hour() < 24 && t.Minute() < 60 && t.Second() < 60
	default:
		return false
	}
}

//TODO DATE IS GREATER THAN NOW
func DateTimeGreaterThanNow(fl validator.FieldLevel) bool {
	switch v := fl.Field().Interface().(type) {
	case time.Time:
		return v.After(time.Now())
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return false
		}
		return t.After(time.Now()) && t.Hour() > 24 && t.Minute() > 60 && t.Second() < 60
	default:
		return false
	}
}

//TODO DATE IS LESS TODAY PASSING IN PARMS
func IsDateLess(dateStr string) bool {
	today := time.Now()

	date, err := time.Parse("2006-01-02T15:04:05.000Z", dateStr)
	if err == nil && date.Before(today) {
		return true
	}

	return false
}

//TODO DATE IS GREATER THAN PASSING IN PARMS
func IsDateGreater(dateStr string) bool {
	today := time.Now()

	date, err := time.Parse("2006-01-02T15:04:05.000Z", dateStr)
	if err == nil && date.After(today) {
		return true
	}

	return false
}

//TODO BETWEEN DAYS PARMS
func BetweenDays(startTimes, endTimes string) bool {
	checkDateStr := time.Now()
	startTime, err := time.Parse(time.RFC3339, startTimes)
	if err != nil {
		// Handle invalid start time format
		return false
	}

	endTime, err := time.Parse(time.RFC3339, endTimes)
	if err != nil {
		// Handle invalid end time format
		return false
	}

	if checkDateStr.After(startTime) && checkDateStr.Before(endTime) {
		// Date is between the startdate and enddate, return true
		return true
	}
	// Check if checkDate is within the range defined by startTime and endTime
	return false
}

//TODO CHECK FIELD IN COLLECTION
// Function to check if a specific field exists in the collection
func IsFieldInCollection(collection interface{}, fieldKey string) bool {
	collectionValue := reflect.ValueOf(collection)

	if collectionValue.Kind() != reflect.Struct {
		return false // The collection is not a struct
	}

	fieldValue := collectionValue.FieldByName(fieldKey)

	return fieldValue.IsValid()
}

//TODO CUSTOM ERR FOR DATA TYPE AND JSON
func GetErrorMessage(err validator.FieldError) string {
	fieldName := err.Field()
	switch err.Tag() {
	case "required":
		return fieldName + " is required."
	case "min":
		return fieldName + " should be greater than or equal to " + err.Param()
	case "max":
		return fieldName + " should be less than or equal to " + err.Param()
	case "email":
		fieldValue := err.Value().(string)
		emailPattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
		if !regexp.MustCompile(emailPattern).MatchString(fieldValue) {
			// *Check which part of the email is missing
			if !strings.Contains(fieldValue, "@") {
				return fmt.Sprintf("%s should be a valid email address. Missing '@' in the email.", fieldName)
			} else if !strings.Contains(fieldValue, ".") {
				return fmt.Sprintf("%s should be a valid email address. Missing '.' in the domain.", fieldName)
			} else if strings.HasPrefix(fieldValue, ".") || strings.HasSuffix(fieldValue, ".") {
				return fmt.Sprintf("%s should be a valid email address. Invalid placement of '.' in the email.", fieldName)
			} else {
				return fmt.Sprintf("%s should be a valid email address. Invalid email format.", fieldName)
			}
		}
	case "numeric":
		return fieldName + " should be a numeric value."
	case "time":
		return fieldName + " should be in a valid time format (YYYY-MM-DDTHH:MM:SS)."
	case "string":
		return fieldName + " should be a string."
	default:
		return fieldName + " is invalid."
	}
	return ""
}
