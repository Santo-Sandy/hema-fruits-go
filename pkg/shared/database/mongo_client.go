package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ConnObject struct {
	OrgId  string `json:"org_id" bson:"org_id"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	DbName string `json:"db_name" bson:"db_name"`
	UserId string `json:"user_id" bson:"user_id"`
	Pwd    string `json:"pwd"`
}

var ctx = context.Background()
var MongoClient *mongo.Client
var DBError error

var DBConnections = make(map[string]*mongo.Database)

// By default create shared db connection
var SharedDB *mongo.Database

func Init() {
	SharedDB = CreateDBConnection(GetenvStr("MONGO_SHAREDDB_HOST"), GetenvInt("MONGO_SHAREDDB_PORT"), GetenvStr("MONGO_SHAREDDB_NAME"), GetenvStr("MONGO_SHAREDDB_USER"), GetenvStr("MONGO_SHAREDDB_PASSWORD"))
}

func GetConnection(orgId string) *mongo.Database {
	if connection, exists := DBConnections[orgId]; exists {
		return connection
	}
	fmt.Println(orgId)

	var config ConnObject
	err := SharedDB.Collection("db_config").FindOne(ctx, bson.M{"org_id": orgId, "status": "A"}).Decode(&config)

	// If _demo orgId not found, look up base org config and use _demo db_name
	if err != nil && strings.HasSuffix(orgId, "_demo") {
		baseOrgId := strings.TrimSuffix(orgId, "_demo")
		err = SharedDB.Collection("db_config").FindOne(ctx, bson.M{"org_id": baseOrgId, "status": "A"}).Decode(&config)
		if err == nil {
			// Use same connection but point to _demo database
			config.OrgId = orgId
			config.DbName = strings.ToLower(orgId)
		}
	}

	fmt.Println(config)
	if err != nil {
		return SharedDB
	}

	DBConnections[orgId] = CreateDBConnection(config.Host, config.Port, config.DbName, config.UserId, config.Pwd)
	return DBConnections[orgId]
}

// func CreateNewMongoDatabase(dbName string) (string, error) {
// 	host := GetenvStr("MONGO_SHAREDDB_HOST")
// 	port := GetenvInt("MONGO_SHAREDDB_PORT")
// 	user := GetenvStr("MONGO_SHAREDDB_USER")
// 	password := GetenvStr("MONGO_SHAREDDB_PASSWORD")

// 	// uri := fmt.Sprintf("mongodb://%s:%s@%s:%d", user, password, host, port)
// 	uri := fmt.Sprintf("mongodb+srv://%s:%s@%s", user, password, host)
// 	clientOptions := options.Client().ApplyURI(uri)

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	client, err := mongo.Connect(ctx, clientOptions)
// 	if err != nil {
// 		return "", fmt.Errorf("mongo connect error: %v", err)
// 	}

// 	// Ping to test connection
// 	if err := client.Ping(ctx, nil); err != nil {
// 		return "", fmt.Errorf("mongo ping error: %v", err)
// 	}

// 	// Create dummy collection to trigger DB creation
// 	db := client.Database(dbName)
// 	dummyCollection := db.Collection("init_collection")
// 	_, err = dummyCollection.InsertOne(ctx, bson.M{"created_at": time.Now()})
// 	if err != nil {
// 		return "", fmt.Errorf("insert dummy doc failed: %v", err)
// 	}

// 	Id := uuid.New().String()
// 	dbConfig := map[string]interface{}{
// 		"_id":     Id,
// 		"org_id":  "",
// 		"host":    host,
// 		"port":    port,
// 		"pwd":     password,
// 		"user_id": user,
// 		"db_name": dbName,
// 		"status":  "A",
// 	}

// 	GetConnection("shared").Collection("db_config").InsertOne(context.Background(), dbConfig)

// 	return Id, nil
// }

func CreateNewMongoDatabase(dbName string, orgId string) (string, error) {
	host := GetenvStr("MONGO_SHAREDDB_HOST")
	port := GetenvInt("MONGO_SHAREDDB_PORT")
	user := GetenvStr("MONGO_SHAREDDB_USER")
	password := GetenvStr("MONGO_SHAREDDB_PASSWORD")

	uri := fmt.Sprintf("mongodb+srv://%s:%s@%s", user, password, host)
	clientOptions := options.Client().ApplyURI(uri)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return "", fmt.Errorf("mongo connect error: %v", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return "", fmt.Errorf("mongo ping error: %v", err)
	}

	newDB := client.Database(dbName)

	// Create dummy collection to initialize the new DB
	dummyCollection := newDB.Collection("init_collection")
	_, err = dummyCollection.InsertOne(ctx, bson.M{"created_at": time.Now()})
	if err != nil {
		return "", fmt.Errorf("insert dummy doc failed: %v", err)
	}

	id := uuid.New().String()
	
	// Use Upsert to ensure we only have one active configuration per organization
	filter := bson.M{"org_id": orgId}
	update := bson.M{
		"$set": bson.M{
			"org_id":  orgId,
			"host":    host,
			"port":    port,
			"pwd":     password,
			"user_id": user,
			"db_name": dbName,
			"status":  "A",
		},
		"$setOnInsert": bson.M{
			"_id": id,
		},
	}
	_, err = GetConnection("shared").Collection("db_config").UpdateOne(context.Background(), filter, update, options.Update().SetUpsert(true))
	if err != nil {
		return "", fmt.Errorf("failed to register database configuration: %v", err)
	}

	// CRITICAL: Clear any cached connection for this orgId so that subsequent GetConnection calls
	// fetch the new configuration instead of a stale one (like SharedDB fallback).
	delete(DBConnections, orgId)

	return id, nil
}

func copyCollection(ctx context.Context, fromDB, toDB *mongo.Database, colName string) error {
	fromCol := fromDB.Collection(colName)
	toCol := toDB.Collection(colName)

	cursor, err := fromCol.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []interface{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return fmt.Errorf("decode error: %w", err)
		}
		docs = append(docs, doc)
	}

	if len(docs) > 0 {
		_, err = toCol.InsertMany(ctx, docs)
		if err != nil {
			return fmt.Errorf("insert many error: %w", err)
		}
	}

	return nil
}

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
)

func CreateDBConnection(host string, port int, dbName string, userid string, pwd string) *mongo.Database {
	// dbUrl := fmt.Sprintf("mongodb://%s:%s@%s:%d/?retryWrites=true&authSource=admin&w=majority&authMechanism=SCRAM-SHA-256", userid, pwd, host, port)
	//dbUrl := fmt.Sprintf("mongodb://%s:%s@%s:%d/%s?retryWrites=true&w=majority&authMechanism=SCRAM-SHA-256", userid, pwd, host, port, dbName)
	dbUrl := fmt.Sprintf("mongodb+srv://%s:%s@%s", userid, pwd, host)
	// cmdMonitor := &event.CommandMonitor{
	// 	Started: func(_ context.Context, evt *event.CommandStartedEvent) {
	// 		start := time.Now().Format("2006-01-02 15:04:05.000")
	// 		fmt.Printf("%s➡️ Mongo Started:%s %s | Start Time: %s | Command: %v\n",
	// 			Cyan, Reset, evt.CommandName, start, evt.Command)
	// 	},
	// 	Succeeded: func(_ context.Context, evt *event.CommandSucceededEvent) {
	// 		end := time.Now().Format("2006-01-02 15:04:05.000")
	// 		fmt.Printf("%s✅ Mongo Succeeded:%s %s | End Time: %s | Duration: %.2fms\n",
	// 			Green, Reset, evt.CommandName, end, float64(evt.DurationNanos)/1e6)
	// 	},
	// 	Failed: func(_ context.Context, evt *event.CommandFailedEvent) {
	// 		end := time.Now().Format("2006-01-02 15:04:05.000")
	// 		fmt.Printf("%s❌ Mongo Failed:%s %s | End Time: %s | Error: %v\n",
	// 			Red, Reset, evt.CommandName, end, evt.Failure)
	// 	},
	// }

	// fmt.Println(dbUrl)
	client, err := mongo.Connect(
		context.Background(),
		options.Client().ApplyURI(dbUrl),
		// options.Client().SetMonitor(cmdMonitor),
		//.SetAuth(credential),
	)
	if err != nil {
		log.Fatal(err)
		return nil
	}

	// Check the connection
	err = client.Ping(ctx, nil)
	if err != nil {
		log.Printf("DB Ping Error")
		log.Fatal(err)
		return nil
	}

	return client.Database(dbName)
}

func Ping() bool {
	DBError = MongoClient.Ping(context.TODO(), nil)
	if DBError != nil {
		// fmt.Println(DBError)
		return false
	}
	return true
}

func GetenvStr(key string) string {
	return os.Getenv(key)
}

func GetenvInt(key string) int {
	s := GetenvStr(key)
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func CreateDb(host string, port int, dbName string, userid string, pwd string, collectionName string) *mongo.Database {
	dbUrl := fmt.Sprintf("mongodb+srv://%s:%s@%s", userid, pwd, host)
	// dbUrl := fmt.Sprintf("mongodb://%s:%s@%s:%d/?retryWrites=true&authSource=admin&w=majority&authMechanism=SCRAM-SHA-256", userid, pwd, host, port)

	client, err := mongo.Connect(
		context.Background(),
		options.Client().ApplyURI(dbUrl),
		//.SetAuth(credential),
	)
	if err != nil {
		log.Fatal(err)
		return nil
	}
	// Check the connection
	err = client.Ping(context.TODO(), nil)
	if err != nil {
		log.Printf("DB Ping Error")
		log.Fatal(err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = client.Database(dbName).CreateCollection(ctx, collectionName)
	if err != nil {
		log.Println(err.Error())
	}

	fmt.Println("Database created successfully:", dbName)
	return client.Database(dbName)

}
