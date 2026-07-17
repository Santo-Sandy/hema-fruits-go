package einvoice

import (
	"strings"
	"time"
)

// AuthRequest represents the authentication request structure
type AuthRequest struct {
	Email        string `json:"email"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	IPAddress    string `json:"ip_address"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GSTIN        string `json:"gstin"`
}

// AuthData contains the authentication token and related data
type AuthData struct {
	UserName    string `json:"UserName"`
	TokenExpiry string `json:"TokenExpiry"`
	Sek         string `json:"Sek"`
	ClientID    string `json:"ClientId"`
	AuthToken   string `json:"AuthToken"`
}

// AuthHeader contains the request header information
type AuthHeader struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	IPAddress    string `json:"ip_address"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GSTIN        string `json:"gstin"`
	CacheControl string `json:"cache-control"`
	PostmanToken string `json:"postman-token"`
	GSTUsername  string `json:"gst_username"`
	Txn          string `json:"txn"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	IRP        string     `json:"irp"`
	Data       AuthData   `json:"data"`
	StatusCode string     `json:"status_cd"`
	StatusDesc string     `json:"status_desc"`
	Header     AuthHeader `json:"header"`
}

// GenerateIRNRequest represents the IRN generation request
type GenerateIRNRequest struct {
	Version    string     `json:"Version"`
	TranDtls   TranDtls   `json:"TranDtls"`
	DocDtls    DocDtls    `json:"DocDtls"`
	SellerDtls SellerDtls `json:"SellerDtls"`
	BuyerDtls  BuyerDtls  `json:"BuyerDtls"`
	ItemList   []Item     `json:"ItemList"`
	ValDtls    ValDtls    `json:"ValDtls"`
}

// TranDtls contains mandatory transaction details
type TranDtls struct {
	TaxSch string `json:"TaxSch"`
	SupTyp string `json:"SupTyp"`
}

// DocDtls contains mandatory document details
type DocDtls struct {
	Typ string `json:"Typ"`
	No  string `json:"No"`
	Dt  string `json:"Dt"`
}

// SellerDtls contains mandatory seller details
type SellerDtls struct {
	Gstin string      `json:"Gstin"`
	LglNm string      `json:"LglNm"`
	Addr1 string      `json:"Addr1"`
	Loc   string      `json:"Loc"`
	Pin   interface{} `json:"Pin"`
	Stcd  string      `json:"Stcd"`
}

// BuyerDtls contains mandatory buyer details
type BuyerDtls struct {
	Gstin string      `json:"Gstin"`
	LglNm string      `json:"LglNm"`
	Pos   string      `json:"Pos"`
	Addr1 string      `json:"Addr1"`
	Loc   string      `json:"Loc"`
	Stcd  string      `json:"Stcd"`
	Pin   interface{} `json:"Pin"`
}

// Item represents a single mandatory item in ItemList
type Item struct {
	SlNo       string      `json:"SlNo"`
	IsServc    string      `json:"IsServc"`
	HsnCd      string      `json:"HsnCd"`
	UnitPrice  interface{} `json:"UnitPrice"`
	TotAmt     interface{} `json:"TotAmt"`
	AssAmt     interface{} `json:"AssAmt"`
	GstRt      interface{} `json:"GstRt"`
	TotItemVal interface{} `json:"TotItemVal"`
	PrdDesc    string      `json:"PrdDesc"`
	Qty        interface{} `json:"Qty"`
	Unit       string      `json:"Unit"`
	IgstAmt    interface{} `json:"IgstAmt"`
	CgstAmt    interface{} `json:"CgstAmt"`
	SgstAmt    interface{} `json:"SgstAmt"`
}

// ValDtls contains mandatory total/aggregate values
type ValDtls struct {
	AssVal    interface{} `json:"AssVal"`
	CgstVal   interface{} `json:"CgstVal"`
	SgstVal   interface{} `json:"SgstVal"`
	IgstVal   interface{} `json:"IgstVal"`
	TotInvVal interface{} `json:"TotInvVal"`
}

// GenerateIRNResponse represents the IRN generation response
type GenerateIRNResponse struct {
	Data         GenerateIRNData        `json:"data"`
	Iss          string                 `json:"iss"`
	StatusCode   string                 `json:"status_cd"`
	StatusDesc   string                 `json:"status_desc"`
	Header       map[string]interface{} `json:"header"`
	ErrorDetails []ErrorDetail          `json:"error_details,omitempty"`
}

// GenerateIRNData contains the generated IRN/QR code data
type GenerateIRNData struct {
	AckNo         int64  `json:"AckNo"`
	AckDt         string `json:"AckDt"`
	Irn           string `json:"Irn"`
	SignedInvoice string `json:"SignedInvoice"`
	SignedQRCode  string `json:"SignedQRCode"`
	Status        string `json:"Status"`
	EwbNo         int64  `json:"EwbNo"`
	EwbDt         string `json:"EwbDt"`
	EwbValidTill  string `json:"EwbValidTill"`
	Remarks       string `json:"Remarks"`
	// Legacy fields for backward compatibility
	GstnNumber        string `json:"gstnNumber"`
	SgstAmount        string `json:"sgstAmount"`
	CessAmount        string `json:"cessAmount"`
	CgstAmount        string `json:"cgstAmount"`
	InvoiceNumber     string `json:"invoceNumber"`
	IgstAmount        string `json:"igstAmount"`
	BankIFSCCode      string `json:"bankIFSCCode"`
	DynamicQrCode     string `json:"dynamicQrCode"`
	BankAccountNo     string `json:"bankAccountNo"`
	InvoiceDate       string `json:"invoceDate"`
	TotalInvoiceValue string `json:"totalInvoceValue"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	ErrorSource  string `json:"error_source,omitempty"`
}

// Config holds the e-invoice API configuration
type Config struct {
	AuthEndpoint           string
	GenerateIRNEndpoint    string
	CancelEndpoint         string
	GSTNDetailsEndpoint    string
	QRCodeEndpoint         string
	EwayBillEndpoint       string // IRN-based e-way bill generation
	
	// Standalone e-way bill endpoints (used for both IRN-based and standalone)
	EwayBillAuthEndpoint     string
	EwayBillGenerateEndpoint string
	EwayBillCancelEndpoint   string
	
	Email                  string
	Username               string
	Password               string
	IPAddress              string
	ClientID               string
	ClientSecret           string
	GSTIN                  string
}

// Session holds the authenticated session data
type Session struct {
	AuthToken   string
	Sek         string
	ClientID    string
	TokenExpiry time.Time
}

// CancelIRNRequest represents an IRN cancel request
type CancelIRNRequest struct {
	Irn       string `json:"Irn"`
	CnlRsn    string `json:"CnlRsn"`
	CnlRem    string `json:"CnlRem"`
	SaleId    string `json:"sale_id"`
	FactoryId string `json:"factory_id"`
}

// CancelIRNResponse represents the response from cancel IRN API
type CancelIRNResponse struct {
	Irp  string `json:"irp"`
	Data struct {
		Irn        string `json:"Irn"`
		CancelDate string `json:"CancelDate"`
	} `json:"data"`
	StatusCode string `json:"status_cd"`
	StatusDesc string `json:"status_desc"`
}

// GenerateEwayBillRequest represents the e-way bill generation request
type GenerateEwayBillRequest struct {
	Irn        string `json:"Irn"`
	Distance   int    `json:"Distance"`
	TransMode  string `json:"TransMode"`
	TransId    string `json:"TransId,omitempty"` // Optional - omit if empty
	TransDocDt string `json:"TransDocDt"`
	TransDocNo string `json:"TransDocNo"`
	VehNo      string `json:"VehNo,omitempty"` // Optional - omit if empty
	VehType    string `json:"VehType"`
}

// GenerateEwayBillResponse represents the e-way bill generation response
type GenerateEwayBillResponse struct {
	Irp  string `json:"irp"`
	Data struct {
		EwbNo        int64  `json:"EwbNo"`
		EwbDt        string `json:"EwbDt"`
		EwbValidTill string `json:"EwbValidTill"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		ErrorCd string `json:"error_cd"`
		Info    string `json:"info"`
	} `json:"error,omitempty"`
	StatusCode string `json:"status_cd"`
	StatusDesc string `json:"status_desc"`
}

// CancelEwayBillRequest represents the e-way bill cancellation request
type CancelEwayBillRequest struct {
	EwbNo     int64  `json:"ewbNo"`
	CancelRsn int    `json:"cancelRsnCode"` // Changed to int - API expects numeric reason code
	CancelRem string `json:"cancelRmrk"`
	SaleId    string `json:"sale_id"`
	FactoryId string `json:"factory_id"`
}

// CancelEwayBillResponse represents the e-way bill cancellation response
type CancelEwayBillResponse struct {
	Irp  string `json:"irp"`
	Data struct {
		EwbNo      int64  `json:"ewbNo"`
		CancelDate string `json:"cancelDate"`
	} `json:"data"`
	StatusCode string `json:"status_cd"`
	StatusDesc string `json:"status_desc"`
}

// isSuccessStatus normalizes various success indicators used by different APIs.
func isSuccessStatus(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "success" || s == "sucess" || s == "1"
}

// GSTNDetailsResponse represents the response from GSTN details API
type GSTNDetailsResponse struct {
	IRP          string          `json:"irp"`
	Data         GSTNDetailsData `json:"data"`
	StatusCode   string          `json:"status_cd"`
	StatusDesc   string          `json:"status_desc"`
	ErrorDetails []ErrorDetail   `json:"error_details,omitempty"`
}

// GSTNDetailsData contains the GSTN business details
type GSTNDetailsData struct {
	Gstin     string      `json:"Gstin"`
	LegalName string      `json:"LegalName"`
	TradeName interface{} `json:"TradeName"` // Can be null or string
	AddrBnm   interface{} `json:"AddrBnm"`   // Building Name (can be null)
	AddrBno   interface{} `json:"AddrBno"`   // Building Number (can be null)
	AddrFlno  interface{} `json:"AddrFlno"`  // Floor Number (can be null)
	AddrSt    interface{} `json:"AddrSt"`    // Street (can be null)
	AddrLoc   interface{} `json:"AddrLoc"`   // Location (can be null)
	StateCode int         `json:"StateCode"` // State code as integer
	AddrPncd  interface{} `json:"AddrPncd"`  // Pincode (can be string or number)
	TxpType   string      `json:"TxpType"`   // Taxpayer Type
	Status    string      `json:"Status"`    // Active/Inactive
	BlkStatus string      `json:"BlkStatus"` // Block Status
	DtReg     interface{} `json:"DtReg"`     // Registration Date (can be null)
	DtDReg    interface{} `json:"DtDReg"`    // De-registration Date (can be null)
	CancelDt  string      `json:"cancelDt"`  // Cancellation Date
}

// StandaloneEwayBillRequest represents standalone e-way bill generation request (without IRN)
type StandaloneEwayBillRequest struct {
	SupplyType       string  `json:"supplyType"`      // "O" for Outward, "I" for Inward
	SubSupplyType    string  `json:"subSupplyType"`   // "1" for Supply, "2" for Import, etc.
	SubSupplyDesc    string  `json:"subSupplyDesc,omitempty"`
	DocType          string  `json:"docType"`         // "INV" for Invoice, "CHL" for Challan, etc.
	DocNo            string  `json:"docNo"`
	DocDate          string  `json:"docDate"`         // DD/MM/YYYY
	TransactionType  int     `json:"transactionType"` // 1 for Regular, 2 for Bill To-Ship To, 3 for Bill From-Dispatch From, 4 for Combination of 2 and 3
	FromGstin        string  `json:"fromGstin"`
	FromTrdName      string  `json:"fromTrdName"`
	FromAddr1        string  `json:"fromAddr1"`
	FromAddr2        string  `json:"fromAddr2,omitempty"`
	FromPlace        string  `json:"fromPlace"`
	FromPincode      int     `json:"fromPincode"`
	FromStateCode    int     `json:"fromStateCode"`
	ActFromStateCode int     `json:"actFromStateCode"`
	ToGstin          string  `json:"toGstin"`
	ToTrdName        string  `json:"toTrdName"`
	ToAddr1          string  `json:"toAddr1"`
	ToAddr2          string  `json:"toAddr2,omitempty"`
	ToPlace          string  `json:"toPlace"`
	ToPincode        int     `json:"toPincode"`
	ToStateCode      int     `json:"toStateCode"`
	ActToStateCode   int     `json:"actToStateCode"`
	TotalValue       float64 `json:"totalValue"`
	CgstValue        float64 `json:"cgstValue"`
	SgstValue        float64 `json:"sgstValue"`
	IgstValue        float64 `json:"igstValue"`
	CessValue        float64 `json:"cessValue"`
	TotInvValue      float64 `json:"totInvValue"`
	TransporterId    string  `json:"transporterId,omitempty"`
	TransporterName  string  `json:"transporterName,omitempty"`
	TransDocNo       string  `json:"transDocNo,omitempty"`
	TransMode        string  `json:"transMode"`       // "1" for Road, "2" for Rail, etc.
	TransDistance    string  `json:"transDistance"`   // Distance as string
	TransDocDate     string  `json:"transDocDate,omitempty"` // DD/MM/YYYY
	VehicleNo        string  `json:"vehicleNo,omitempty"`
	VehicleType      string  `json:"vehicleType"`     // "R" for Regular, "O" for Over Dimensional Cargo
	ItemList         []StandaloneEwayBillItem `json:"itemList"`
}

// StandaloneEwayBillItem represents an item in standalone e-way bill
type StandaloneEwayBillItem struct {
	ProductName  string  `json:"productName"`
	ProductDesc  string  `json:"productDesc,omitempty"`
	HsnCode      string  `json:"hsnCode"`
	Quantity     float64 `json:"quantity"`
	QtyUnit      string  `json:"qtyUnit"`
	CgstRate     float64 `json:"cgstRate,omitempty"`
	SgstRate     float64 `json:"sgstRate,omitempty"`
	IgstRate     float64 `json:"igstRate,omitempty"`
	CessRate     float64 `json:"cessRate,omitempty"`
	CessAdvol    float64 `json:"cessAdvol,omitempty"`
	TaxableAmount float64 `json:"taxableAmount"`
}

// StandaloneEwayBillResponse represents standalone e-way bill generation response
type StandaloneEwayBillResponse struct {
	Data struct {
		EwbNo        int64  `json:"ewayBillNo"`
		EwbDt        string `json:"ewayBillDate"`
		EwbValidTill string `json:"validUpto"`
		Alert        string `json:"alert,omitempty"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		ErrorCd string `json:"error_cd"`
		Info    string `json:"info"`
	} `json:"error,omitempty"`
	StatusCode string `json:"status_cd"`
	StatusDesc string `json:"status_desc"`
}
