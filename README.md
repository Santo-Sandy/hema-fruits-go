# 🥭 Hema Fruits - Go Fiber Backend REST API

A high-performance backend REST API built with **Go** and the **Fiber v2** web framework, powering the Hema Fruits e-commerce platform for Fresh Fruits, Vegetables, and Dry Nuts.

---

## 🚀 API Endpoint Features

- **Store Catalog**: Category tree, subcategories, paginated product listings with weight variant breakdowns, organic tags, and promo banners.
- **Cart Engine**: In-memory & DB-backed cart state, item quantity adjustments, coupon validation (`FRESH100`).
- **Express Checkout**: Pincode serviceability check, delivery slot allocation (Express 2-Hours, Morning, Evening).
- **Order State Machine**: Order creation, payment verification (UPI, Card, COD), OTP delivery confirmation, and order tracking.
- **Return Processing**: 24-hour perishable damaged item return request submission with photo proof handling.

---

## 🛠️ Tech Stack & Dependencies

- **Language**: Go 1.20+
- **HTTP Framework**: [Fiber v2](https://gofiber.io)
- **Database**: MongoDB (via official `mongo-driver`)
- **Environment Management**: `godotenv`
- **CORS & Middleware**: Fiber Recover, Logger, CORS, Auth JWT

---

## 📂 Project Structure

```
hema-fruits-go/
├── cmd/
│   └── admin-service/
│       ├── .env                  # Port & MongoDB connection string
│       └── main.go               # Server entrypoint & Fiber route setup
├── pkg/
│   ├── config/                   # MongoDB connection pool & init
│   ├── handlers/                 # Store, Auth, Payment & CRUD Handlers (store.go, auth.go, etc.)
│   └── middleware/             # Auth JWT & CORS middleware
├── go.mod                        # Go module definition
└── README.md                     # Backend documentation
```

---

## 🚦 Running the Backend Server

1. **Navigate to the Backend Directory**:
   ```bash
   cd hema-fruits-go
   ```

2. **Verify Dependencies**:
   ```bash
   go mod tidy
   ```

3. **Build the Application**:
   ```bash
   go build ./...
   ```

4. **Run the Admin Service**:
   ```bash
   go run cmd/admin-service/main.go
   ```

---

## 📄 License
Proprietary software for Hema Fruits E-Commerce.
