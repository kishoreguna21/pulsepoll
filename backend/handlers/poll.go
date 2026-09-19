package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"pulsepoll/backend/models"
)

type PollHandler struct {
	Collection     *mongo.Collection
	UserCollection *mongo.Collection
	Redis          *redis.Client
}

type voteRequest struct {
	OptionIndex int `json:"optionIndex"`
}

func (h *PollHandler) CreatePoll(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	var payload models.Poll

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid poll payload"})
		return
	}

	payload.Question = strings.TrimSpace(payload.Question)

	if payload.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Question is required"})
		return
	}

	if len(payload.Options) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least 2 options are required"})
		return
	}

	seen := make(map[string]bool)
	cleanedOptions := make([]string, 0, len(payload.Options))

	for _, item := range payload.Options {
		optionText := strings.TrimSpace(item)

		if optionText == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Options cannot be empty"})
			return
		}

		if seen[optionText] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Duplicate options are not allowed"})
			return
		}

		seen[optionText] = true
		cleanedOptions = append(cleanedOptions, optionText)
	}

	payload.Options = cleanedOptions
	payload.Votes = make([]int, len(payload.Options))
	payload.CreatedBy = fmt.Sprint(userID)
	payload.CreatedAt = time.Now()
	payload.UpdatedAt = time.Now()

	result, err := h.Collection.InsertOne(c.Request.Context(), payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create poll"})
		return
	}

	pollID, ok := result.InsertedID.(bson.ObjectID)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid poll ID"})
		return
	}

	pollIDString := pollID.Hex()

	redisKey := "poll:" + pollIDString + ":votes"

	for i := range payload.Options {
		if err := h.Redis.HSet(
			c.Request.Context(),
			redisKey,
			fmt.Sprintf("option_%d", i),
			0,
		).Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to initialize Redis vote counts",
			})
			return
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Poll created successfully",
		"id":       pollIDString,
		"poll":     payload,
		"shareUrl": "http://localhost:5174/poll/" + pollIDString,
	})
}

func (h *PollHandler) GetPoll(c *gin.Context) {
	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid poll ID"})
		return
	}

	poll, err := h.getPollByID(c.Request.Context(), pollID)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Poll not found"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to load poll",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"poll": poll})
}

func (h *PollHandler) GetMyPolls(c *gin.Context) {
	userID, exists := c.Get("userID")

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Authentication required",
		})
		return
	}

	cursor, err := h.Collection.Find(
		c.Request.Context(),
		bson.M{"createdBy": fmt.Sprint(userID)},
		options.Find().SetSort(
			bson.D{{Key: "createdAt", Value: -1}},
		),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to load polls",
		})
		return
	}

	defer cursor.Close(c.Request.Context())

	var polls []models.Poll

	if err := cursor.All(c.Request.Context(), &polls); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to decode polls",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"polls": polls})
}

func (h *PollHandler) Vote(c *gin.Context) {
	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	var req voteRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid vote payload",
		})
		return
	}

	poll, err := h.getPollByID(c.Request.Context(), pollID)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Poll not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to load poll",
		})
		return
	}

	if req.OptionIndex < 0 || req.OptionIndex >= len(poll.Options) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid option selected",
		})
		return
	}

	// Update vote count
	poll.Votes[req.OptionIndex]++
	poll.UpdatedAt = time.Now()

	// Save updated votes in MongoDB
	_, err = h.Collection.UpdateOne(
		c.Request.Context(),
		bson.M{"_id": poll.ID},
		bson.M{
			"$set": bson.M{
				"votes":     poll.Votes,
				"updatedAt": poll.UpdatedAt,
			},
		},
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to save vote",
		})
		return
	}

	// Sync counts to Redis
	if err := h.syncVoteCountsToRedis(
		c.Request.Context(),
		pollID,
		poll.Votes,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update Redis vote counts",
		})
		return
	}

	// Prepare realtime message
	payload := map[string]interface{}{
		"pollId":    pollID,
		"votes":     poll.Votes,
		"question":  poll.Question,
		"options":   poll.Options,
		"updatedAt": poll.UpdatedAt,
	}

	message, err := json.Marshal(payload)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to prepare live update",
		})
		return
	}

	// Publish update to Redis
	channelName := "poll:" + pollID + ":updates"

	fmt.Println("Redis PUBLISH:", channelName)

	if err := h.Redis.Publish(
		c.Request.Context(),
		channelName,
		message,
	).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to broadcast live update",
		})
		return
	}

	fmt.Println("Live update published:", string(message))

	c.JSON(http.StatusOK, gin.H{
		"message": "Vote recorded",
		"poll":    poll,
	})
}

func (h *PollHandler) DeletePoll(c *gin.Context) {
	userID, exists := c.Get("userID")

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Authentication required",
		})
		return
	}

	pollID := c.Param("id")

	objID, err := bson.ObjectIDFromHex(pollID)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	result, err := h.Collection.DeleteOne(
		c.Request.Context(),
		bson.M{
			"_id":       objID,
			"createdBy": fmt.Sprint(userID),
		},
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to delete poll",
		})
		return
	}

	if result.DeletedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Poll not found or not authorized",
		})
		return
	}

	_ = h.Redis.Del(
		c.Request.Context(),
		"poll:"+pollID+":votes",
	).Err()

	_ = h.Redis.Publish(
		c.Request.Context(),
		"poll:"+pollID+":updates",
		[]byte(`{"deleted":true}`),
	).Err()

	c.JSON(http.StatusOK, gin.H{
		"message": "Poll deleted successfully",
	})
}

func (h *PollHandler) StreamPoll(c *gin.Context) {
	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	// SSE headers
	c.Writer.Header().Set(
		"Content-Type",
		"text/event-stream; charset=utf-8",
	)
	c.Writer.Header().Set(
		"Cache-Control",
		"no-cache",
	)
	c.Writer.Header().Set(
		"Connection",
		"keep-alive",
	)
	c.Writer.Header().Set(
		"X-Accel-Buffering",
		"no",
	)

	// CORS
	origin := c.Request.Header.Get("Origin")

	switch origin {
	case "http://localhost:5173",
		"http://localhost:5174",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:5174":

		c.Writer.Header().Set(
			"Access-Control-Allow-Origin",
			origin,
		)
	}

	c.Writer.Header().Set(
		"Access-Control-Allow-Credentials",
		"true",
	)

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	channelName := "poll:" + pollID + ":updates"

	fmt.Println("================================")
	fmt.Println("SSE CLIENT CONNECTED:", pollID)
	fmt.Println("REDIS CHANNEL:", channelName)
	fmt.Println("================================")

	// Subscribe to Redis
	pubsub := h.Redis.Subscribe(
		c.Request.Context(),
		channelName,
	)

	defer pubsub.Close()

	// Confirm Redis subscription
	if _, err := pubsub.Receive(
		c.Request.Context(),
	); err != nil {
		fmt.Println("Redis subscription error:", err)
		return
	}

	fmt.Println("Redis subscription ready:", channelName)

	// Send current poll state immediately
	poll, err := h.getPollByID(
		c.Request.Context(),
		pollID,
	)

	if err == nil {
		initialPayload := map[string]interface{}{
			"pollId":    pollID,
			"votes":     poll.Votes,
			"question":  poll.Question,
			"options":   poll.Options,
			"updatedAt": poll.UpdatedAt,
		}

		data, _ := json.Marshal(initialPayload)

		fmt.Fprintf(
			c.Writer,
			"data: %s\n\n",
			data,
		)

		c.Writer.Flush()

		fmt.Println(
			"SSE INITIAL DATA:",
			string(data),
		)
	}

	// Listen for Redis messages
	channel := pubsub.Channel()

	for {
		select {

		case <-c.Request.Context().Done():

			fmt.Println(
				"SSE CLIENT DISCONNECTED:",
				pollID,
			)

			return

		case message, ok := <-channel:

			if !ok {
				fmt.Println(
					"Redis channel closed:",
					pollID,
				)
				return
			}

			if message == nil ||
				message.Payload == "" {
				continue
			}

			fmt.Println(
				"REDIS MESSAGE RECEIVED:",
				message.Payload,
			)

			fmt.Fprintf(
				c.Writer,
				"data: %s\n\n",
				message.Payload,
			)

			c.Writer.Flush()

			fmt.Println(
				"SSE UPDATE SENT:",
				message.Payload,
			)
		}
	}
}

func (h *PollHandler) getPollByID(
	ctx context.Context,
	pollID string,
) (models.Poll, error) {

	var poll models.Poll

	objID, err := bson.ObjectIDFromHex(pollID)

	if err != nil {
		return poll, err
	}

	err = h.Collection.FindOne(
		ctx,
		bson.M{"_id": objID},
	).Decode(&poll)

	return poll, err
}

func (h *PollHandler) syncVoteCountsToRedis(
	ctx context.Context,
	pollID string,
	votes []int,
) error {

	redisKey := "poll:" + pollID + ":votes"

	// Remove old Redis values
	if err := h.Redis.Del(
		ctx,
		redisKey,
	).Err(); err != nil {
		return err
	}

	// Store latest counts
	for i, count := range votes {

		if err := h.Redis.HSet(
			ctx,
			redisKey,
			fmt.Sprintf("option_%d", i),
			count,
		).Err(); err != nil {
			return err
		}
	}

	return nil
}
