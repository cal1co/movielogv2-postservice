package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

func RateLimiterMiddleware() gin.HandlerFunc {
	limiter := rate.NewLimiter(1, 20)

	return func(c *gin.Context) {
		if limiter.Allow() == false {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			return
		}
		c.Next()
	}
}
func verifyToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("SECRET_TOKEN")), nil
	})
}
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			fmt.Println("NO AUTH HEADER", c.Request)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

		token, err := verifyToken(tokenString)
		if err != nil {
			fmt.Println("ERROR HERE", tokenString, token, err)
			log.Printf("error: %s", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization token"})
			return
		}
		userID := token.Claims.(jwt.MapClaims)["id"].(float64)
		c.Set("user_id", userID)
		_, exists := c.Get("user_id")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization token"})
			return
		}
		c.Next()
	}
}

func ActivityTrackerMiddleware(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		userId, exists := c.Get("user_id")
		fmt.Println("before")
		if !exists {
			c.Next()
			return
		}
		fmt.Println("after")

		lastActiveKey := fmt.Sprintf("user:%v:lastActive", userId)

		now := time.Now().UTC().Unix()
		fmt.Println("TIME", now)
		if err := redisClient.Set(ctx, lastActiveKey, now, 0).Err(); err != nil {
			fmt.Println("Error updating user activity:", err)
		}

		c.Next()
	}
}
