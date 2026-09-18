package database

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func ConnectRedis(redisURL string) (*redis.Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opt)

	ctx := context.Background()

	_, err = client.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}

	fmt.Println("Redis connected successfully")

	return client, nil
}
