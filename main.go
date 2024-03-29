package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	// URI for MongoDB connection.
	URI_MONGODB string

	// Database to use for storing logs.
	DATABASE string

	// Collection to use for storing logs.
	COLLECTION string

	// Port to listen.
	PORT string

	// Timezone to display datetime.
	TIMEZONE string
)

// Data structure as defined in https://github.com/bryanasdev000/microservice-jitsi-log .
type Jitsilog struct {
	Sala      string `json:"sala"`
	Curso     string `json:"curso"`
	Turma     string `json:"turma"`
	Aluno     string `json:"aluno"`
	Jid       string `json:"jid"`
	Email     string `json:"email"`
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
}

func cabecalhoCSV() (c []string) {
	c = append(c, "sala")
	c = append(c, "curso")
	c = append(c, "turma")
	c = append(c, "aluno")
	c = append(c, "jid")
	c = append(c, "email")
	c = append(c, "timestamp")
	c = append(c, "action")
	return
}

func (jl *Jitsilog) registroCSV() (r []string) {
	r = append(r, jl.Sala)
	r = append(r, jl.Curso)
	r = append(r, jl.Turma)
	r = append(r, jl.Aluno)
	r = append(r, jl.Jid)
	r = append(r, jl.Email)
	r = append(r, jl.Timestamp)
	r = append(r, jl.Action)
	return
}

// Setup of logs and database related configs.
func init() {
	log.Debug("microservice-jitsi-log-view init")
	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetReportCaller(true)
	if os.Getenv("DEBUG") == "true" {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if mongoUri, found := os.LookupEnv("URI_MONGODB"); found {
		URI_MONGODB = mongoUri
	} else {
		URI_MONGODB = "mongodb://localhost:27017"
	}
	if dbName, found := os.LookupEnv("DATABASE"); found {
		DATABASE = dbName
	} else {
		DATABASE = "jitsilog"
	}
	if collection, found := os.LookupEnv("COLLECTION"); found {
		COLLECTION = collection
	} else {
		COLLECTION = "logs"
	}
	if tz, found := os.LookupEnv("TIMEZONE"); found {
		TIMEZONE = tz
	} else {
		TIMEZONE = "America/Sao_Paulo"
	}
	if port := os.Getenv("PORT"); strings.HasPrefix(port, ":") {
		PORT = port
	} else {
		PORT = ":8080"
		log.Info("Port variable is missing or in wrong format (missing a colon ( : ) at start. It should be like ':8080'), using default: :8080")
	}
	log.WithFields(log.Fields{
		"URI":        URI_MONGODB,
		"Database":   DATABASE,
		"Collection": COLLECTION}).Info("Database Connection Info")
	log.Info("Listening at ", PORT)
	log.Info("Using ", TIMEZONE, " as timezone")
	log.Info("CORS Enabled")
}

// Creates and return a MongoDB client.
func getClient() *mongo.Client {
	context, _ := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := mongo.Connect(context, options.Client().ApplyURI(URI_MONGODB))
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Fatal("Failed to create the Mongo client!")
	}
	return client
}

// Find logs with filter and ordered by decrescent timestamp, can limit & skip items in dataset.
func findLogsFilter(size string, filter bson.D, skip string) (error, []*Jitsilog) {
	tz, err := time.LoadLocation(TIMEZONE)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Fatal("Failed to load TZ info")
	}
	client := getClient()
	optFind := options.Find()
	var jitsilogs []*Jitsilog

	sizeInt, err := strconv.ParseInt(size, 10, 64)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to convert size argument to int")
		return err, nil
	}

	skipInt, err := strconv.ParseInt(skip, 10, 64)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to convert skip argument to int")
		return err, nil
	}
	log.Debug("Dataset row limit ", sizeInt)
	log.Debug("Dataset row skip ", skipInt)
	collection := client.Database(DATABASE).Collection(COLLECTION)
	count, err := collection.CountDocuments(context.TODO(), filter)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Error on count of the documents")
		return err, nil
	}
	if skipInt > count {
		skipInt = count
	} else if skipInt < 0 {
		skipInt = 0
	}
	if sizeInt < 0 {
		sizeInt = 20
	}
	log.Debug("Dataset row max: ", count)
	optFind.SetSkip(skipInt)
	optFind.SetLimit(sizeInt)
	optFind.SetSort(bson.D{{"timestamp", -1}})
	cursor, err := collection.Find(context.TODO(), filter, optFind)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Error on finding the documents")
		return err, nil
	}
	log.Debug("Connection to MongoDB opened.")
	for cursor.Next(context.TODO()) {
		var jitsilog Jitsilog
		err = cursor.Decode(&jitsilog)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err}).Info("Error on decoding the document")
			return err, nil
		}
		t, err := time.ParseInLocation(time.RFC3339, jitsilog.Timestamp, tz)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err}).Info("Failed to parse ISO8601")
			jitsilog.Timestamp = "Falha no parser"
		} else {
			jitsilog.Timestamp = t.In(tz).String()
		}
		jitsilogs = append(jitsilogs, &jitsilog)
	}
	log.Debug("Data retrived")
	err = client.Disconnect(context.TODO())
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Fatal("Failed to disconnect from database!")
	}
	log.Debug("Connection to MongoDB closed.")
	return nil, jitsilogs
}

// Default handler, return the name of this service.
func defaultHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "microservice-jitsi-log-view")
}

// Check health of the microservice. Returns the hostname of the machine or container running on.
func checkHealth(w http.ResponseWriter, r *http.Request) {
	name, err := os.Hostname()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Fatal("Failed to get hostname!")
	}
	fmt.Fprintf(w, "Awake and alive from %s", name)
}

// Query the latest logs with a variable dataset size based on the URL.
func latestLogsHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	err, jitsilogs := findLogsFilter(queryParams["size"][0], bson.D{}, queryParams["skip"][0])
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to get logs!")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jitsilogs)
}

// Query all logs that correspond with desired courseid.
func searchCourseHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	filter := bson.D{{}}
	filter = append(filter, bson.E{Key: "curso", Value: bson.D{{"$regex", primitive.Regex{Pattern: queryParams["id"][0], Options: "gi"}}}})
	err, jitsilogs := findLogsFilter(queryParams["size"][0], filter, queryParams["skip"][0])
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to get logs!")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jitsilogs)
}

// Query all logs that correspond with desired classid
func searchClassHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	filter := bson.D{{}}
	filter = append(filter, bson.E{Key: "turma", Value: bson.D{{"$regex", primitive.Regex{Pattern: queryParams["id"][0], Options: "gi"}}}})
	err, jitsilogs := findLogsFilter(queryParams["size"][0], filter, queryParams["skip"][0])
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to get logs!")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jitsilogs)
}

// Query all logs that correspond with desired roomid
func searchRoomHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	filter := bson.D{{}}
	filter = append(filter, bson.E{Key: "sala", Value: bson.D{{"$regex", primitive.Regex{Pattern: queryParams["id"][0], Options: "gi"}}}})
	err, jitsilogs := findLogsFilter(queryParams["size"][0], filter, queryParams["skip"][0])
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to get logs!")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jitsilogs)
}

// Query all logs that correspond with desired student email
func searchStudentHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	filter := bson.D{{}}
	filter = append(filter, bson.E{Key: "email", Value: bson.D{{"$regex", primitive.Regex{Pattern: queryParams["email"][0], Options: "gi"}}}})
	err, jitsilogs := findLogsFilter(queryParams["size"][0], filter, queryParams["skip"][0])
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to get logs!")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jitsilogs)
}

// Query all logs earlier than a timestamp and export them as a CSV file
func searchAndExportAsCSV(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	timestamp := queryParams.Get("ts")
	now := time.Now()

	// preparing the response to output a csv file
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition",
		"attachment; filename=jitsi-presence-logger."+now.Format(time.RFC3339)+".csv")
	csvWriter := csv.NewWriter(w)
	csvWriter.Comma = ';'

	// querying database
	filter := bson.D{{"timestamp", bson.D{{"$gte", timestamp}}}}
	err, jitsilogs := findLogsFilter("0", filter, "0")

	// writing response
	if err != nil {
		log.WithFields(log.Fields{
			"error": err}).Info("Failed to get logs!")
		csvWriter.Write([]string{
			"Ocorreu um erro ao realizar a requisição", err.Error(),
		})
	} else {
		csvWriter.Write(cabecalhoCSV())
		for _, log := range jitsilogs {
			csvWriter.Write(log.registroCSV())
		}
	}

	csvWriter.Flush()
}

func main() {
	router := mux.NewRouter()
	router.HandleFunc("/", defaultHandler).Methods(http.MethodGet)
	router.HandleFunc("/healthcheck", checkHealth).Methods(http.MethodGet)
	version := router.PathPrefix("/v1").Subrouter()
	version.HandleFunc("/csv", searchAndExportAsCSV).Methods(http.MethodGet).Queries("ts", "{ts}")
	api := version.PathPrefix("/logs").Subrouter()
	api.HandleFunc("/last", latestLogsHandler).Methods("GET")
	api.HandleFunc("/course", searchCourseHandler).Methods("GET").Queries("id", "{id}")
	api.HandleFunc("/class", searchClassHandler).Methods("GET").Queries("id", "{id}")
	api.HandleFunc("/student", searchStudentHandler).Methods("GET").Queries("email", "{email}")
	api.HandleFunc("/room", searchRoomHandler).Methods("GET").Queries("id", "{id}")
	http.Handle("/", router)
	loggedRouter := handlers.LoggingHandler(os.Stdout, router)
	log.Fatal(http.ListenAndServe(PORT, handlers.CORS()(loggedRouter)))
}
