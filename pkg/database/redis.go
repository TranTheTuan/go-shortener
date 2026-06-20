package database

import (
	"context"
	"fmt"
	"time"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	Client *redis.Client
}

func SetupRedis(cfg configs.RedisConfig) (*RedisClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("không thể kết nối Redis tại %s: %v", cfg.Host, err)
	}

	return &RedisClient{Client: rdb}, nil
}
