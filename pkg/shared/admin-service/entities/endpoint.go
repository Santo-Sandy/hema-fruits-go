package entities

import (
	"github.com/gofiber/fiber/v2"
	einvoice "kriyatec.com/pms-api/pkg/shared/einvoice"
	"kriyatec.com/pms-api/pkg/shared/helper"
	"kriyatec.com/pms-api/pkg/shared/onboarding"
	"kriyatec.com/pms-api/pkg/shared/utils"
)

func SetupAllRoutes(app *fiber.App) {
	SetupPDFRoutes(app)
	SetupCRUDRoutes(app)
	SetupLookupRoutes(app)
	SetupDownloadRoutes(app)
	SetupDatasets(app)
	updateProcessProduct(app)
	SetupkernalInventory(app)
	SetupProductionMovementsRoutes(app)
	setUpPackingUpdate(app)
	SetupexcelRoutes(app)
	SetupemployeebulkUpload(app)
	// SetupStockLedgerRoutes(app)
	SetupPettyCash(app)
	STockReadjusmentApi(app)
	app.Static("/image", fileUploadPath)
	SetupaccessUser(app)
	StreamApiData(app)
	DBMigrate(app)
	DeleteByRule(app)
	// genratesample(app)
	getDemofactory(app)
	SetupEInvoiceRoutes(app)
	SetupEwayBillRoutes(app)
	SetupHelperRoutes(app)

	//	app.Get("/generate-pdf", PdfGenerator)
	// app.Get("/generate-pdf/:purchaseId/:start_date/:end_date", PdfGenerator)
	app.Put("/update-production/:Id?", ProductionDataUpdate)
	app.Get("/post-check", PostChecking)
}

func SetupaccessUser(app *fiber.App) {
	r := app.Group("/activation-api/")

	// Open routes (No Token Required)
	r.Put("/generate-pwd/:access_key", helper.UpdateUserPasswordandremoveTempData)
	r.Get("/:access_key", helper.RetrieveTemporaryUserDataByAccessKey)
	r.Post("/onboard-org", onboarding.ProvisionOrgHandler)
	r.Post("/trigger-daily-data", onboarding.ManualTriggerDailyDataHandler) // Moved here for cron job access

	// Protected routes (Token Required)
	p := r.Group("/", utils.JWTMiddleware())
	// p.Get("/generation-status/:orgId", onboarding.GetGenerationStatusHandler)
	p.Post("/activate-org/:orgId", onboarding.ActivateOrgHandler)
	p.Post("/remove-data", onboarding.RemoveCollectionDataHandler)
	p.Post("/generate-trial-data", onboarding.InitiateTrialDataSimulationHandler)
}


// SetupCRUDRoutes  --METHOD BaseCud Endpoint

func SetupCRUDRoutes(app *fiber.App) { // cerp
	r := helper.CreateRouteGroup(app, "/entities/", "REST API")
	r.Post("/:model_name", PostDocHandler)
	r.Put("/:model_name/:id?/", putDocByIDHandlers)
	r.Get("/:collectionName/:id", GetDocByIdHandler)
	r.Delete("/:collectionName/:id", DeleteById)
	r.Delete("/:collectionName/:id/:type", DeleteById)

	r.Delete("/:collectionName", DeleteByAll)
	r.Post("/filter/:collectionName", getDocsHandler)
	//pdf report
	r.Get("/collections", GetCollectionsHandler)
	r.Get("/collections/:collectionName/fields", GetCollectionFieldsHandler)
	r.Post("/:collectionName/aggregate", AggregateHandler)
	r.Get("datasets", GetDatasetHandler)
	//pdf report

	// Custom route for creating screen collections for active processes
	r.Post("/screen/create-for-active-processes", CreateScreenForActiveProcessesHandler)

	//Old pms code endpoint and func
	r.Get("/filter/:collectionName/:projectid", getDocByIddHandler)
	r.Get("/filters/:collectionName/:clientname", getDocByClientIdHandler)
}

// Data set
func SetupDatasets(app *fiber.App) { // cerp
	r := helper.CreateRouteGroup(app, "/dataset", "Data Sets")
	r.Post("/config/:options?", helper.DatasetsConfig)
	r.Post("/data/:datasetname", helper.DatasetsRetrieve)
	r.Put("/:datasetname", helper.UpdateDataset)
}

// SetupLookupRoutes -- METHOD current  Individual Endpoints
func SetupLookupRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/lookup", "Data Lookup API")
	r.Get("/lot_history/:factory_id", GetLotHistoryFlag) // cerp
	r.Get("/productions/:start_date/:end_date", GetLotHistoryCount)
	r.Get("/purchase_details/:types/:id", GetPurchaseDetailsWithSalesAndFactoryInwards)
	r.Get("/purchase_details", GetPurchaseDetailsWithInwards)
}

// SetupDownloadRoutes   --  METHOD S3 Handler  Setup Download
func SetupDownloadRoutes(app *fiber.App) { // cerp
	r := helper.CreateRouteGroup(app, "/file", "Upload APIs")
	r.Post("/:folder/:refId", helper.FileUpload)
	r.Get("/all/:category/:status/:page?/:limit?", getAllFileDetails)
	r.Get("/:folder/:refId", getFileDetails)
	r.Delete("/:folder/:refId", helper.DeleteFileIns3)
	r.Post("/generate-excel", helper.ExcelGenerator1)
	// r.Post("/generate-pdf", helper.GenerateSaleReport)
}

func SetupProductionMovementsRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/stock-report", "Generate Pdf APIs")
	r.Get("/generate-pdf/:purchaseId/:start_date/:end_date", PdfGenerator)
}

func setUpPackingUpdate(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/update-packing", "Generate Pdf APIs")
	r.Put("/:model_name/:id", UpdateDocForPacking)
}

// func SetupStockLedgerRoutes(app *fiber.App) {
// 	r := helper.CreateRouteGroup(app, "/stock", "Stock Management APIs")
// 	// r.Post("/test", TestPurchase)
// 	r.Post("/test/adjustment", TestAdjustment)
// }

func SetupPettyCash(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/cash", "Cash Management APIs")
	r.Post("/:modelName", ProcessPettyCash)
	r.Put("/r", ProcessPettyCashRentry)
}

func SetupkernalInventory(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/static", "Kernal Inventory APIs")
	r.Post("/kernal_inventory", KernalInventory)
	r.Put("/kernal_inventory/:id", KernalInventory)
	r.Post("/kernal_inventory/serial", GetKernalInventory)
}
func STockReadjusmentApi(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/stock-ledger", "Petty Cash Management APIs")
	r.Post("/", StockReadjustmentApi)
	r.Post("/cok", StockReadjustmentApiForCooking)
}

func StreamApiData(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/stream", "Data Stream APIs")
	r.Get("/:modelName/:type", StreamData)
	r.Get("/:modelName", StreamData)
}

func DBMigrate(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/migrate", "Data Stream APIs")
	r.Post("/", MigrateOneDBToAnotherDB)
	r.Get("/re", ReadjusmnetProduction)
	r.Post("/recal", recalculateStockDetails)
}

func DeleteByRule(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/delete", "Data Stream APIs")
	r.Delete("/rule", CheckDelete)
}

// SetupPDFRoutes sets up PDF generation routes
func SetupPDFRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/pdf", "PDF Generation APIs")

	// Export PDF via Node.js service
	r.Post("/export", helper.ExportPDFViaNodeHandler)
}

// SetupPDFRoutes sets up PDF generation routes
func SetupexcelRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/excel", "PDF Generation APIs")

	// Export PDF via Node.js service
	r.Post("/upload-production", ExcelUploadToProductions)
}

// SetupEInvoiceRoutes sets up e-invoice API routes
func SetupEInvoiceRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/api/einvoice", "E-Invoice APIs")
	r.Get("/generate", einvoice.GenerateEInvoiceHandler)
	r.Post("/cancel", einvoice.CancelIRNHandler)
	r.Get("/download", einvoice.DownloadEInvoiceHandler)
}

func SetupHelperRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/helper", "Helper APIs")
	r.Post("/validate-gst", einvoice.ValidateGSTNHandler)

	// Customer validation route
	customer := r.Group("/customer")
	customer.Post("/validate-gst", einvoice.ValidateCustomerHandler)
}

func SetupEwayBillRoutes(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/api/ewaybill", "E-way Bill APIs")
	r.Get("/generate", einvoice.GenerateEwayBillHandler)
	r.Post("/cancel", einvoice.CancelEwayBillHandler)
}

func updateProcessProduct(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/update-process-product", "Update Process Product API")
	r.Get("/update", UpdateProductField)
}
func SetupemployeebulkUpload(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/bulk-upload", "Employee APIs")
	r.Post("/employee", EmployeeBulkPostHandler)

	r.Post("/equipments", EquipmentBulkPostHandler)
}
func genratesample(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/generate-sample", "Sample Data Generation API")
	r.Post("/data/demo", generateSampleData)
}
func getDemofactory(app *fiber.App) {
	r := helper.CreateRouteGroup(app, "/demo", "Demo APIs")
	r.Get("/data", GetDemoFactory)
}
