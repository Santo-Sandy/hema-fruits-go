package entities

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"kriyatec.com/pms-api/pkg/shared/database"
	"kriyatec.com/pms-api/pkg/shared/helper"
)

func GetServiceProviderDetails(orgId string, jobworkId string) ([]bson.M, error) {
	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", jobworkId}}}},
		bson.D{
			{"$lookup",
				bson.D{
					{"from", "customer"},
					{"localField", "service_provider"},
					{"foreignField", "_id"},
					{"as", "service_provider_result"},
				},
			},
		},
		bson.D{
			{"$unwind",
				bson.D{
					{"path", "$service_provider_result"},
					{"preserveNullAndEmptyArrays", true},
				},
			},
		},
		bson.D{
			{"$set",
				bson.D{
					{"is_login_available",
						bson.D{
							{"$ifNull",
								bson.A{
									"$service_provider_result.is_login_available",
									false,
								},
							},
						},
					},
					{"service_provider_org_id",
						bson.D{
							{"$ifNull",
								bson.A{
									"$service_provider_result.org_id",
									"",
								},
							},
						},
					},
				},
			},
		},
		bson.D{
			{
				"$unset", bson.A{"service_provider_result"},
			},
		},
	}

	result, err := helper.GetAggregateQueryResult(orgId, "job_work", pipeline)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		log.Printf("[WARN] GetServiceProviderDetails: No service provider found for jobwork %s in org %s", jobworkId, orgId)
	} else {
		log.Printf("[SUCCESS] GetServiceProviderDetails: Found service provider for jobwork %s in org %s", jobworkId, orgId)
	}

	return result, nil
}

func GetCustomerByOrgId(destOrgId string, sourceOrgId string) string {
	pipeline := bson.A{
		bson.D{{"$match", bson.D{{"_id", sourceOrgId}}}},
	}

	customerDetails, err := helper.GetAggregateQueryResult(destOrgId, "customer", pipeline)
	if err != nil {
		return ""
	}

	if len(customerDetails) == 0 {
		return ""
	}

	customerId := helper.ToString(customerDetails[0]["_id"])
	log.Printf("[SUCCESS] GetCustomerByOrgId: Found customer %s for org_id %s in dest org %s", customerId, sourceOrgId, destOrgId)
	return customerId
}

func UpsertCustomerToDestOrg(destOrgId string, sourceOrgId string, serviceProviderCustomerId string) (string, error) {

	sourceCustomerData, err := GetCustomerByOrgID(sourceOrgId, sourceOrgId)
	constructedCustomerData := make(map[string]interface{})
	constructedCustomerData["_id"] = sourceCustomerData["_id"]
	constructedCustomerData["org_id"] = sourceCustomerData["_id"]
	constructedCustomerData["customer_name"] = sourceCustomerData["name"]
	constructedCustomerData["primary_contact_number"] = sourceCustomerData["mobile_number"]
	constructedCustomerData["primary_contact_email"] = sourceCustomerData["org_email_id"]
	sourceCustomerData = constructedCustomerData
	if err != nil {
		return "", fmt.Errorf("failed to fetch source customer data: %w", err)
	}

	existingCustomerId := GetCustomerByOrgId(destOrgId, sourceOrgId)

	if existingCustomerId != "" {

		filter := bson.M{"_id": existingCustomerId}
		update := bson.M{"$set": sourceCustomerData}

		_, err := database.GetConnection(destOrgId).Collection("customer").UpdateOne(context.Background(), filter, update)
		if err != nil {
			return "", fmt.Errorf("failed to update customer: %w", err)
		}

		log.Printf("[SUCCESS] UpsertCustomerToDestOrg: Updated customer %s in dest org %s", existingCustomerId, destOrgId)
		return existingCustomerId, nil
	}

	sourceCustomerData["created_at"] = primitive.NewDateTimeFromTime(time.Now())

	_, err = database.GetConnection(destOrgId).Collection("customer").InsertOne(context.Background(), sourceCustomerData)
	if err != nil {
		return "", fmt.Errorf("failed to insert customer: %w", err)
	}

	log.Printf("[SUCCESS] UpsertCustomerToDestOrg: Inserted new customer %s to dest org %s from source org %s", sourceCustomerData["_id"], destOrgId, sourceOrgId)
	return sourceCustomerData["_id"].(string), nil
}

func InsertJobWorkProcessToDestOrg(destOrgId string, sourceOrgId string, jobWorkData map[string]interface{}, inputData map[string]interface{}) error {
	serviceProviderCustomerId := helper.ToString(jobWorkData["service_provider"])

	customerAId, err := UpsertCustomerToDestOrg(destOrgId, sourceOrgId, serviceProviderCustomerId)
	if err != nil {
		customerAId = ""
	}

	jobWorkData["type"] = "inWard-jobWork"
	jobWorkData["service_provider"] = customerAId
	jobWorkData["source_org_id"] = sourceOrgId

	_, err = database.GetConnection(destOrgId).Collection("job_work_process").InsertOne(context.Background(), jobWorkData)
	if err != nil {
		return fmt.Errorf("failed to insert job_work_process: %w", err)
	}

	_, err = database.GetConnection(destOrgId).Collection("jobwork_process_details").InsertOne(context.Background(), inputData)
	if err != nil {
		return fmt.Errorf("failed to insert jobwork_process_details: %w", err)
	}

	log.Printf("[SUCCESS] InsertJobWorkProcessToDestOrg: Completed insertion from source org %s to dest org %s", sourceOrgId, destOrgId)
	return nil
}

func ProcessCrossOrgJobWorkStock(inputData map[string]interface{}, sourceOrgId string, destOrgId string, userId string, insertedId string) error {
	jobworkID := helper.ToString(inputData["jobwork_id"])
	purchaseID := helper.ToString(inputData["purchase_id"])
	templateID := helper.ToString(inputData["template_id"])

	purchase, err := GetDataById(sourceOrgId, purchaseID, "purchase")
	if err != nil {
		log.Println("purchase data not found:", err)
		purchase = map[string]interface{}{}
	}

	jobwork, err := GetDataById(sourceOrgId, jobworkID, "job_work")
	if err != nil {
		log.Println("jobwork data not found:", err)
		jobwork = map[string]interface{}{}
	}

	jobworkTemplate, err := GetDataById(sourceOrgId, templateID, "jobwork_template")
	if err != nil {
		log.Println("jobwork template not found:", err)
		jobworkTemplate = map[string]interface{}{}
	}

	actionType := helper.ToString(jobwork["type"])
	switch actionType {
	case "outWard-jobWork", "inWard-jobWork":
		actionType = "outWard-jobWork"
	default:
		actionType = ""
	}

	err = ProcessJobWorkStockMovementCrossOrg(inputData, jobwork, jobworkTemplate, purchase, sourceOrgId, destOrgId, userId, actionType, insertedId, purchaseID)
	if err != nil {
		return err
	}

	log.Printf("[SUCCESS] ProcessCrossOrgJobWorkStock: Completed stock movement from %s to %s for jobwork %s", sourceOrgId, destOrgId, jobworkID)
	return nil
}

func CheckServiceProviderLogin(inputData map[string]interface{}, orgId string, userId string, insertedId string) (bool, error) {

	if inputData["jobwork_id"] == nil {
		return true, nil
	}
	jobworkId := inputData["jobwork_id"].(string)

	serviceProviderList, err := GetServiceProviderDetails(orgId, jobworkId)
	if err != nil {
		return true, nil
	}
	if len(serviceProviderList) == 0 {
		return true, nil
	}

	serviceProviderId, ok := serviceProviderList[0]["service_provider_org_id"].(string)
	if !ok || serviceProviderId == "" {
		return true, nil
	}

	isLoginAvailable, _ := serviceProviderList[0]["is_login_available"].(bool)
	if !isLoginAvailable {
		return true, nil
	}

	inputData["service_type"] = "process"

	err = InsertJobWorkProcessToDestOrg(serviceProviderId, orgId, serviceProviderList[0], inputData)
	if err != nil {
		return true, err
	}
	///call once approved
	err = ProcessCrossOrgJobWorkStock(inputData, orgId, serviceProviderId, userId, insertedId)
	if err != nil {
		return true, err
	}

	log.Printf("[SUCCESS] CheckServiceProviderLogin: Completed cross-org jobwork from %s to %s for jobwork %s", orgId, serviceProviderId, jobworkId)
	return false, nil
}
