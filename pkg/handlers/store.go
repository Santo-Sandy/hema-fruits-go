package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

type Subcategory struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IconURL   string `json:"icon_url"`
	ItemCount int    `json:"item_count"`
}

type Category struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Slug          string        `json:"slug"`
	IconURL       string        `json:"icon_url"`
	BannerURL     string        `json:"banner_url"`
	Subcategories []Subcategory `json:"subcategories"`
	DisplayOrder  int           `json:"display_order"`
	IsActive      bool          `json:"is_active"`
}

type ProductVariant struct {
	ID            string  `json:"id"`
	ProductID     string  `json:"product_id"`
	WeightValue   float64 `json:"weight_value"`
	WeightUnit    string  `json:"weight_unit"`
	PackagingType string  `json:"packaging_type"`
	MRP           float64 `json:"mrp"`
	SellingPrice  float64 `json:"selling_price"`
	StockQuantity int     `json:"stock_quantity"`
	SKU           string  `json:"sku"`
	IsAvailable   bool    `json:"is_available"`
}

type Product struct {
	ID                  string           `json:"id"`
	Title               string           `json:"title"`
	Slug                string           `json:"slug"`
	CategoryID          string           `json:"category_id"`
	SubcategoryID       string           `json:"subcategory_id"`
	Description         string           `json:"description"`
	Images              []string         `json:"images"`
	ShelfLifeDays       int              `json:"shelf_life_days"`
	StorageInstructions string           `json:"storage_instructions"`
	IsOrganic           bool             `json:"is_organic"`
	QualityGrade        string           `json:"quality_grade"`
	OriginRegion        string           `json:"origin_region"`
	Variants            []ProductVariant `json:"variants"`
	AvgRating           float64          `json:"avg_rating"`
	ReviewCount         int              `json:"review_count"`
	IsFeatured          bool             `json:"is_featured"`
}

type StoreBanner struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	ImageURL       string `json:"image_url"`
	TargetCategory string `json:"target_category"`
	ActionURL      string `json:"action_url"`
}

type CartItem struct {
	ProductID    string  `json:"product_id"`
	VariantID    string  `json:"variant_id"`
	ProductTitle string  `json:"product_title"`
	ImageURL     string  `json:"image_url"`
	WeightValue  float64 `json:"weight_value"`
	WeightUnit   string  `json:"weight_unit"`
	UnitPrice    float64 `json:"unit_price"`
	MRP          float64 `json:"mrp"`
	Quantity     int     `json:"quantity"`
	TotalPrice   float64 `json:"total_price"`
	IsPerishable bool    `json:"is_perishable"`
}

type Cart struct {
	UserID            string     `json:"user_id"`
	Items             []CartItem `json:"items"`
	ItemTotal         float64    `json:"item_total"`
	TotalMRP          float64    `json:"total_mrp"`
	DiscountTotal     float64    `json:"discount_total"`
	DeliveryFee       float64    `json:"delivery_fee"`
	PackagingFee      float64    `json:"packaging_fee"`
	AppliedCouponCode string     `json:"applied_coupon"`
	CouponDiscount    float64    `json:"coupon_discount"`
	GrandTotal        float64    `json:"grand_total"`
}

type DeliverySlot struct {
	SlotID    string `json:"slot_id"`
	DateStr   string `json:"date_str"`
	TimeRange string `json:"time_range"`
}

type Order struct {
	ID                 string     `json:"id"`
	OrderNumber        string     `json:"order_number"`
	UserID             string     `json:"user_id"`
	Items              []CartItem `json:"items"`
	PaymentMethod      string     `json:"payment_method"`
	OrderStatus        string     `json:"order_status"`
	GrandTotal         float64    `json:"grand_total"`
	DeliveryOTP        string     `json:"delivery_otp"`
	DeliveryAgentName  string     `json:"delivery_agent_name"`
	DeliveryAgentPhone string     `json:"delivery_agent_phone"`
	PlacedAt           time.Time  `json:"placed_at"`
}

var userCarts = make(map[string]*Cart)
var storeOrders = make(map[string]*Order)

func SetupStoreRoutes(app *fiber.App) {
	api := app.Group("/api/v1/store")

	api.Get("/categories", GetCategoriesHandler)
	api.Get("/products", GetProductsHandler)
	api.Get("/products/:id", GetProductByIDHandler)
	api.Get("/banners", GetBannersHandler)

	api.Get("/cart", GetCartHandler)
	api.Post("/cart/item", AddOrUpdateCartItemHandler)
	api.Delete("/cart/item/:variantId", RemoveCartItemHandler)
	api.Post("/cart/coupon", ApplyCouponHandler)

	api.Post("/checkout/validate", ValidateCheckoutHandler)
	api.Post("/orders", CreateOrderHandler)
	api.Get("/orders", GetUserOrdersHandler)
	api.Get("/orders/:id", GetOrderByIDHandler)
}

func GetCategoriesHandler(c *fiber.Ctx) error {
	categories := []Category{
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
	return c.JSON(fiber.Map{"success": true, "categories": categories})
}

func GetProductsHandler(c *fiber.Ctx) error {
	products := getMockProducts()
	return c.JSON(fiber.Map{"success": true, "products": products})
}

func GetProductByIDHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	for _, p := range getMockProducts() {
		if p.ID == id {
			return c.JSON(fiber.Map{"success": true, "product": p})
		}
	}
	return c.Status(404).JSON(fiber.Map{"success": false, "message": "Product not found"})
}

func GetBannersHandler(c *fiber.Ctx) error {
	banners := []StoreBanner{
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
	return c.JSON(fiber.Map{"success": true, "banners": banners})
}

func GetCartHandler(c *fiber.Ctx) error {
	userID := "demo_user"
	cart, exists := userCarts[userID]
	if !exists {
		cart = &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
		userCarts[userID] = cart
	}
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func AddOrUpdateCartItemHandler(c *fiber.Ctx) error {
	userID := "demo_user"
	var item CartItem
	if err := c.BodyParser(&item); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "message": err.Error()})
	}
	cart, exists := userCarts[userID]
	if !exists {
		cart = &Cart{UserID: userID, Items: []CartItem{}, DeliveryFee: 35, PackagingFee: 15}
		userCarts[userID] = cart
	}
	cart.Items = append(cart.Items, item)
	return c.JSON(fiber.Map{"success": true, "cart": cart})
}

func RemoveCartItemHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true})
}

func ApplyCouponHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "discount": 100})
}

func ValidateCheckoutHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "serviceable": true})
}

func CreateOrderHandler(c *fiber.Ctx) error {
	order := &Order{
		ID:                 "ord_101",
		OrderNumber:        "HEMA-FRESH-9921",
		UserID:             "demo_user",
		PaymentMethod:      "UPI",
		OrderStatus:        "PLACED",
		GrandTotal:         480,
		DeliveryOTP:        "7194",
		DeliveryAgentName:  "Ramesh (Express Delivery)",
		DeliveryAgentPhone: "+91 98123 45678",
		PlacedAt:           time.Now(),
	}
	storeOrders[order.ID] = order
	return c.JSON(fiber.Map{"success": true, "order": order})
}

func GetUserOrdersHandler(c *fiber.Ctx) error {
	var list []Order
	for _, o := range storeOrders {
		list = append(list, *o)
	}
	return c.JSON(fiber.Map{"success": true, "orders": list})
}

func GetOrderByIDHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	if o, exists := storeOrders[id]; exists {
		return c.JSON(fiber.Map{"success": true, "order": o})
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
