import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "os"
    "strings"
    "time"


func (h *PollHandler) StreamPoll(c *gin.Context) {
	pollID := c.Param("id")

	if _, err := bson.ObjectIDFromHex(pollID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid poll ID",
		})
		return
	}

	// Production + local CORS
	origin := c.Request.Header.Get("Origin")
	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")

	allowed := false
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

	if frontendURL != "" && origin == frontendURL {
		allowed = true
	}

	if allowed {
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
	}

	c.Writer.Header().Set(
		"Access-Control-Allow-Methods",
		"GET, OPTIONS",
	)

	c.Writer.Header().Set(
		"Access-Control-Allow-Headers",
		"Content-Type, Authorization",
	)

	// SSE headers
	c.Writer.Header().Set(
		"Content-Type",
		"text/event-stream; charset=utf-8",
	)

	c.Writer.Header().Set(
		"Cache-Control",
		"no-cache, no-store, must-revalidate",
	)

	c.Writer.Header().Set(
		"Connection",
		"keep-alive",
	)

	c.Writer.Header().Set(
		"X-Accel-Buffering",
		"no",
	)

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	channelName := "poll:" + pollID + ":updates"

	fmt.Println("================================")
	fmt.Println("SSE CLIENT CONNECTED:", pollID)
	fmt.Println("REDIS CHANNEL:", channelName)
	fmt.Println("ORIGIN:", origin)
	fmt.Println("FRONTEND URL:", frontendURL)
	fmt.Println("================================")

	// Subscribe to Redis
	pubsub := h.Redis.Subscribe(
		c.Request.Context(),
		channelName,
	)

	defer pubsub.Close()

	if _, err := pubsub.Receive(
		c.Request.Context(),
	); err != nil {
		fmt.Println("Redis subscription error:", err)
		return
	}

	fmt.Println("Redis subscription ready:", channelName)

	// Send current poll state
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

		data, err := json.Marshal(initialPayload)

		if err == nil {
			fmt.Fprintf(
				c.Writer,
				"data: %s\n\n",
				data,
			)

			c.Writer.Flush()

			fmt.Println("SSE INITIAL DATA:", string(data))
		}
	}

	// Heartbeat keeps the production SSE connection alive
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	channel := pubsub.Channel()

	for {
		select {

		case <-c.Request.Context().Done():

			fmt.Println(
				"SSE CLIENT DISCONNECTED:",
				pollID,
			)

			return

		case <-heartbeat.C:

			fmt.Fprint(
				c.Writer,
				": heartbeat\n\n",
			)

			c.Writer.Flush()

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