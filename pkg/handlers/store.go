package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"hema-fruits-go/pkg/config"
	"hema-fruits-go/pkg/middleware"
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

var userCarts = make(map[string]*Cart)
var storeOrders = make(map[string]*Order)

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
}

// ─────────────────────────────────────────────────────────────────────────────
// CATEGORY HANDLERS
// ─────────────────────────────────────────────────────────────────────────────

func GetCategoriesHandler(c *fiber.Ctx) error {
	db := config.GetDB()
	var categories []Category
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cursor, err := db.Collection("categories").Find(ctx, bson.M{})
		if err == nil {
			cursor.All(ctx, &categories)
		}
	}
	if len(categories) == 0 {
		categories = getMockCategories()
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
	search := strings.ToLower(c.Query("search"))
	organic := c.Query("organic")

	db := config.GetDB()
	var products []Product
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		filter := bson.M{}
		if catID != "" {
			filter["category_id"] = catID
		}
		if organic == "true" {
			filter["is_organic"] = true
		}
		if search != "" {
			filter["title"] = bson.M{"$regex": search, "$options": "i"}
		}
		cursor, err := db.Collection("products").Find(ctx, filter)
		if err == nil {
			cursor.All(ctx, &products)
		}
	}

	if len(products) == 0 {
		products = getMockProducts()
		if catID != "" {
			filtered := []Product{}
			for _, p := range products {
				if p.CategoryID == catID {
					filtered = append(filtered, p)
				}
			}
			products = filtered
		}
		if organic == "true" {
			filtered := []Product{}
			for _, p := range products {
				if p.IsOrganic {
					filtered = append(filtered, p)
				}
			}
			products = filtered
		}
		if search != "" {
			filtered := []Product{}
			for _, p := range products {
				if strings.Contains(strings.ToLower(p.Title), search) || strings.Contains(strings.ToLower(p.Description), search) {
					filtered = append(filtered, p)
				}
			}
			products = filtered
		}
	}
	return c.JSON(fiber.Map{"success": true, "products": products})
}

func GetProductByIDHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var p Product
		err := db.Collection("products").FindOne(ctx, bson.M{"_id": id}).Decode(&p)
		if err == nil {
			return c.JSON(fiber.Map{"success": true, "product": p})
		}
	}

	for _, p := range getMockProducts() {
		if p.ID == id {
			return c.JSON(fiber.Map{"success": true, "product": p})
		}
	}
	return c.Status(404).JSON(fiber.Map{"success": false, "message": "Product not found"})
}

func CreateProductHandler(c *fiber.Ctx) error {
	var prod Product
	if err := c.BodyParser(&prod); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	if prod.ID == "" {
		prod.ID = "prod_" + GenerateUniqueKey()
	}
	db := config.GetDB()
	if db != nil {
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
	var banners []StoreBanner
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cursor, err := db.Collection("banners").Find(ctx, bson.M{})
		if err == nil {
			cursor.All(ctx, &banners)
		}
	}
	if len(banners) == 0 {
		banners = getMockBanners()
	}
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
		db.Collection("banners").InsertOne(ctx, b)
	}
	return c.JSON(fiber.Map{"success": true, "banner": b})
}

func DeleteBannerHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("banners").DeleteOne(ctx, bson.M{"_id": id})
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
		userID = "demo_user"
	}

	cart, exists := userCarts[userID]
	if !exists {
		cart = &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
		userCarts[userID] = cart
	}
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func AddOrUpdateCartItemHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		userID = "demo_user"
	}

	var item CartItem
	if err := c.BodyParser(&item); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	item.TotalPrice = item.UnitPrice * float64(item.Quantity)

	cart, exists := userCarts[userID]
	if !exists {
		cart = &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
		userCarts[userID] = cart
	}

	found := false
	for i, existing := range cart.Items {
		if existing.VariantID == item.VariantID {
			cart.Items[i].Quantity = item.Quantity
			cart.Items[i].TotalPrice = item.UnitPrice * float64(item.Quantity)
			if cart.Items[i].Quantity <= 0 {
				cart.Items = append(cart.Items[:i], cart.Items[i+1:]...)
			}
			found = true
			break
		}
	}
	if !found && item.Quantity > 0 {
		cart.Items = append(cart.Items, item)
	}

	recalculateCart(cart)
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func RemoveCartItemHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		userID = "demo_user"
	}

	variantID := c.Params("variantId")
	cart, exists := userCarts[userID]
	if exists {
		var updated []CartItem
		for _, item := range cart.Items {
			if item.VariantID != variantID {
				updated = append(updated, item)
			}
		}
		cart.Items = updated
		recalculateCart(cart)
	}
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func ClearCartHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		userID = "demo_user"
	}
	cart := &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
	userCarts[userID] = cart
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func ApplyCouponHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		userID = "demo_user"
	}

	var req struct {
		Coupon string `json:"coupon"`
	}
	c.BodyParser(&req)

	cart, exists := userCarts[userID]
	if !exists {
		cart = &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
		userCarts[userID] = cart
	}

	if req.Coupon == "FRESH100" {
		cart.AppliedCouponCode = "FRESH100"
		cart.CouponDiscount = 100
	} else if req.Coupon == "FRESH50" {
		cart.AppliedCouponCode = "FRESH50"
		cart.CouponDiscount = 50
	} else {
		cart.AppliedCouponCode = ""
		cart.CouponDiscount = 0
	}

	recalculateCart(cart)
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
		userID = "demo_user"
	}

	var order Order
	c.BodyParser(&order)

	if order.ID == "" {
		order.ID = "ord_" + GenerateUniqueKey()
	}
	if order.OrderNumber == "" {
		order.OrderNumber = fmt.Sprintf("HEMA-FRESH-%d", time.Now().Unix()%10000)
	}
	order.UserID = userID
	order.OrderStatus = "PLACED"
	order.DeliveryOTP = "7194"
	order.DeliveryAgentName = "Ramesh (Express Delivery)"
	order.DeliveryAgentPhone = "+91 98123 45678"
	order.PlacedAt = time.Now()

	storeOrders[order.ID] = &order
	userCarts[userID] = &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}

	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("orders").InsertOne(ctx, order)
	}
	return c.JSON(fiber.Map{"success": true, "order": order})
}

func GetUserOrdersHandler(c *fiber.Ctx) error {
	userToken := middleware.GetUserTokenValue(c)
	userID := userToken.UserId
	if userID == "" {
		userID = "demo_user"
	}

	db := config.GetDB()
	var list []Order
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cursor, err := db.Collection("orders").Find(ctx, bson.M{"user_id": userID}, options.Find().SetSort(bson.D{{Key: "placed_at", Value: -1}}))
		if err == nil {
			cursor.All(ctx, &list)
		}
	}
	if len(list) == 0 {
		for _, o := range storeOrders {
			if o.UserID == userID || userID == "demo_user" {
				list = append(list, *o)
			}
		}
	}
	return c.JSON(fiber.Map{"success": true, "orders": list})
}

func GetOrderByIDHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	if o, exists := storeOrders[id]; exists {
		return c.JSON(fiber.Map{"success": true, "order": o})
	}
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var order Order
		err := db.Collection("orders").FindOne(ctx, bson.M{"_id": id}).Decode(&order)
		if err == nil {
			return c.JSON(fiber.Map{"success": true, "order": order})
		}
	}
	return c.JSON(fiber.Map{"success": true, "order": Order{
		ID:                 id,
		OrderNumber:        "HEMA-FRESH-9921",
		UserID:             "demo_user",
		PaymentMethod:      "UPI",
		OrderStatus:        "OUT_FOR_DELIVERY",
		GrandTotal:         480,
		DeliveryOTP:        "7194",
		DeliveryAgentName:  "Ramesh (Express Delivery)",
		DeliveryAgentPhone: "+91 98123 45678",
		PlacedAt:           time.Now(),
	}})
}

func UpdateOrderStatusHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	var req struct {
		Status string `json:"status"`
	}
	c.BodyParser(&req)

	if o, exists := storeOrders[id]; exists {
		o.OrderStatus = req.Status
	}
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("orders").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"order_status": req.Status}})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Order status updated"})
}

func DeleteOrderHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	delete(storeOrders, id)
	db := config.GetDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		db.Collection("orders").DeleteOne(ctx, bson.M{"_id": id})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Order deleted"})
}

// ─────────────────────────────────────────────────────────────────────────────
// SEED MOCK DATA HELPERS
// ─────────────────────────────────────────────────────────────────────────────

func getMockCategories() []Category {
	return []Category{
		{
			ID:           "cat_fruits",
			Name:         "Fresh Fruits",
			Slug:         "fresh-fruits",
			IconURL:      "https://images.unsplash.com/photo-1619566636858-adf3ef46400b?w=200",
			BannerURL:    "https://images.unsplash.com/photo-1619566636858-adf3ef46400b?w=800",
			DisplayOrder: 1,
			IsActive:     true,
			Subcategories: []Subcategory{
				{ID: "sub_apples", Name: "Apples & Pears", Slug: "apples-pears", ItemCount: 12},
				{ID: "sub_citrus", Name: "Oranges & Citrus", Slug: "oranges-citrus", ItemCount: 8},
				{ID: "sub_exotic", Name: "Exotic Fruits", Slug: "exotic-fruits", ItemCount: 15},
			},
		},
		{
			ID:           "cat_veggies",
			Name:         "Fresh Vegetables",
			Slug:         "fresh-vegetables",
			IconURL:      "https://images.unsplash.com/photo-1540420773420-3366772f4999?w=200",
			BannerURL:    "https://images.unsplash.com/photo-1540420773420-3366772f4999?w=800",
			DisplayOrder: 2,
			IsActive:     true,
			Subcategories: []Subcategory{
				{ID: "sub_leafy", Name: "Leafy Greens", Slug: "leafy-greens", ItemCount: 18},
				{ID: "sub_daily", Name: "Daily Essentials", Slug: "daily-essentials", ItemCount: 25},
			},
		},
		{
			ID:           "cat_dry_nuts",
			Name:         "Dry Fruits & Nuts",
			Slug:         "dry-fruits-nuts",
			IconURL:      "https://images.unsplash.com/photo-1508061252966-177209772242?w=200",
			BannerURL:    "https://images.unsplash.com/photo-1508061252966-177209772242?w=800",
			DisplayOrder: 3,
			IsActive:     true,
			Subcategories: []Subcategory{
				{ID: "sub_almonds", Name: "Almonds & Cashews", Slug: "almonds-cashews", ItemCount: 20},
				{ID: "sub_walnuts", Name: "Walnuts & Pistachios", Slug: "walnuts-pistachios", ItemCount: 16},
			},
		},
	}
}

func getMockBanners() []StoreBanner {
	return []StoreBanner{
		{
			ID:             "b1",
			Title:          "Farm Fresh Mangoes & Berries",
			Subtitle:       "Up to 30% OFF | Handpicked Daily",
			ImageURL:       "https://images.unsplash.com/photo-1553279768-865429fa0078?w=1000",
			TargetCategory: "cat_fruits",
		},
		{
			ID:             "b2",
			Title:          "100% Organic Pesticide-Free Greens",
			Subtitle:       "Direct from Certified Farmers",
			ImageURL:       "https://images.unsplash.com/photo-1540420773420-3366772f4999?w=1000",
			TargetCategory: "cat_veggies",
		},
		{
			ID:             "b3",
			Title:          "Premium Jumbo Cashews & Walnuts",
			Subtitle:       "Rich in Nutrients | Vacuum Packed",
			ImageURL:       "https://images.unsplash.com/photo-1508061252966-177209772242?w=1000",
			TargetCategory: "cat_dry_nuts",
		},
	}
}

func getMockProducts() []Product {
	return []Product{
		{
			ID:                  "prod_1",
			Title:               "Shimla Premium Royal Delicious Apples",
			Slug:                "shimla-apples",
			CategoryID:          "cat_fruits",
			SubcategoryID:       "sub_apples",
			Description:         "Crisp, sweet, and juicy handpicked royal apples sourced directly from Shimla orchards.",
			Images:              []string{"https://images.unsplash.com/photo-1560806887-1e4cd0b6cbd6?w=600"},
			ShelfLifeDays:       7,
			StorageInstructions: "Store in cool refrigeration.",
			IsOrganic:           false,
			QualityGrade:        "Grade A Farm Fresh",
			OriginRegion:        "Shimla, HP",
			AvgRating:           4.8,
			ReviewCount:         142,
			IsFeatured:          true,
			Variants: []ProductVariant{
				{ID: "v1_500g", ProductID: "prod_1", WeightValue: 500, WeightUnit: "g", PackagingType: "Eco Tray", MRP: 140, SellingPrice: 110, StockQuantity: 45, SKU: "APL-500G", IsAvailable: true},
				{ID: "v1_1kg", ProductID: "prod_1", WeightValue: 1, WeightUnit: "kg", PackagingType: "Eco Box", MRP: 260, SellingPrice: 210, StockQuantity: 30, SKU: "APL-1KG", IsAvailable: true},
			},
		},
		{
			ID:                  "prod_2",
			Title:               "Organic Pesticide-Free Spinach (Palak)",
			Slug:                "organic-spinach",
			CategoryID:          "cat_veggies",
			SubcategoryID:       "sub_leafy",
			Description:         "Fresh hydroponic spinach leaves packed with Iron, Foliate, and Vitamins.",
			Images:              []string{"https://images.unsplash.com/photo-1576045057995-568f588f82fb?w=600"},
			ShelfLifeDays:       3,
			StorageInstructions: "Keep chilled.",
			IsOrganic:           true,
			QualityGrade:        "Certified Organic",
			OriginRegion:        "Bengaluru, KA",
			AvgRating:           4.7,
			ReviewCount:         89,
			IsFeatured:          true,
			Variants: []ProductVariant{
				{ID: "v2_250g", ProductID: "prod_2", WeightValue: 250, WeightUnit: "g", PackagingType: "Breathable Pack", MRP: 40, SellingPrice: 28, StockQuantity: 60, SKU: "SPN-250G", IsAvailable: true},
				{ID: "v2_500g", ProductID: "prod_2", WeightValue: 500, WeightUnit: "g", PackagingType: "Breathable Pack", MRP: 75, SellingPrice: 52, StockQuantity: 40, SKU: "SPN-500G", IsAvailable: true},
			},
		},
		{
			ID:                  "prod_3",
			Title:               "W320 Jumbo Kernel Cashew Nuts (Kaju)",
			Slug:                "jumbo-cashew-w320",
			CategoryID:          "cat_dry_nuts",
			SubcategoryID:       "sub_almonds",
			Description:         "Whole jumbo Grade W320 cashew nuts. Crunchy, buttery, and rich in healthy fats.",
			Images:              []string{"https://images.unsplash.com/photo-1508061252966-177209772242?w=600"},
			ShelfLifeDays:       180,
			StorageInstructions: "Store in airtight vacuum container.",
			IsOrganic:           false,
			QualityGrade:        "W320 Export Quality",
			OriginRegion:        "Mangaluru, KA",
			AvgRating:           4.9,
			ReviewCount:         310,
			IsFeatured:          true,
			Variants: []ProductVariant{
				{ID: "v3_250g", ProductID: "prod_3", WeightValue: 250, WeightUnit: "g", PackagingType: "Vacuum Zipper", MRP: 320, SellingPrice: 260, StockQuantity: 100, SKU: "CAS-250G", IsAvailable: true},
				{ID: "v3_500g", ProductID: "prod_3", WeightValue: 500, WeightUnit: "g", PackagingType: "Vacuum Zipper", MRP: 620, SellingPrice: 499, StockQuantity: 80, SKU: "CAS-500G", IsAvailable: true},
			},
		},
	}
}
