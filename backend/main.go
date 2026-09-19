package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"pulsepoll/backend/database"
	"pulsepoll/backend/handlers"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found")
	}

	mongoURI := os.Getenv("MONGODB_URI")
	redisURL := os.Getenv("REDIS_URL")

	if mongoURI == "" {
		log.Fatal("MONGODB_URI is missing")
	}

	if redisURL == "" {
		log.Fatal("REDIS_URL is missing")
	}

	mongoClient, err := database.ConnectMongoDB(mongoURI)
	if err != nil {
		log.Fatal("MongoDB connection failed:", err)
	}

	redisClient, err := database.ConnectRedis(redisURL)
	if err != nil {
		log.Fatal("Redis connection failed:", err)
	}

	db := mongoClient.Database("pulsepoll")

	pollCollection := db.Collection("polls")
	userCollection := db.Collection("users")

	authHandler := &handlers.AuthHandler{
		Collection: userCollection,
	}

	authMiddleware := authHandler.AuthMiddleware()

	pollHandler := &handlers.PollHandler{
		Collection:     pollCollection,
		UserCollection: userCollection,
		Redis:          redisClient,
	}

	router := gin.Default()

	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		allowedOrigins := map[string]bool{
			"http://localhost:5173": true,
			"http://localhost:5174": true,
			"http://127.0.0.1:5173": true,
			"http://127.0.0.1:5174": true,
		}

		frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")

		if frontendURL != "" {
			allowedOrigins[frontendURL] = true
		}

		if allowedOrigins[origin] {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
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
			"Access-Control-Allow-Credentials",
			"true",
		)

		c.Writer.Header().Set(
			"Access-Control-Expose-Headers",
			"Content-Type",
		)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "PulsePoll Backend is running",
			"status":  "ok",
		})
	})

	// Authentication
	router.POST("/api/auth/register", authHandler.Register)
	router.POST("/api/auth/login", authHandler.Login)

	// Protected poll routes
	router.POST("/api/polls", authMiddleware, pollHandler.CreatePoll)
	router.GET("/api/my-polls", authMiddleware, pollHandler.GetMyPolls)
	router.DELETE("/api/polls/:id", authMiddleware, pollHandler.DeletePoll)

	// Public poll routes
	router.GET("/api/polls/:id", pollHandler.GetPoll)
	router.POST("/api/polls/:id/vote", pollHandler.Vote)
	router.GET("/api/polls/:id/stream", pollHandler.StreamPoll)

	log.Println("================================")
	log.Println("PulsePoll Backend Started")
	log.Println("MongoDB: Connected")
	log.Println("Redis: Connected")

	port := os.Getenv("PORT")

	if port == "" {
		port = "8080"
	}

	log.Println("Server: http://localhost:" + port)
	log.Println("================================")

	if err := router.Run(":" + port); err != nil {
		log.Fatal("Server failed:", err)
	}
}
