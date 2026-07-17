package utils

// var OrgList = make(map[string]Organization)

// type Organization struct {
// 	Id        string      `json:"_id" bson:"_id"`
// 	Name      string      `json:"name" bson:"name"`
// 	Type      string      `json:"type" bson:"type"`
// 	SubDomain string      `json:"sub_domain" bson:"sub_domain"`
// 	Style     interface{} `json:"style" bson:"style"`
// 	Logo      string      `json:"logo" bson:"logo"`
// 	Group     string      `json:"group" bson:"group"`
// 	AppName   string      `json:"app_name" bson:"app_name"`
// 	LocOption bool        `json:"loc" bson:"loc"`
// }

type UserToken struct {
	UserId   string `json:"user_id" bson:"user_id"`
	UserRole string `json:"user_role" bson:"user_role"`
	// UserType string  `json:"user_type" bson:"user_type"`
	OrgId     string `json:"uo_id" bson:"uo_id"`
	OrgType   string `json:"uo_type" bson:"uo_type"`
	FactoryId string `json:"factory_id" bson:"factory_id"`
}

type EmailTemplete struct {
	Title        string `json:"title" bson:"title"`
	EmailType    string `json:"emailtype" bson:"emailtype"`
	BodyTemplate string `json:"body_template" bson:"body_template"`
	Link         string `json:"link" bson:"link"`
}
