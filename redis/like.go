package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var redisClient *redis.Client

func InitializeRedis(ctx context.Context) {
	redisClient = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	// Ping Redis to check connectivity
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		panic(err)
	}
}

func Like(ctx context.Context, postID string, c *gin.Context) {
	// Endpoint accessed
	// checks if post is in cache.
	// ----if post in cache, increment like
	// ----if not, get the like count from cassandra and increment. store the like count with postid
	// if post is a comment, check if comment in cache.
	// ----if post cache, increment like and sort?
	// ----if post not in cache, add to cache and increment. store the comment id, parent id, and like count. need to get all comments belonging to post
	redisKey := fmt.Sprintf("post:%s", postID)
	isCached, err := redisClient.Exists(redisKey).Result()
	if err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, "Error Liking post")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if isCached(redisClient, postID, ctx) {
		// simply inc like
		fmt.Println("cached")
	} else {
		fmt.Println("not cached")
	}

}

func isCached(redisClient *redis.Client, key string, ctx context.Context) bool {
	_, err := redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false // key does not exist in cache
	} else if err != nil {
		// handle other errors here
	} else {
		return true // key exists in cache
	}
	return true
}
