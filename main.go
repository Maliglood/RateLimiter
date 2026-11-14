package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"rate-limiter/handler"
	"rate-limiter/limiter"
	"rate-limiter/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	defaultLimit := getEnvInt("DEFAULT_LIMIT", 60)
	listenAddr := getEnv("LISTEN_ADDR", ":8080")

	rdb := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		PoolSize:     50,
	})

	redisStore := store.NewRedisStore(rdb, "ratelimiter", time.Minute)
	memStore := store.NewMemoryStore(time.Minute)
	resilient := limiter.NewResilientLimiter(redisStore, memStore, logger)
	defer resilient.Close()

	proxyHandler := handler.NewProxyHandler(resilient, defaultLimit, logger)

	mux := http.NewServeMux()
	mux.Handle("/", proxyHandler)

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("starting rate limiter", "addr", listenAddr, "redis", redisAddr, "default_limit", defaultLimit)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
