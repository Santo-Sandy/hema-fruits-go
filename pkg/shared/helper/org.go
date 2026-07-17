package helper

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"go.mongodb.org/mongo-driver/bson"
	"kriyatec.com/pms-api/pkg/shared/database"
)

func GetOrg(c *fiber.Ctx) (Organization, bool) {
	orgId := c.Get("OrgId")
	if orgId == "shared" {
		return Organization{Id: "shared", Name: "Shared Context"}, true
	}
	origin := strings.ToLower(c.Get("Origin"))
	referer := strings.ToLower(c.Get("Referer"))
	var org Organization

	if (orgId == "" || orgId == "null") && (strings.Contains(origin, "onboarding") || strings.Contains(referer, "onboarding")) {
		return Organization{Id: "shared", Name: "Shared Context"}, true
	}

	// Handle _demo suffix — look up base org but keep _demo id for DB connection
	lookupId := orgId
	isDemo := strings.HasSuffix(orgId, "_demo")
	if isDemo {
		lookupId = strings.TrimSuffix(orgId, "_demo")
	}

	if orgId == "" || orgId == "null" {
		org = GetOrgIdFromDomainName(origin, true)
	} else {
		org = GetOrgIdFromDomainName(lookupId, false)
	}

	orgId = org.Id
	if orgId == "" {
		return Organization{}, false
	}

	// Restore _demo suffix for actual DB routing
	if isDemo {
		org.Id = orgId + "_demo"
	}

	if _, exists := OrgList[org.Id]; exists {
		return OrgList[org.Id], true
	}

	LoadOrgConfig()

	// For demo orgs, return with _demo id even if not in OrgList
	if isDemo {
		return org, true
	}

	if _, exists := OrgList[orgId]; !exists {
		return Organization{}, false
	}

	return OrgList[orgId], true
}

func GetOrgIdFromDomainName(host string, origin bool) Organization {

	// fmt.Println(host)

	host = strings.ReplaceAll(host, "www.", "")
	// http://admin.pms.com:4200

	domainParts := strings.Split(host, ".")
	if len(domainParts) < 3 {
		// return nil
	}

	domainName := domainParts[0]
	if strings.Index(domainParts[0], "//") > 0 {
		domainName = strings.Split(domainParts[0], "//")[1]
	}

	ctx := context.Background()
	var org Organization

	fmt.Println(domainName)
	// if domainName == "" || domainName == "192" || domainName == "10" {
	// 	domainName = "cerp"
	// }
	if origin {
		database.SharedDB.Collection("organization").FindOne(ctx, bson.M{"domain_name": domainName}).Decode(&org)
	} else {
		database.SharedDB.Collection("organization").FindOne(ctx, bson.M{"_id": domainName}).Decode(&org)
	}

	//database.SharedDB.Collection("organization").FindOne(ctx, bson.M{"domain_name": domainName}).Decode(&org)

	return org
}

func GetOrgIdFromHeader(c *fiber.Ctx) string {
	return c.Get("OrgId")
}

func LoadOrgConfig() {
	ctx := context.Background()
	cur, err := database.SharedDB.Collection("organization").Find(ctx, bson.D{})
	if err != nil {
		log.Errorf("Organization Configuration Error %s", err.Error())
		defer cur.Close(ctx)
		return
	}
	var result []Organization
	if err = cur.All(ctx, &result); err != nil {
		return
	}
	for _, o := range result {
		OrgList[o.Id] = o
	}

}
