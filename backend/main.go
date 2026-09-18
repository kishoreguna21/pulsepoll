package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"pulsepoll/backend/database"
	"pulsepoll/backend/handlers"
)

func main() {

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found")
	}

	// Read environment variables
	mongoURI := os.Getenv("MONGODB_URI")
	redisURL := os.Getenv("REDIS_URL")

	if mongoURI == "" {
		log.Fatal("MONGODB_URI is missing")
	}

	if redisURL == "" {
		log.Fatal("REDIS_URL is missing")
	}

	// -----------------------------
	// MongoDB Connection
	// -----------------------------
	mongoClient, err := database.ConnectMongoDB(mongoURI)
	if err != nil {
		log.Fatal("MongoDB connection failed:", err)
	}

	// -----------------------------
	// Redis Connection
	// -----------------------------
	redisClient, err := database.ConnectRedis(redisURL)
	if err != nil {
		log.Fatal("Redis connection failed:", err)
	}

	// -----------------------------
	// Database Collections
	// -----------------------------
	db := mongoClient.Database("pulsepoll")

	pollCollection := db.Collection("polls")
	userCollection := db.Collection("users")

	// -----------------------------
	// Handlers
	// -----------------------------
	pollHandler := &handlers.PollHandler{
		Collection:     pollCollection,
		UserCollection: userCollection,
		Redis:          redisClient,
	}

	// -----------------------------
	// Gin Router
	// -----------------------------
	router := gin.Default()

	// -----------------------------
	// CORS
	// IMPORTANT:
	// Frontend is running on 5174
	// -----------------------------
	router.Use(func(c *gin.Context) {

		origin := c.Request.Header.Get("Origin")

		allowedOrigins := map[string]bool{
			"http://localhost:5173": true,
			"http://localhost:5174": true,
			"http://127.0.0.1:5173": true,
			"http://127.0.0.1:5174": true,
		}

		if allowedOrigins[origin] {
			c.Writer.Header().Set(
				"Access-Control-Allow-Origin",
				origin,
			)
		}

		c.Writer.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, POST, PUT, DELETE, OPTIONS",
		)

		c.Writer.Header().Set(
			"Access-Control-Allow-Headers",
			"Origin, Content-Type, Accept, Authorization",
		)

		c.Writer.Header().Set(
			"Access-Control-Expose-Headers",
			"Content-Type",
		)

		c.Writer.Header().Set(
			"Access-Control-Allow-Credentials",
			"true",
		)

		// Handle browser preflight requests
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// -----------------------------
	// Health Check
	// -----------------------------
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "PulsePoll Backend is running",
			"status":  "ok",
		})
	})

	// -----------------------------
	// Authentication
	// -----------------------------
	authHandler := &handlers.AuthHandler{
		Collection: userCollection,
	}

	router.POST("/api/auth/register", authHandler.Register)
	router.POST("/api/auth/login", authHandler.Login)

	// -----------------------------
	// Poll APIs
	// -----------------------------

	// Create poll
	router.POST("/api/polls", pollHandler.CreatePoll)

	// Get logged-in user's polls
	router.GET("/api/my-polls", pollHandler.GetMyPolls)

	// Delete poll
	router.DELETE("/api/polls/:id", pollHandler.DeletePoll)

	// Get single poll
	router.GET("/api/polls/:id", pollHandler.GetPoll)

	// Vote
	router.POST("/api/polls/:id/vote", pollHandler.Vote)

	// REALTIME SSE STREAM
	router.GET("/api/polls/:id/stream", pollHandler.StreamPoll)

	// -----------------------------
	// Start Server
	// -----------------------------
	log.Println("================================")
	log.Println("PulsePoll Backend Started")
	log.Println("MongoDB: Connected")
	log.Println("Redis: Connected")
	log.Println("Server: http://localhost:8080")
	log.Println("Frontend: http://localhost:5174")
	log.Println("================================")

	if err := router.Run(":8080"); err != nil {
		log.Fatal("Server failed:", err)
	}
}
