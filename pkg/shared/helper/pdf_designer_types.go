package helper

import "time"

// PDFTemplate - Main Template Structure
type PDFTemplate struct {
	ID             string          `json:"_id" bson:"_id"`
	Name           string          `json:"name" bson:"name"`
	Description    string          `json:"description,omitempty" bson:"description,omitempty"`
	Widgets        []Widget        `json:"widgets" bson:"widgets"`
	PageSettings   PageSettings    `json:"pageSettings" bson:"pageSettings"`
	CommonDatasets []CommonDataset `json:"commonDatasets,omitempty" bson:"commonDatasets,omitempty"`
	CreatedBy      string          `json:"createdBy,omitempty" bson:"createdBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt,omitempty" bson:"createdAt,omitempty"`
	UpdatedAt      time.Time       `json:"updatedAt,omitempty" bson:"updatedAt,omitempty"`
	OrgID          string          `json:"org_id,omitempty" bson:"org_id,omitempty"`
	Status         string          `json:"status,omitempty" bson:"status,omitempty"`
}

// Widget - Base Widget Structure
type Widget struct {
	ID     string       `json:"id" bson:"id"`
	Type   string       `json:"type" bson:"type"` // table, text, image, divider, barcode, qrcode, signature
	Title  string       `json:"title,omitempty" bson:"title,omitempty"`
	X      float64      `json:"x" bson:"x"`           // Grid X position (0-11)
	Y      float64      `json:"y" bson:"y"`           // Grid Y position
	W      float64      `json:"w" bson:"w"`           // Grid width (1-12)
	H      float64      `json:"h" bson:"h"`           // Grid height
	Z      int          `json:"z,omitempty" bson:"z"` // Z-index
	PdfXpx float64      `json:"pdfXpx,omitempty" bson:"pdfXpx,omitempty"`
	PdfYpx float64      `json:"pdfYpx,omitempty" bson:"pdfYpx,omitempty"`
	PdfWpx float64      `json:"pdfWpx,omitempty" bson:"pdfWpx,omitempty"`
	PdfHpx float64      `json:"pdfHpx,omitempty" bson:"pdfHpx,omitempty"`
	Config WidgetConfig `json:"config" bson:"config"`
}

// WidgetConfig - Widget-specific configuration
type WidgetConfig struct {
	// Common fields
	Type       string `json:"type,omitempty" bson:"type,omitempty"`
	DataSource string `json:"dataSource,omitempty" bson:"dataSource,omitempty"` // static, dataset, common-dataset

	// Text widget fields
	HTML                     string             `json:"html,omitempty" bson:"html,omitempty"`
	FontSize                 float64            `json:"fontSize,omitempty" bson:"fontSize,omitempty"`
	FontFamily               string             `json:"fontFamily,omitempty" bson:"fontFamily,omitempty"`
	Alignment                string             `json:"alignment,omitempty" bson:"alignment,omitempty"`
	Bold                     bool               `json:"bold,omitempty" bson:"bold,omitempty"`
	Italic                   bool               `json:"italic,omitempty" bson:"italic,omitempty"`
	CommonDatasetId          string             `json:"commonDatasetId,omitempty" bson:"commonDatasetId,omitempty"`
	CommonDatasetField       string             `json:"commonDatasetField,omitempty" bson:"commonDatasetField,omitempty"`
	CommonDatasetFieldFormat *FieldFormatConfig `json:"commonDatasetFieldFormat,omitempty" bson:"commonDatasetFieldFormat,omitempty"`
	PreviewData              interface{}        `json:"previewData,omitempty" bson:"previewData,omitempty"`
	DatasetFields            []DatasetField     `json:"datasetFields,omitempty" bson:"datasetFields,omitempty"`
	ConditionalStyles        []ConditionalStyle `json:"conditionalStyles,omitempty" bson:"conditionalStyles,omitempty"`
	DateFormat               string             `json:"dateFormat,omitempty" bson:"dateFormat,omitempty"`
	PageNumberFormat         string             `json:"pageNumberFormat,omitempty" bson:"pageNumberFormat,omitempty"`

	// Table widget fields
	Collections       []Collection  `json:"collections,omitempty" bson:"collections,omitempty"`
	ShowSerialNumber  bool          `json:"showSerialNumber,omitempty" bson:"showSerialNumber,omitempty"`
	SerialNumberType  string        `json:"serialNumberType,omitempty" bson:"serialNumberType,omitempty"` // 1,2,3 or I,II,III
	SerialNumberWidth float64       `json:"serialNumberWidth,omitempty" bson:"serialNumberWidth,omitempty"`
	LayoutDirection   string        `json:"layoutDirection,omitempty" bson:"layoutDirection,omitempty"` // row, column
	Styling           TableStyling  `json:"styling,omitempty" bson:"styling,omitempty"`
	HeaderStyle       CellStyle     `json:"headerStyle,omitempty" bson:"headerStyle,omitempty"`
	CellStyle         CellStyle          `json:"cellStyle,omitempty" bson:"cellStyle,omitempty"`
	Aggregations      []FieldAggregation `json:"aggregations,omitempty" bson:"aggregations,omitempty"`

	// Image widget fields
	ImageUrl  string  `json:"imageUrl,omitempty" bson:"imageUrl,omitempty"`
	ImageData string  `json:"imageData,omitempty" bson:"imageData,omitempty"` // Base64 data
	Width     float64 `json:"width,omitempty" bson:"width,omitempty"`
	Height    float64 `json:"height,omitempty" bson:"height,omitempty"`

	// Divider widget fields
	Orientation  string  `json:"orientation,omitempty" bson:"orientation,omitempty"` // horizontal, vertical
	Thickness    float64 `json:"thickness,omitempty" bson:"thickness,omitempty"`
	Color        string  `json:"color,omitempty" bson:"color,omitempty"`
	MarginTop    float64 `json:"marginTop,omitempty" bson:"marginTop,omitempty"`
	MarginBottom float64 `json:"marginBottom,omitempty" bson:"marginBottom,omitempty"`
	MarginLeft   float64 `json:"marginLeft,omitempty" bson:"marginLeft,omitempty"`
	MarginRight  float64 `json:"marginRight,omitempty" bson:"marginRight,omitempty"`

	// Barcode widget fields
	Value        string `json:"value,omitempty" bson:"value,omitempty"`
	DisplayValue bool   `json:"displayValue,omitempty" bson:"displayValue,omitempty"`

	// QR Code widget fields
	Size float64 `json:"size,omitempty" bson:"size,omitempty"`

	// Signature widget fields
	Label     string `json:"label,omitempty" bson:"label,omitempty"`
	ShowLine  bool   `json:"showLine,omitempty" bson:"showLine,omitempty"`
	LineColor string `json:"lineColor,omitempty" bson:"lineColor,omitempty"`
}

// Collection - Table collection structure
type Collection struct {
	Name           string  `json:"name,omitempty" bson:"name,omitempty"`
	CollectionName string  `json:"collectionName,omitempty" bson:"collectionName,omitempty"`
	Fields         []Field `json:"fields" bson:"fields"`
}

// Field - Table field configuration
type Field struct {
	FieldName     string        `json:"fieldName" bson:"fieldName"`
	DisplayName   string        `json:"displayName,omitempty" bson:"displayName,omitempty"`
	DataType      string        `json:"dataType,omitempty" bson:"dataType,omitempty"` // string, number, date, boolean
	Format        string        `json:"format,omitempty" bson:"format,omitempty"`
	Align         string        `json:"align,omitempty" bson:"align,omitempty"` // left, center, right
	Width         float64       `json:"width,omitempty" bson:"width,omitempty"` // in px or percentage
	FontColor     string        `json:"fontColor,omitempty" bson:"fontColor,omitempty"`
	AppendSymbol  bool               `json:"appendSymbol,omitempty" bson:"appendSymbol,omitempty"`
	Prefix        string             `json:"prefix,omitempty" bson:"prefix,omitempty"`
	Suffix        string             `json:"suffix,omitempty" bson:"suffix,omitempty"`
	TextTransform string             `json:"textTransform,omitempty" bson:"textTransform,omitempty"`
	Aggregations  []FieldAggregation `json:"aggregations,omitempty" bson:"aggregations,omitempty"`
}

// FieldAggregation - Field aggregation configuration for PDF tables
type FieldAggregation struct {
	Type           string `json:"type" bson:"type"`                     // sum, count, average, min, max, etc.
	Enabled        bool   `json:"enabled" bson:"enabled"`               //
	RenderLocation string `json:"renderLocation" bson:"renderLocation"` // page, global
	Label          string `json:"label,omitempty" bson:"label,omitempty"`
}

// TableStyling - Table styling options
type TableStyling struct {
	ShowHeader              bool    `json:"showHeader,omitempty" bson:"showHeader,omitempty"`
	ShowTableBorder         bool    `json:"showTableBorder,omitempty" bson:"showTableBorder,omitempty"`
	ShowCellBorders         bool    `json:"showCellBorders,omitempty" bson:"showCellBorders,omitempty"`
	BorderWidth             float64 `json:"borderWidth,omitempty" bson:"borderWidth,omitempty"`
	BorderColor             string  `json:"borderColor,omitempty" bson:"borderColor,omitempty"`
	HeaderBackgroundColor   string  `json:"headerBackgroundColor,omitempty" bson:"headerBackgroundColor,omitempty"`
	HeaderTextColor         string  `json:"headerTextColor,omitempty" bson:"headerTextColor,omitempty"`
	StripedRows             bool    `json:"stripedRows,omitempty" bson:"stripedRows,omitempty"`
	StripeColor             string  `json:"stripeColor,omitempty" bson:"stripeColor,omitempty"`
	ShowTableHeading        bool    `json:"showTableHeading,omitempty" bson:"showTableHeading,omitempty"`
	TableHeading            string  `json:"tableHeading,omitempty" bson:"tableHeading,omitempty"`
	TableHeadingAlignment   string  `json:"tableHeadingAlignment,omitempty" bson:"tableHeadingAlignment,omitempty"`
	TableHeadingFontSize    float64 `json:"tableHeadingFontSize,omitempty" bson:"tableHeadingFontSize,omitempty"`
}

// CellStyle - Cell styling options
type CellStyle struct {
	FontSize   float64 `json:"fontSize,omitempty" bson:"fontSize,omitempty"`
	FontFamily string  `json:"fontFamily,omitempty" bson:"fontFamily,omitempty"`
	Color      string  `json:"color,omitempty" bson:"color,omitempty"`
	Bold       bool    `json:"bold,omitempty" bson:"bold,omitempty"`
	Italic     bool    `json:"italic,omitempty" bson:"italic,omitempty"`
}

// DatasetField - Dataset field placeholder
type DatasetField struct {
	DatasetId    string             `json:"datasetId" bson:"datasetId"`
	FieldPath    string             `json:"fieldPath" bson:"fieldPath"`
	Placeholder  string             `json:"placeholder" bson:"placeholder"`
	FormatConfig *FieldFormatConfig `json:"formatConfig,omitempty" bson:"formatConfig,omitempty"`
}

// FieldFormatConfig - Field formatting configuration
type FieldFormatConfig struct {
	Type               string `json:"type,omitempty" bson:"type,omitempty"` // string, number, currency, percentage, date, boolean
	TextTransform      string `json:"textTransform,omitempty" bson:"textTransform,omitempty"`
	Prefix             string `json:"prefix,omitempty" bson:"prefix,omitempty"`
	Suffix             string `json:"suffix,omitempty" bson:"suffix,omitempty"`
	DecimalPlaces      int    `json:"decimalPlaces,omitempty" bson:"decimalPlaces,omitempty"`
	ThousandsSeparator bool   `json:"thousandsSeparator,omitempty" bson:"thousandsSeparator,omitempty"`
	CurrencySymbol     string `json:"currencySymbol,omitempty" bson:"currencySymbol,omitempty"`
	CurrencyPosition   string `json:"currencyPosition,omitempty" bson:"currencyPosition,omitempty"` // before, after
	DateFormat         string `json:"dateFormat,omitempty" bson:"dateFormat,omitempty"`
	TrueText           string `json:"trueText,omitempty" bson:"trueText,omitempty"`
	FalseText          string `json:"falseText,omitempty" bson:"falseText,omitempty"`
	DefaultValue       string `json:"defaultValue,omitempty" bson:"defaultValue,omitempty"`
}

// ConditionalStyle - Conditional styling rules
type ConditionalStyle struct {
	Field    string      `json:"field" bson:"field"`
	Operator string      `json:"operator" bson:"operator"` // ==, !=, >, >=, <, <=, contains, startsWith, endsWith
	Value    interface{} `json:"value" bson:"value"`
	Style    CellStyle   `json:"style" bson:"style"`
}

// PageSettings - Page configuration
type PageSettings struct {
	PageSizeName   string       `json:"pageSizeName" bson:"pageSizeName"` // A4, A3, LETTER, CUSTOM
	Orientation    string       `json:"orientation" bson:"orientation"`   // portrait, landscape
	CustomWidthMm  float64      `json:"customWidthMm,omitempty" bson:"customWidthMm,omitempty"`
	CustomHeightMm float64      `json:"customHeightMm,omitempty" bson:"customHeightMm,omitempty"`
	MarginTop      float64      `json:"marginTop" bson:"marginTop"`       // in mm
	MarginBottom   float64      `json:"marginBottom" bson:"marginBottom"` // in mm
	MarginLeft     float64      `json:"marginLeft" bson:"marginLeft"`     // in mm
	MarginRight    float64      `json:"marginRight" bson:"marginRight"`   // in mm
	RTL            bool         `json:"rtl,omitempty" bson:"rtl,omitempty"`
	GlobalStyles   GlobalStyles `json:"globalStyles,omitempty" bson:"globalStyles,omitempty"`
	Watermark      *Watermark   `json:"watermark,omitempty" bson:"watermark,omitempty"`
	Sections       Sections     `json:"sections,omitempty" bson:"sections,omitempty"`
}

// GlobalStyles - Global styling options
type GlobalStyles struct {
	FontFamily   string `json:"fontFamily,omitempty" bson:"fontFamily,omitempty"`
	PrimaryColor string `json:"primaryColor,omitempty" bson:"primaryColor,omitempty"`
	CompanyName  string `json:"companyName,omitempty" bson:"companyName,omitempty"`
}

// Watermark - Watermark configuration
type Watermark struct {
	Enabled  bool    `json:"enabled" bson:"enabled"`
	Type     string  `json:"type" bson:"type"` // text, image
	Text     string  `json:"text,omitempty" bson:"text,omitempty"`
	ImageUrl string  `json:"imageUrl,omitempty" bson:"imageUrl,omitempty"`
	Opacity  float64 `json:"opacity,omitempty" bson:"opacity,omitempty"`
	Rotation float64 `json:"rotation,omitempty" bson:"rotation,omitempty"`
	Color    string  `json:"color,omitempty" bson:"color,omitempty"`
}

// Sections - Header/Footer sections
type Sections struct {
	ReportHeader *PdfSection `json:"reportHeader,omitempty" bson:"reportHeader,omitempty"`
	PageHeader   *PdfSection `json:"pageHeader,omitempty" bson:"pageHeader,omitempty"`
	PageFooter   *PdfSection `json:"pageFooter,omitempty" bson:"pageFooter,omitempty"`
	ReportFooter *PdfSection `json:"reportFooter,omitempty" bson:"reportFooter,omitempty"`
}

// PdfSection - Section configuration
type PdfSection struct {
	Type       string            `json:"type,omitempty" bson:"type,omitempty"` // ReportHeader, PageHeader, PageFooter, ReportFooter
	Core       SectionCore       `json:"core" bson:"core"`
	Content    SectionContent    `json:"content" bson:"content"`
	Styling    SectionStyling    `json:"styling,omitempty" bson:"styling,omitempty"`
	Pagination SectionPagination `json:"pagination,omitempty" bson:"pagination,omitempty"`
}

// SectionCore - Section core settings
type SectionCore struct {
	Enabled     bool    `json:"enabled" bson:"enabled"`
	HeightMode  string  `json:"heightMode,omitempty" bson:"heightMode,omitempty"`   // auto, fixed
	FixedHeight float64 `json:"fixedHeight,omitempty" bson:"fixedHeight,omitempty"` // in px
}

// SectionContent - Section content settings
type SectionContent struct {
	HtmlContent           string         `json:"htmlContent,omitempty" bson:"htmlContent,omitempty"`
	ConditionalVisibility string         `json:"conditionalVisibility,omitempty" bson:"conditionalVisibility,omitempty"`
	DatasetFields         []DatasetField `json:"datasetFields,omitempty" bson:"datasetFields,omitempty"`
	DateFormat            string         `json:"dateFormat,omitempty" bson:"dateFormat,omitempty"`
}

// SectionStyling - Section styling options
type SectionStyling struct {
	FontFamily      string  `json:"fontFamily,omitempty" bson:"fontFamily,omitempty"`
	FontSize        float64 `json:"fontSize,omitempty" bson:"fontSize,omitempty"`
	Color           string  `json:"color,omitempty" bson:"color,omitempty"`
	Bold            bool    `json:"bold,omitempty" bson:"bold,omitempty"`
	Italic          bool    `json:"italic,omitempty" bson:"italic,omitempty"`
	BackgroundColor string  `json:"backgroundColor,omitempty" bson:"backgroundColor,omitempty"`
	Padding         float64 `json:"padding,omitempty" bson:"padding,omitempty"`
	BorderTop       bool    `json:"borderTop,omitempty" bson:"borderTop,omitempty"`
	BorderBottom    bool    `json:"borderBottom,omitempty" bson:"borderBottom,omitempty"`
	BorderWidth     float64 `json:"borderWidth,omitempty" bson:"borderWidth,omitempty"`
	BorderColor     string  `json:"borderColor,omitempty" bson:"borderColor,omitempty"`
	FontInheritance string  `json:"fontInheritance,omitempty" bson:"fontInheritance,omitempty"` // inherit, override
}

// SectionPagination - Section pagination settings
type SectionPagination struct {
	SupportsPageNumbers bool    `json:"supportsPageNumbers,omitempty" bson:"supportsPageNumbers,omitempty"`
	PageNumberFormat    string  `json:"pageNumberFormat,omitempty" bson:"pageNumberFormat,omitempty"` // e.g., "Page {current} of {total}"
	PageNumberAlignment string  `json:"pageNumberAlignment,omitempty" bson:"pageNumberAlignment,omitempty"`
	PageNumberFontSize  float64 `json:"pageNumberFontSize,omitempty" bson:"pageNumberFontSize,omitempty"`
	ShowTradeMark       bool    `json:"showTradeMark,omitempty" bson:"showTradeMark,omitempty"`
	TradeMarkText       string  `json:"tradeMarkText,omitempty" bson:"tradeMarkText,omitempty"`
	TradeMarkAlignment  string  `json:"tradeMarkAlignment,omitempty" bson:"tradeMarkAlignment,omitempty"`
	TradeMarkFontSize   float64 `json:"tradeMarkFontSize,omitempty" bson:"tradeMarkFontSize,omitempty"`
}

// CommonDataset - Dataset configuration
type CommonDataset struct {
	ID          string                   `json:"id" bson:"id"`
	Name        string                   `json:"name" bson:"name"`
	DatasetName string                   `json:"datasetName,omitempty" bson:"datasetName,omitempty"`
	Collection  string                   `json:"collection,omitempty" bson:"collection,omitempty"`
	Filter      interface{}              `json:"filter,omitempty" bson:"filter,omitempty"`
	Data        []map[string]interface{} `json:"data,omitempty" bson:"data,omitempty"`
}

// PDFExportRequest - API request structure
type PDFExportRequest struct {
	TemplateID  string                            `json:"templateId,omitempty"`
	Template    *PDFTemplate                      `json:"template,omitempty"`
	DataPayload map[string]map[string]interface{} `json:"dataPayload,omitempty"`
}

// PageSize - Page dimensions in points
type PageSize struct {
	Width  float64
	Height float64
}

// Standard page sizes in points
var PageSizes = map[string]PageSize{
	"A4":     {Width: 595.28, Height: 841.89},
	"A3":     {Width: 841.89, Height: 1190.55},
	"LETTER": {Width: 612, Height: 792},
	"LEGAL":  {Width: 612, Height: 1008},
	"A5":     {Width: 419.53, Height: 595.28},
}

// Aggregation type constants
const (
	AggSum        = "sum"
	AggCount      = "count"
	AggAverage    = "average"
	AggMin        = "min"
	AggMax        = "max"
	AggFirst      = "first"
	AggLast       = "last"
	AggDistinct   = "distinct"
	AggMedian     = "median"
	AggMode       = "mode"
	AggStdDev     = "stddev"
	AggVariance   = "variance"
	AggRange      = "range"
	AggPercentile = "percentile"
)
