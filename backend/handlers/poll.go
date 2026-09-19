package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"pulsepoll/backend/models"
)

type PollHandler struct {
	Collection     *mongo.Collection
	UserCollection *mongo.Collection
	Redis          *redis.Client
}

type createPollRequest struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

func (h *PollHandler) CreatePoll(c *gin.Context) {
	userIDValue, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	userID, ok := userIDValue.(string)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user"})
		return
	}

	var req createPollRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	req.Question = strings.TrimSpace(req.Question)

	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Question is required"})
		return
	}

	if len(req.Options) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "At least 2 options are required",
		})
		return
	}

	options := make([]string, 0, len(req.Options))
	seen := make(map[string]bool)

	for _, option := range req.Options {
		option = strings.TrimSpace(option)

		if option == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Options cannot be empty",
			})
			return
		}

		key := strings.ToLower(option)

		if seen[key] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Duplicate options are not allowed",
			})
			return
		}

		seen[key] = true
		options = append(options, option)
	}

	now := time.Now()

	votes := make([]int, len(options))

	poll := models.Poll{
		Question:  req.Question,
		Options:   options,
		Votes:     votes,
		CreatedBy: userID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	result, err := h.Collection.InsertOne(c.Request.Context(), poll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to create poll",
		})
		return
	}

	poll.ID = result.InsertedID.(bson.ObjectID)

	redisKey := "poll:" + poll.ID.Hex() + ":votes"

	for i := range votes {
		h.Redis.HSet(
			c.Request.Context(),
			redisKey,
			fmt.Sprintf("%d", i),
			0,
		)
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Poll created successfully",
		"poll":    poll,
	})
}

func (h *PollHandler) GetPoll(c *gin.Context) {
	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
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
			"error": "Unable to fetch poll",
		})
		return
	}

	c.JSON(http.StatusOK, poll)
}

func (h *PollHandler) GetMyPolls(c *gin.Context) {
	userIDValue, exists := c.Get("userID")

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Authentication required",
		})
		return
	}

	userID, ok := userIDValue.(string)

	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid user",
		})
		return
	}

	cursor, err := h.Collection.Find(
		c.Request.Context(),
		bson.M{"createdBy": userID},
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to fetch polls",
		})
		return
	}

	defer cursor.Close(c.Request.Context())

	var polls []models.Poll

	if err := cursor.All(c.Request.Context(), &polls); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to read polls",
		})
		return
	}

	if polls == nil {
		polls = []models.Poll{}
	}

	c.JSON(http.StatusOK, polls)
}

func (h *PollHandler) Vote(c *gin.Context) {
	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	var req struct {
		OptionIndex int `json:"optionIndex"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request",
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
			"error": "Unable to fetch poll",
		})
		return
	}

	if req.OptionIndex < 0 || req.OptionIndex >= len(poll.Options) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid option",
		})
		return
	}

	poll.Votes[req.OptionIndex]++
	poll.UpdatedAt = time.Now()

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
			"error": "Unable to save vote",
		})
		return
	}

	err = h.syncVoteCountsToRedis(
		c.Request.Context(),
		poll,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to update realtime data",
		})
		return
	}

	payload, _ := json.Marshal(poll)

	channel := "poll:" + poll.ID.Hex() + ":updates"

	err = h.Redis.Publish(
		c.Request.Context(),
		channel,
		string(payload),
	).Err()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to publish realtime update",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Vote recorded",
		"poll":    poll,
	})
}

func (h *PollHandler) DeletePoll(c *gin.Context) {
	userIDValue, exists := c.Get("userID")

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Authentication required",
		})
		return
	}

	userID, ok := userIDValue.(string)

	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid user",
		})
		return
	}

	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	objectID, err := bson.ObjectIDFromHex(pollID)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	result, err := h.Collection.DeleteOne(
		c.Request.Context(),
		bson.M{
			"_id":       objectID,
			"createdBy": userID,
		},
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Unable to delete poll",
		})
		return
	}

	if result.DeletedCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Poll not found",
		})
		return
	}

	h.Redis.Del(
		c.Request.Context(),
		"poll:"+pollID+":votes",
	)

	payload := `{"deleted":true}`

	h.Redis.Publish(
		c.Request.Context(),
		"poll:"+pollID+":updates",
		payload,
	)

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

	origin := c.Request.Header.Get("Origin")
	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")

	allowedOrigins := map[string]bool{
		"http://localhost:5173": true,
		"http://localhost:5174": true,
		"http://127.0.0.1:5173": true,
		"http://127.0.0.1:5174": true,
	}

	if frontendURL != "" {
		allowedOrigins[frontendURL] = true
	}

	if origin != "" && allowedOrigins[origin] {
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Vary", "Origin")
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Streaming unsupported",
		})
		return
	}
	flusher.Flush()

	poll, err := h.getPollByID(c.Request.Context(), pollID)
	if err != nil {
		return
	}

	initialData, err := json.Marshal(poll)
	if err != nil {
		return
	}

	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", initialData); err != nil {
		return
	}
	flusher.Flush()

	ctx := c.Request.Context()
	channel := "poll:" + pollID + ":updates"

	pubsub := h.Redis.Subscribe(ctx, channel)
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		return
	}

	redisChannel := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-redisChannel:
			if !ok {
				return
			}
			if msg == nil {
				continue
			}

			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", msg.Payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *PollHandler) getPollByID(
	ctx context.Context,
	pollID string,
) (models.Poll, error) {

	objectID, err := bson.ObjectIDFromHex(pollID)

	if err != nil {
		return models.Poll{}, err
	}

	var poll models.Poll

	err = h.Collection.FindOne(
		ctx,
		bson.M{"_id": objectID},
	).Decode(&poll)

	return poll, err
}

func (h *PollHandler) syncVoteCountsToRedis(
	ctx context.Context,
	poll models.Poll,
) error {

	key := "poll:" + poll.ID.Hex() + ":votes"

	pipe := h.Redis.TxPipeline()

	for i, voteCount := range poll.Votes {
		pipe.HSet(
			ctx,
			key,
			fmt.Sprintf("%d", i),
			voteCount,
		)
	}

	_, err := pipe.Exec(ctx)

	return err
}
