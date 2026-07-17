package fcm

type FCMRegisterRequest struct {
	Platform    string `json:"platform"` // "web" | "android" | "ios"
	AppID       string `json:"app_id,omitempty"`
	IMEI        string `json:"imei,omitempty"`
	DeviceModel string `json:"device_model,omitempty"`
	FCMToken    string `json:"fcm_token"`
}
