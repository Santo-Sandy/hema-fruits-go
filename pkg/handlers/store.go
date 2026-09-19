package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/middleware"
	"hema-fruits-go/pkg/models"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Subcategory struct {
	ID        string `json:"id" bson:"id"`
	Name      string `json:"name" bson:"name"`
	Slug      string `json:"slug" bson:"slug"`
	IconURL   string `json:"icon_url" bson:"icon_url"`
	ItemCount int    `json:"item_count" bson:"item_count"`
}

type Category struct {
	ID            string        `json:"id" bson:"_id"`
	Name          string        `json:"name" bson:"name"`
	Slug          string        `json:"slug" bson:"slug"`
	IconURL       string        `json:"icon_url" bson:"icon_url"`
	BannerURL     string        `json:"banner_url" bson:"banner_url"`
	Subcategories []Subcategory `json:"subcategories" bson:"subcategories"`
	DisplayOrder  int           `json:"display_order" bson:"display_order"`
	IsActive      bool          `json:"is_active" bson:"is_active"`
}

type ProductVariant struct {
	ID            string  `json:"id" bson:"id"`
	ProductID     string  `json:"product_id" bson:"product_id"`
	WeightValue   float64 `json:"weight_value" bson:"weight_value"`
	WeightUnit    string  `json:"weight_unit" bson:"weight_unit"`
	PackagingType string  `json:"packaging_type" bson:"packaging_type"`
	MRP           float64 `json:"mrp" bson:"mrp"`
	SellingPrice  float64 `json:"selling_price" bson:"selling_price"`
	StockQuantity int     `json:"stock_quantity" bson:"stock_quantity"`
	SKU           string  `json:"sku" bson:"sku"`
	IsAvailable   bool    `json:"is_available" bson:"is_available"`
}

type Product struct {
	ID                  string           `json:"id" bson:"_id"`
	Title               string           `json:"title" bson:"title"`
	Slug                string           `json:"slug" bson:"slug"`
	CategoryID          string           `json:"category_id" bson:"category_id"`
	SubcategoryID       string           `json:"subcategory_id" bson:"subcategory_id"`
	Description         string           `json:"description" bson:"description"`
	Images              []string         `json:"images" bson:"images"`
	ShelfLifeDays       int              `json:"shelf_life_days" bson:"shelf_life_days"`
	StorageInstructions string           `json:"storage_instructions" bson:"storage_instructions"`
	IsOrganic           bool             `json:"is_organic" bson:"is_organic"`
	QualityGrade        string           `json:"quality_grade" bson:"quality_grade"`
	OriginRegion        string           `json:"origin_region" bson:"origin_region"`
	SellerID            string           `json:"seller_id,omitempty" bson:"seller_id,omitempty"`
	SellerName          string           `json:"seller_name,omitempty" bson:"seller_name,omitempty"`
	Variants            []ProductVariant `json:"variants" bson:"variants"`
	AvgRating           float64          `json:"avg_rating" bson:"avg_rating"`
	ReviewCount         int              `json:"review_count" bson:"review_count"`
	IsFeatured          bool             `json:"is_featured" bson:"is_featured"`
}

type StoreBanner struct {
	ID             string `json:"id" bson:"_id"`
	Title          string `json:"title" bson:"title"`
	Subtitle       string `json:"subtitle" bson:"subtitle"`
	ImageURL       string `json:"image_url" bson:"image_url"`
	TargetCategory string `json:"target_category" bson:"target_category"`
	ActionURL      string `json:"action_url" bson:"action_url"`
}

type CartItem struct {
	ProductID    string  `json:"product_id" bson:"product_id"`
	VariantID    string  `json:"variant_id" bson:"variant_id"`
	ProductTitle string  `json:"product_title" bson:"product_title"`
	ImageURL     string  `json:"image_url" bson:"image_url"`
	WeightValue  float64 `json:"weight_value" bson:"weight_value"`
	WeightUnit   string  `json:"weight_unit" bson:"weight_unit"`
	UnitPrice    float64 `json:"unit_price" bson:"unit_price"`
	MRP          float64 `json:"mrp" bson:"mrp"`
	Quantity     int     `json:"quantity" bson:"quantity"`
	TotalPrice   float64 `json:"total_price" bson:"total_price"`
	IsPerishable bool    `json:"is_perishable" bson:"is_perishable"`
}

type Cart struct {
	UserID            string     `json:"user_id" bson:"_id"`
	Items             []CartItem `json:"items" bson:"items"`
	ItemTotal         float64    `json:"item_total" bson:"item_total"`
	TotalMRP          float64    `json:"total_mrp" bson:"total_mrp"`
	DiscountTotal     float64    `json:"discount_total" bson:"discount_total"`
	DeliveryFee       float64    `json:"delivery_fee" bson:"delivery_fee"`
	PackagingFee      float64    `json:"packaging_fee" bson:"packaging_fee"`
	AppliedCouponCode string     `json:"applied_coupon" bson:"applied_coupon"`
	CouponDiscount    float64    `json:"coupon_discount" bson:"coupon_discount"`
	GrandTotal        float64    `json:"grand_total" bson:"grand_total"`
}

type Order struct {
	ID                 string     `json:"id" bson:"_id"`
	OrderNumber        string     `json:"order_number" bson:"order_number"`
	UserID             string     `json:"user_id" bson:"user_id"`
	Items              []CartItem `json:"items" bson:"items"`
	PaymentMethod      string     `json:"payment_method" bson:"payment_method"`
	OrderStatus        string     `json:"order_status" bson:"order_status"`
	GrandTotal         float64    `json:"grand_total" bson:"grand_total"`
	DeliveryOTP        string     `json:"delivery_otp" bson:"delivery_otp"`
	DeliveryAgentName  string     `json:"delivery_agent_name" bson:"delivery_agent_name"`
	DeliveryAgentPhone string     `json:"delivery_agent_phone" bson:"delivery_agent_phone"`
	PlacedAt           time.Time  `json:"placed_at" bson:"placed_at"`
}

func SetupStoreRoutes(app *fiber.App) {
	api := app.Group("/api/v1/store")

	// Categories CRUD
	api.Get("/categories", GetCategoriesHandler)
	api.Post("/categories", CreateCategoryHandler)
	api.Put("/categories/:id", UpdateCategoryHandler)
	api.Delete("/categories/:id", DeleteCategoryHandler)

	// Products CRUD
	api.Get("/products", GetProductsHandler)
	api.Get("/products/:id", GetProductByIDHandler)
	api.Post("/products", CreateProductHandler)
	api.Put("/products/:id", UpdateProductHandler)
	api.Delete("/products/:id", DeleteProductHandler)

	// Banners CRUD
	api.Get("/banners", GetBannersHandler)
	api.Post("/banners", CreateBannerHandler)
	api.Delete("/banners/:id", DeleteBannerHandler)

	// Cart CRUD
	api.Get("/cart", GetCartHandler)
	api.Post("/cart/item", AddOrUpdateCartItemHandler)
	api.Delete("/cart/item/:variantId", RemoveCartItemHandler)
	api.Delete("/cart", ClearCartHandler)
	api.Post("/cart/coupon", ApplyCouponHandler)

	// Checkout & Orders CRUD
	api.Post("/checkout/validate", ValidateCheckoutHandler)
	api.Post("/orders", CreateOrderHandler)
	api.Get("/orders", GetUserOrdersHandler)
	api.Get("/orders/:id", GetOrderByIDHandler)
	api.Put("/orders/:id", UpdateOrderStatusHandler)
	api.Delete("/orders/:id", DeleteOrderHandler)

	// Admin & Users Stats
	api.Get("/admin/stats", GetAdminStatsHandler)
	api.Get("/users", GetStoreUsersHandler)
}

// ─────────────────────────────────────────────────────────────────────────────
// CATEGORY HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

func GetCategoriesHandler(c *fiber.Ctx) error {
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var categories []Category
	cursor, err := db.Collection("categories").Find(ctx, bson.M{"is_active": true}, options.Find().SetSort(bson.D{{Key: "display_order", Value: 1}}))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch categories"})
	}
	if err := cursor.All(ctx, &categories); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to decode categories"})
	}
	return c.JSON(fiber.Map{"success": true, "categories": categories})
}

func CreateCategoryHandler(c *fiber.Ctx) error {
	var cat Category
	if err := c.BodyParser(&cat); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	if cat.ID == "" {
		cat.ID = "cat_" + GenerateUniqueKey()
	}
	cat.IsActive = true
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("categories").InsertOne(ctx, cat)
	}
	return c.JSON(fiber.Map{"success": true, "category": cat})
}

func UpdateCategoryHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	var cat Category
	if err := c.BodyParser(&cat); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	cat.ID = id
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("categories").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": cat})
	}
	return c.JSON(fiber.Map{"success": true, "category": cat})
}

func DeleteCategoryHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("categories").DeleteOne(ctx, bson.M{"_id": id})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Category deleted successfully"})
}

// ─────────────────────────────────────────────────────────────────────────────
// PRODUCT HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

func GetProductsHandler(c *fiber.Ctx) error {
	catID := c.Query("category_id")
	sellerID := c.Query("seller_id")
	search := strings.ToLower(c.Query("search"))
	organic := c.Query("organic")

	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{}
	if catID != "" {
		filter["category_id"] = catID
	}
	if sellerID != "" {
		filter["seller_id"] = sellerID
	}
	if organic == "true" {
		filter["is_organic"] = true
	}
	if search != "" {
		filter["$or"] = bson.A{
			bson.M{"title": bson.M{"$regex": search, "$options": "i"}},
			bson.M{"description": bson.M{"$regex": search, "$options": "i"}},
		}
	}

	var products []Product
	cursor, err := db.Collection("products").Find(ctx, filter)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch products"})
	}
	if err := cursor.All(ctx, &products); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to decode products"})
	}
	return c.JSON(fiber.Map{"success": true, "products": products})
}

func GetProductByIDHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var p Product
	if err := db.Collection("products").FindOne(ctx, bson.M{"_id": id}).Decode(&p); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Product not found"})
	}
	return c.JSON(fiber.Map{"success": true, "product": p})
}

func CreateProductHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	var prod Product
	if err := c.BodyParser(&prod); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	if prod.ID == "" {
		prod.ID = "prod_" + GenerateUniqueKey()
	}
	if prod.SellerID == "" && userToken.UserId != "" {
		prod.SellerID = userToken.UserId
	}

	db := config.GetDB()
	if db != nil {
		if prod.SellerName == "" && prod.SellerID != "" {
			var u models.User
			if err := db.Collection("users").FindOne(context.Background(), bson.M{"_id": prod.SellerID}).Decode(&u); err == nil {
				if u.StoreName != "" {
					prod.SellerName = u.StoreName
				} else {
					prod.SellerName = u.Name
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("products").InsertOne(ctx, prod)
	}
	return c.JSON(fiber.Map{"success": true, "product": prod})
}

func UpdateProductHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	var prod Product
	if err := c.BodyParser(&prod); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	prod.ID = id
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("products").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": prod})
	}
	return c.JSON(fiber.Map{"success": true, "product": prod})
}

func DeleteProductHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("products").DeleteOne(ctx, bson.M{"_id": id})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Product deleted successfully"})
}

// ─────────────────────────────────────────────────────────────────────────────
// BANNER HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

func GetBannersHandler(c *fiber.Ctx) error {
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var banners []StoreBanner
	cursor, err := db.Collection("store_banners").Find(ctx, bson.M{})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch banners"})
	}
	cursor.All(ctx, &banners)
	return c.JSON(fiber.Map{"success": true, "banners": banners})
}

func CreateBannerHandler(c *fiber.Ctx) error {
	var b StoreBanner
	if err := c.BodyParser(&b); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	if b.ID == "" {
		b.ID = "b_" + GenerateUniqueKey()
	}
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("store_banners").InsertOne(ctx, b)
	}
	return c.JSON(fiber.Map{"success": true, "banner": b})
}

func DeleteBannerHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("store_banners").DeleteOne(ctx, bson.M{"_id": id})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Banner deleted successfully"})
}

// ─────────────────────────────────────────────────────────────────────────────
// CART HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

func GetCartHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cart Cart
	err := db.Collection("carts").FindOne(ctx, bson.M{"_id": userID}).Decode(&cart)
	if err != nil {
		cart = Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
	}
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func recalculateCartTotals(cart *Cart) {
	cart.ItemTotal = 0
	cart.TotalMRP = 0
	for _, item := range cart.Items {
		cart.ItemTotal += item.UnitPrice * float64(item.Quantity)
		cart.TotalMRP += item.MRP * float64(item.Quantity)
	}
	cart.DiscountTotal = cart.TotalMRP - cart.ItemTotal
	if cart.ItemTotal >= 499 || len(cart.Items) == 0 {
		cart.DeliveryFee = 0
	} else {
		cart.DeliveryFee = 35
	}
	if len(cart.Items) == 0 {
		cart.PackagingFee = 0
	} else {
		cart.PackagingFee = 15
	}

	if cart.AppliedCouponCode == "FRESH100" {
		cart.CouponDiscount = 100
	} else if cart.AppliedCouponCode == "FRESH50" {
		cart.CouponDiscount = 50
	} else {
		cart.CouponDiscount = 0
	}

	cart.GrandTotal = cart.ItemTotal + cart.DeliveryFee + cart.PackagingFee - cart.CouponDiscount
	if cart.GrandTotal < 0 {
		cart.GrandTotal = 0
	}
}

func AddOrUpdateCartItemHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	var item CartItem
	if err := c.BodyParser(&item); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	item.TotalPrice = item.UnitPrice * float64(item.Quantity)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cart Cart
	err := db.Collection("carts").FindOne(ctx, bson.M{"_id": userID}).Decode(&cart)
	if err != nil {
		cart = Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
	}

	found := false
	for i, existing := range cart.Items {
		if existing.VariantID == item.VariantID {
			if item.Quantity <= 0 {
				cart.Items = append(cart.Items[:i], cart.Items[i+1:]...)
			} else {
				cart.Items[i].Quantity = item.Quantity
				cart.Items[i].TotalPrice = item.UnitPrice * float64(item.Quantity)
			}
			found = true
			break
		}
	}
	if !found && item.Quantity > 0 {
		cart.Items = append(cart.Items, item)
	}

	recalculateCart(&cart)
	db.Collection("carts").FindOneAndReplace(ctx, bson.M{"_id": userID}, cart, options.FindOneAndReplace().SetUpsert(true))
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func RemoveCartItemHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	variantID := c.Params("variantId")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cart Cart
	if err := db.Collection("carts").FindOne(ctx, bson.M{"_id": userID}).Decode(&cart); err == nil {
		var updated []CartItem
		for _, item := range cart.Items {
			if item.VariantID != variantID {
				updated = append(updated, item)
			}
		}
		cart.Items = updated
		recalculateCart(&cart)
		db.Collection("carts").FindOneAndReplace(ctx, bson.M{"_id": userID}, cart, options.FindOneAndReplace().SetUpsert(true))
	}
	return c.JSON(fiber.Map{"success": true})
}

func ClearCartHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	empty := Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
	db.Collection("carts").FindOneAndReplace(ctx, bson.M{"_id": userID}, empty, options.FindOneAndReplace().SetUpsert(true))
	return c.JSON(fiber.Map{"success": true})
}

func ApplyCouponHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	var req struct {
		Coupon string `json:"coupon"`
	}
	c.BodyParser(&req)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cart Cart
	if err := db.Collection("carts").FindOne(ctx, bson.M{"_id": userID}).Decode(&cart); err != nil {
		cart = Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
	}
	switch req.Coupon {
	case "FRESH100":
		cart.AppliedCouponCode = "FRESH100"
		cart.CouponDiscount = 100
	case "FRESH50":
		cart.AppliedCouponCode = "FRESH50"
		cart.CouponDiscount = 50
	default:
		cart.AppliedCouponCode = ""
		cart.CouponDiscount = 0
	}
	recalculateCart(&cart)
	db.Collection("carts").FindOneAndReplace(ctx, bson.M{"_id": userID}, cart, options.FindOneAndReplace().SetUpsert(true))
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func recalculateCart(cart *Cart) {
	var itemTotal float64 = 0
	var totalMRP float64 = 0
	for _, it := range cart.Items {
		itemTotal += it.TotalPrice
		totalMRP += it.MRP * float64(it.Quantity)
	}
	cart.ItemTotal = itemTotal
	cart.TotalMRP = totalMRP
	cart.DiscountTotal = totalMRP - itemTotal
	if itemTotal >= 499 || len(cart.Items) == 0 {
		cart.DeliveryFee = 0
	} else {
		cart.DeliveryFee = 35
	}
	grand := itemTotal + cart.DeliveryFee + cart.PackagingFee - cart.CouponDiscount
	if grand < 0 {
		grand = 0
	}
	cart.GrandTotal = grand
}

// ─────────────────────────────────────────────────────────────────────────────
// ORDER HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

func ValidateCheckoutHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "serviceable": true, "estimated_mins": 45})
}

func CreateOrderHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}

	var order Order
	if err := c.BodyParser(&order); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attach cart items from DB
	var cart Cart
	if err := db.Collection("carts").FindOne(ctx, bson.M{"_id": userID}).Decode(&cart); err == nil {
		order.Items = cart.Items
		order.GrandTotal = cart.GrandTotal
	}

	order.ID = "ord_" + GenerateUniqueKey()
	order.OrderNumber = fmt.Sprintf("HEMA-%d", time.Now().Unix()%100000)
	order.UserID = userID
	order.OrderStatus = "PLACED"
	order.PlacedAt = time.Now()

	// Persist order
	if _, err := db.Collection("orders").InsertOne(ctx, order); err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to place order"})
	}

	// Clear the cart
	empty := Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
	db.Collection("carts").FindOneAndReplace(ctx, bson.M{"_id": userID}, empty, options.FindOneAndReplace().SetUpsert(true))

	return c.JSON(fiber.Map{"success": true, "order": order})
}

func GetUserOrdersHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		return c.Status(401).JSON(fiber.Map{"success": false, "message": "Unauthorized"})
	}
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var list []Order
	cursor, err := db.Collection("orders").Find(
		ctx,
		bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "placed_at", Value: -1}}),
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch orders"})
	}
	cursor.All(ctx, &list)
	return c.JSON(fiber.Map{"success": true, "orders": list})
}

func GetOrderByIDHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var order Order
	if err := db.Collection("orders").FindOne(ctx, bson.M{"_id": id}).Decode(&order); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "message": "Order not found"})
	}
	return c.JSON(fiber.Map{"success": true, "order": order})
}

func UpdateOrderStatusHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}

	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.Collection("orders").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"order_status": req.Status}})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to update order status"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Order status updated"})
}

func DeleteOrderHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.Collection("orders").DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to delete order"})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Order deleted"})
}

// ─────────────────────────────────────────────────────────────────────────────
// ADMIN & ANALYTICS HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

// GetAdminStatsHandler returns aggregated overview metrics for Admin Dashboard
func GetAdminStatsHandler(c *fiber.Ctx) error {
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	totalProducts, _ := db.Collection("products").CountDocuments(ctx, bson.M{})
	totalUsers, _ := db.Collection("users").CountDocuments(ctx, bson.M{})
	totalSellers, _ := db.Collection("users").CountDocuments(ctx, bson.M{"$or": bson.A{bson.M{"role": "processor"}, bson.M{"role": "seller"}}})
	totalBuyers, _ := db.Collection("users").CountDocuments(ctx, bson.M{"role": "buyer"})
	totalOrders, _ := db.Collection("orders").CountDocuments(ctx, bson.M{})

	// Calculate gross revenue from orders
	var orders []Order
	cur, _ := db.Collection("orders").Find(ctx, bson.M{})
	if cur != nil {
		cur.All(ctx, &orders)
	}
	var totalRevenue float64
	for _, o := range orders {
		totalRevenue += o.GrandTotal
	}

	return c.JSON(fiber.Map{
		"success": true,
		"stats": fiber.Map{
			"total_products": totalProducts,
			"total_users":    totalUsers,
			"total_sellers":  totalSellers,
			"total_buyers":   totalBuyers,
			"total_orders":   totalOrders,
			"total_revenue":  totalRevenue,
		},
	})
}

// GetStoreUsersHandler returns registered accounts for Admin view
func GetStoreUsersHandler(c *fiber.Ctx) error {
	roleFilter := c.Query("role")
	db := config.GetDB()
	if db == nil {
		return c.Status(503).JSON(fiber.Map{"success": false, "message": "Database not available"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{}
	if roleFilter != "" {
		if roleFilter == "seller" || roleFilter == "processor" {
			filter["$or"] = bson.A{bson.M{"role": "processor"}, bson.M{"role": "seller"}}
		} else {
			filter["role"] = roleFilter
		}
	}

	var users []bson.M
	cur, err := db.Collection("users").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_on", Value: -1}}))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "message": "Failed to fetch users"})
	}
	if cur != nil {
		cur.All(ctx, &users)
	}
	for i := range users {
		delete(users[i], "pwd")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"users":   users,
	})
}

// SeedStoreData seeds complete mock data for categories, products, and banners if collections are empty.
func SeedStoreData() {
	db := config.GetDB()
	if db == nil {
		fmt.Println("SeedStoreData: no DB connection, skipping store seed")
		return
	}
	ctx := context.Background()

	// ── Seed Categories ───────────────────────────────────────────────────────
	catCount, _ := db.Collection("categories").CountDocuments(ctx, bson.M{})
	if catCount == 0 {
		categories := []interface{}{
			bson.M{
				"_id": "cat_fruits", "name": "Fresh Fruits", "slug": "fresh-fruits",
				"icon_url":      "https://images.unsplash.com/photo-1619566636858-adf3ef46400b?w=200",
				"banner_url":    "https://images.unsplash.com/photo-1619566636858-adf3ef46400b?w=800",
				"display_order": 1, "is_active": true,
				"subcategories": []bson.M{
					{"id": "sub_apples", "name": "Apples & Pears", "slug": "apples-pears", "icon_url": "", "item_count": 12},
					{"id": "sub_exotic", "name": "Exotic Fruits", "slug": "exotic-fruits", "icon_url": "", "item_count": 15},
					{"id": "sub_citrus", "name": "Citrus & Berries", "slug": "citrus-berries", "icon_url": "", "item_count": 10},
				},
			},
			bson.M{
				"_id": "cat_veggies", "name": "Fresh Vegetables", "slug": "fresh-vegetables",
				"icon_url":      "https://images.unsplash.com/photo-1540420773420-3366772f4999?w=200",
				"banner_url":    "https://images.unsplash.com/photo-1540420773420-3366772f4999?w=800",
				"display_order": 2, "is_active": true,
				"subcategories": []bson.M{
					{"id": "sub_leafy", "name": "Leafy Greens", "slug": "leafy-greens", "icon_url": "", "item_count": 18},
					{"id": "sub_daily", "name": "Daily Essentials", "slug": "daily-essentials", "icon_url": "", "item_count": 25},
					{"id": "sub_roots", "name": "Root Vegetables", "slug": "root-vegetables", "icon_url": "", "item_count": 14},
				},
			},
			bson.M{
				"_id": "cat_dry_nuts", "name": "Dry Fruits & Nuts", "slug": "dry-fruits-nuts",
				"icon_url":      "https://images.unsplash.com/photo-1508061252966-177209772242?w=200",
				"banner_url":    "https://images.unsplash.com/photo-1508061252966-177209772242?w=800",
				"display_order": 3, "is_active": true,
				"subcategories": []bson.M{
					{"id": "sub_almonds", "name": "Almonds & Cashews", "slug": "almonds-cashews", "icon_url": "", "item_count": 20},
					{"id": "sub_walnuts", "name": "Walnuts & Pistachios", "slug": "walnuts-pistachios", "icon_url": "", "item_count": 16},
					{"id": "sub_raisins", "name": "Raisins & Dates", "slug": "raisins-dates", "icon_url": "", "item_count": 12},
				},
			},
		}
		db.Collection("categories").InsertMany(ctx, categories)
		fmt.Println("SeedStoreData: seeded 3 categories")
	}

	// ── Seed Banners ──────────────────────────────────────────────────────────
	bannerCount, _ := db.Collection("store_banners").CountDocuments(ctx, bson.M{})
	if bannerCount == 0 {
		banners := []interface{}{
			bson.M{"_id": "banner_1", "title": "Farm Fresh Mangoes & Berries", "subtitle": "Up to 30% OFF | Handpicked Daily",
				"image_url":       "https://images.unsplash.com/photo-1553279768-865429fa0078?w=1000",
				"target_category": "cat_fruits", "action_url": "/ecommerce/category/cat_fruits"},
			bson.M{"_id": "banner_2", "title": "100% Organic Pesticide-Free Greens", "subtitle": "Direct from Certified Farmers",
				"image_url":       "https://images.unsplash.com/photo-1540420773420-3366772f4999?w=1000",
				"target_category": "cat_veggies", "action_url": "/ecommerce/category/cat_veggies"},
			bson.M{"_id": "banner_3", "title": "Premium Jumbo Cashews & Walnuts", "subtitle": "Rich in Nutrients | Vacuum Packed",
				"image_url":       "https://images.unsplash.com/photo-1508061252966-177209772242?w=1000",
				"target_category": "cat_dry_nuts", "action_url": "/ecommerce/category/cat_dry_nuts"},
		}
		db.Collection("store_banners").InsertMany(ctx, banners)
		fmt.Println("SeedStoreData: seeded 3 banners")
	}

	// ── Seed Products ─────────────────────────────────────────────────────────
	prodCount, _ := db.Collection("products").CountDocuments(ctx, bson.M{})
	if prodCount == 0 {
		products := []interface{}{
			bson.M{
				"_id": "prod_1", "title": "Shimla Premium Royal Delicious Apples", "slug": "shimla-apples",
				"category_id": "cat_fruits", "subcategory_id": "sub_apples",
				"description":     "Crisp, sweet, and juicy handpicked royal apples sourced directly from high-altitude orchards in Shimla.",
				"images":          []string{"https://images.unsplash.com/photo-1560806887-1e4cd0b6cbd6?w=600"},
				"shelf_life_days": 7, "storage_instructions": "Store in cool refrigeration.",
				"is_organic": false, "quality_grade": "Grade A Farm Fresh", "origin_region": "Shimla, HP",
				"avg_rating": 4.8, "review_count": 142, "is_featured": true,
				"variants": []bson.M{
					{"id": "v1_500g", "product_id": "prod_1", "weight_value": 500, "weight_unit": "g", "packaging_type": "Eco Tray", "mrp": 140, "selling_price": 110, "stock_quantity": 45, "sku": "APL-500G", "is_available": true},
					{"id": "v1_1kg", "product_id": "prod_1", "weight_value": 1, "weight_unit": "kg", "packaging_type": "Eco Box", "mrp": 260, "selling_price": 210, "stock_quantity": 30, "sku": "APL-1KG", "is_available": true},
					{"id": "v1_2kg", "product_id": "prod_1", "weight_value": 2, "weight_unit": "kg", "packaging_type": "Eco Box", "mrp": 500, "selling_price": 399, "stock_quantity": 15, "sku": "APL-2KG", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_2", "title": "Fresh Alphonso Mangoes (GI Tagged)", "slug": "ratnagiri-alphonso",
				"category_id": "cat_fruits", "subcategory_id": "sub_exotic",
				"description":     "Naturally ripened GI-tagged Ratnagiri Alphonso mangoes. Rich saffron pulp with sweet aroma.",
				"images":          []string{"https://images.unsplash.com/photo-1553279768-865429fa0078?w=600"},
				"shelf_life_days": 5, "storage_instructions": "Keep at room temp.",
				"is_organic": true, "quality_grade": "GI Tagged Premium", "origin_region": "Ratnagiri, MH",
				"avg_rating": 4.95, "review_count": 520, "is_featured": true,
				"variants": []bson.M{
					{"id": "v2_6pcs", "product_id": "prod_2", "weight_value": 6, "weight_unit": "pcs", "packaging_type": "Wooden Box", "mrp": 850, "selling_price": 699, "stock_quantity": 25, "sku": "MNG-6PCS", "is_available": true},
					{"id": "v2_12pcs", "product_id": "prod_2", "weight_value": 12, "weight_unit": "pcs", "packaging_type": "Wooden Box", "mrp": 1600, "selling_price": 1299, "stock_quantity": 15, "sku": "MNG-12PCS", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_3", "title": "Fresh California Strawberries", "slug": "california-strawberries",
				"category_id": "cat_fruits", "subcategory_id": "sub_citrus",
				"description":     "Plump, juicy strawberries. Rich in antioxidants and Vitamin C. Perfect for desserts.",
				"images":          []string{"https://images.unsplash.com/photo-1464965911861-746a04b4bca6?w=600"},
				"shelf_life_days": 3, "storage_instructions": "Refrigerate. Do not wash before storing.",
				"is_organic": true, "quality_grade": "Farm Fresh Grade A", "origin_region": "Mahabaleshwar, MH",
				"avg_rating": 4.6, "review_count": 203, "is_featured": true,
				"variants": []bson.M{
					{"id": "v3_250g", "product_id": "prod_3", "weight_value": 250, "weight_unit": "g", "packaging_type": "Punnet Box", "mrp": 180, "selling_price": 149, "stock_quantity": 50, "sku": "STR-250G", "is_available": true},
					{"id": "v3_500g", "product_id": "prod_3", "weight_value": 500, "weight_unit": "g", "packaging_type": "Punnet Box", "mrp": 340, "selling_price": 279, "stock_quantity": 30, "sku": "STR-500G", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_4", "title": "Organic Pesticide-Free Spinach (Palak)", "slug": "organic-spinach",
				"category_id": "cat_veggies", "subcategory_id": "sub_leafy",
				"description":     "Fresh hydroponic spinach leaves packed with Iron, Folate, and Vitamins. Certified organic.",
				"images":          []string{"https://images.unsplash.com/photo-1576045057995-568f588f82fb?w=600"},
				"shelf_life_days": 3, "storage_instructions": "Keep chilled.",
				"is_organic": true, "quality_grade": "Certified Organic", "origin_region": "Bengaluru, KA",
				"avg_rating": 4.7, "review_count": 89, "is_featured": true,
				"variants": []bson.M{
					{"id": "v4_250g", "product_id": "prod_4", "weight_value": 250, "weight_unit": "g", "packaging_type": "Breathable Pack", "mrp": 40, "selling_price": 28, "stock_quantity": 60, "sku": "SPN-250G", "is_available": true},
					{"id": "v4_500g", "product_id": "prod_4", "weight_value": 500, "weight_unit": "g", "packaging_type": "Breathable Pack", "mrp": 75, "selling_price": 52, "stock_quantity": 40, "sku": "SPN-500G", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_5", "title": "Farm Fresh Tomatoes (Hybrid)", "slug": "hybrid-tomatoes",
				"category_id": "cat_veggies", "subcategory_id": "sub_daily",
				"description":     "Plump, round, firm hybrid tomatoes. Perfect for cooking gravies, salads, and sandwiches.",
				"images":          []string{"https://images.unsplash.com/photo-1471194402529-8e0f5a675de6?w=600"},
				"shelf_life_days": 5, "storage_instructions": "Room temperature. Avoid direct sunlight.",
				"is_organic": false, "quality_grade": "Grade A+ Hybrid", "origin_region": "Nasik, MH",
				"avg_rating": 4.4, "review_count": 167, "is_featured": false,
				"variants": []bson.M{
					{"id": "v5_500g", "product_id": "prod_5", "weight_value": 500, "weight_unit": "g", "packaging_type": "Net Bag", "mrp": 35, "selling_price": 28, "stock_quantity": 200, "sku": "TOM-500G", "is_available": true},
					{"id": "v5_1kg", "product_id": "prod_5", "weight_value": 1, "weight_unit": "kg", "packaging_type": "Net Bag", "mrp": 65, "selling_price": 52, "stock_quantity": 150, "sku": "TOM-1KG", "is_available": true},
					{"id": "v5_2kg", "product_id": "prod_5", "weight_value": 2, "weight_unit": "kg", "packaging_type": "Net Bag", "mrp": 120, "selling_price": 95, "stock_quantity": 100, "sku": "TOM-2KG", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_6", "title": "Creamy Hass Avocados", "slug": "hass-avocados",
				"category_id": "cat_fruits", "subcategory_id": "sub_exotic",
				"description":     "Buttery, creamy Hass avocados. Nutrient-dense superfood. Perfect for guacamole and smoothies.",
				"images":          []string{"https://images.unsplash.com/photo-1519162808019-7de1683fa2ad?w=600"},
				"shelf_life_days": 4, "storage_instructions": "Ripen at room temp, refrigerate once ripe.",
				"is_organic": true, "quality_grade": "A-Grade Import", "origin_region": "Mexico / Coorg, KA",
				"avg_rating": 4.8, "review_count": 75, "is_featured": true,
				"variants": []bson.M{
					{"id": "v6_2pcs", "product_id": "prod_6", "weight_value": 2, "weight_unit": "pcs", "packaging_type": "Eco Wrap", "mrp": 120, "selling_price": 99, "stock_quantity": 40, "sku": "AVO-2PCS", "is_available": true},
					{"id": "v6_4pcs", "product_id": "prod_6", "weight_value": 4, "weight_unit": "pcs", "packaging_type": "Eco Wrap", "mrp": 220, "selling_price": 179, "stock_quantity": 25, "sku": "AVO-4PCS", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_7", "title": "W320 Jumbo Kernel Cashew Nuts (Kaju)", "slug": "jumbo-cashew-w320",
				"category_id": "cat_dry_nuts", "subcategory_id": "sub_almonds",
				"description":     "Whole jumbo Grade W320 cashew nuts. Crunchy, buttery, and rich in healthy fats and protein.",
				"images":          []string{"https://images.unsplash.com/photo-1508061252966-177209772242?w=600"},
				"shelf_life_days": 180, "storage_instructions": "Store in airtight vacuum container.",
				"is_organic": false, "quality_grade": "W320 Export Quality", "origin_region": "Mangaluru, KA",
				"avg_rating": 4.9, "review_count": 310, "is_featured": true,
				"variants": []bson.M{
					{"id": "v7_250g", "product_id": "prod_7", "weight_value": 250, "weight_unit": "g", "packaging_type": "Vacuum Zipper", "mrp": 320, "selling_price": 260, "stock_quantity": 100, "sku": "CAS-250G", "is_available": true},
					{"id": "v7_500g", "product_id": "prod_7", "weight_value": 500, "weight_unit": "g", "packaging_type": "Vacuum Zipper", "mrp": 620, "selling_price": 499, "stock_quantity": 80, "sku": "CAS-500G", "is_available": true},
					{"id": "v7_1kg", "product_id": "prod_7", "weight_value": 1, "weight_unit": "kg", "packaging_type": "Vacuum Tin", "mrp": 1200, "selling_price": 950, "stock_quantity": 40, "sku": "CAS-1KG", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_8", "title": "California Premium Almonds (Badam)", "slug": "california-almonds",
				"category_id": "cat_dry_nuts", "subcategory_id": "sub_almonds",
				"description":     "Extra-large, crunchy California almonds. High in Vitamin E, magnesium and protein. Raw and unsalted.",
				"images":          []string{"https://images.unsplash.com/photo-1574323347407-f5e1ad6d020b?w=600"},
				"shelf_life_days": 365, "storage_instructions": "Store in cool dry place.",
				"is_organic": false, "quality_grade": "California Grade AA", "origin_region": "USA / Gujarat",
				"avg_rating": 4.7, "review_count": 445, "is_featured": false,
				"variants": []bson.M{
					{"id": "v8_250g", "product_id": "prod_8", "weight_value": 250, "weight_unit": "g", "packaging_type": "Zip Pouch", "mrp": 280, "selling_price": 230, "stock_quantity": 120, "sku": "ALM-250G", "is_available": true},
					{"id": "v8_500g", "product_id": "prod_8", "weight_value": 500, "weight_unit": "g", "packaging_type": "Zip Pouch", "mrp": 540, "selling_price": 445, "stock_quantity": 90, "sku": "ALM-500G", "is_available": true},
					{"id": "v8_1kg", "product_id": "prod_8", "weight_value": 1, "weight_unit": "kg", "packaging_type": "Gift Box", "mrp": 1050, "selling_price": 870, "stock_quantity": 50, "sku": "ALM-1KG", "is_available": true},
				},
			},
			bson.M{
				"_id": "prod_9", "title": "Navel Oranges - Seedless & Juicy", "slug": "navel-oranges",
				"category_id": "cat_fruits", "subcategory_id": "sub_citrus",
				"description":     "Large, seedless Navel oranges. Bursting with Vitamin C and natural sweetness. Excellent juicing variety.",
				"images":          []string{"https://images.unsplash.com/photo-1547514701-42782101795e?w=600"},
				"shelf_life_days": 10, "storage_instructions": "Store in cool place or refrigerate.",
				"is_organic": false, "quality_grade": "Grade A Premium", "origin_region": "Nagpur, MH",
				"avg_rating": 4.6, "review_count": 188, "is_featured": true,
				"variants": []bson.M{
					{"id": "v9_4pcs", "product_id": "prod_9", "weight_value": 4, "weight_unit": "pcs", "packaging_type": "Net Bag", "mrp": 80, "selling_price": 65, "stock_quantity": 100, "sku": "ONG-4PCS", "is_available": true},
					{"id": "v9_1kg", "product_id": "prod_9", "weight_value": 1, "weight_unit": "kg", "packaging_type": "Net Bag", "mrp": 120, "selling_price": 95, "stock_quantity": 80, "sku": "ONG-1KG", "is_available": true},
					{"id": "v9_3kg", "product_id": "prod_9", "weight_value": 3, "weight_unit": "kg", "packaging_type": "Net Bag", "mrp": 340, "selling_price": 269, "stock_quantity": 40, "sku": "ONG-3KG", "is_available": true},
				},
			},
		}
		db.Collection("products").InsertMany(ctx, products)
		fmt.Printf("SeedStoreData: seeded %d products\n", len(products))
	}

	// ── Seed Marketplace Posts ─────────────────────────────────────────────────
	postCount, _ := db.Collection("post").CountDocuments(ctx, bson.M{})
	if postCount == 0 {
		posts := []interface{}{
			bson.M{
				"_id": "post_req_001", "post_type": "requirements", "buyerId": "usr_buyer_seed_003",
				"grade": "A", "yearOfCrop": "2026", "requiredqty": int32(500),
				"status": "Active", "viewed": []string{}, "favorite": []string{},
				"created_on": time.Now().UTC(), "created_by": "usr_buyer_seed_003",
			},
			bson.M{
				"_id": "post_stk_001", "post_type": "stocks", "sellerId": "usr_seller_seed_002",
				"grade": "B+", "yearOfCrop": "2026", "availableqty": int32(1000),
				"status": "Active", "viewed": []string{}, "favorite": []string{},
				"created_on": time.Now().UTC(), "created_by": "usr_seller_seed_002",
			},
		}
		db.Collection("post").InsertMany(ctx, posts)
		fmt.Println("SeedStoreData: seeded marketplace posts")
	}

	// ── Seed Settings ─────────────────────────────────────────────────────────
	settingsCount, _ := db.Collection("settings").CountDocuments(ctx, bson.M{})
	if settingsCount == 0 {
		db.Collection("settings").InsertOne(ctx, bson.M{
			"_id": "settings_001", "pointratio": int32(10), "moneyratio": int32(1),
			"postdetectionpoint": int32(5), "enquiresdetectionpoint": int32(2),
			"setreward": true, "rewardpoint": int32(50),
		})
		fmt.Println("SeedStoreData: seeded default settings")
	}

	fmt.Println("SeedStoreData: seeding complete")
}
