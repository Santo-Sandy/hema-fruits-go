package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	MongoClient *mongo.Client
	DB          *mongo.Database
	JWTSecret   string
)

// InitDB initializes connection to MongoDB
func InitDB() {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		log.Fatal("MONGODB_URI environment variable is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(uri)
	var err error
	MongoClient, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	err = MongoClient.Ping(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}

	dbName := os.Getenv("MONGODB_DB_NAME")
	if dbName == "" {
		dbName = "teamalpha"
	}

	DB = MongoClient.Database(dbName)
	fmt.Printf("Successfully connected to MongoDB database: %s\n", dbName)

	JWTSecret = os.Getenv("JWT_SECRET")
	if JWTSecret == "" {
		JWTSecret = "HEMA_FRUITS_SECURE_JWT_SECRET_KEY_2026"
	}
}

// GetDB returns standard DB connection
func GetDB() *mongo.Database {
	return DB
}

// GetJWTSecret returns JWT signing secret
func GetJWTSecret() []byte {
	return []byte(JWTSecret)
}
