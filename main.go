package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	handlers "github.com/cal1co/movielogv2-postservice/handlers"
	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
	"github.com/golang-jwt/jwt"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

var session *gocql.Session
var redisClient *redis.Client

func init() {
	// Connect to Cassandra
	cluster := gocql.NewCluster("127.0.0.1")
	cluster.Keyspace = "user_posts"
	var err error
	session, err = cluster.CreateSession()
	if err != nil {
		panic(err)
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

}

func MigrateLikesToDB() {
	ctx := context.Background()
	cursor := uint64(0)
	keys := []string{}

	for {
		var err error
		var scanResult []string

		scanResult, cursor, err = redisClient.Scan(ctx, cursor, "post:*:likes", 100).Result()
		if err != nil {
			log.Printf("Error scanning Redis keys: %v", err)
			return
		}
		log.Printf("%s", scanResult)
		keys = append(keys, scanResult...)

		if cursor == 0 {
			break
		}
	}

	for _, key := range keys {
		postID := strings.TrimPrefix(strings.TrimSuffix(key, ":likes"), "post:")
		likesCount, err := redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Error getting likes for post %s: %v", postID, err)
			continue
		}
		err = session.Query("UPDATE post_interactions SET likes = ? WHERE post_id = ?", likesCount, postID).Exec()
		if err != nil {
			log.Printf("Error updating likes for post %s: %v", postID, err)
			continue
		}
	}
}

func rateLimiterMiddleware() gin.HandlerFunc {
	// set up the rate limiter
	limiter := rate.NewLimiter(1, 5)

	return func(c *gin.Context) {
		// check if the limiter allows the request
		if limiter.Allow() == false {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			return
		}

		// move on to the next middleware/handler
		c.Next()
	}
}
func verifyToken(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(""), nil
	})
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		tokenString := strings.Replace(authHeader, "Bearer ", "", 1)

		token, err := verifyToken(tokenString)
		if err != nil {
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

const pageSize int = 15

func main() {
	defer session.Close()

	go func() {
		for {
			MigrateLikesToDB()
			time.Sleep(time.Hour)
		}
	}()

	r := gin.Default()

	r.Use(rateLimiterMiddleware())
	r.Use(authMiddleware())

	r.GET("/", func(c *gin.Context) {

	})

	r.POST("/post", func(c *gin.Context) {
		handlers.HandlePost(c, session)
	})

	r.POST("/post/:id/comment", func(c *gin.Context) {
		handlers.HandleComment(c, session)
	})

	r.POST("/post/:id/like", func(c *gin.Context) {
		handlers.HandleLike(c, false, session, redisClient)
	})

	r.POST("/post/:id/unlike", func(c *gin.Context) {
		handlers.HandleUnlike(c, false, session, redisClient)
	})

	r.POST("/comment/:id/like", func(c *gin.Context) {
		handlers.HandleLike(c, true, session, redisClient)
	})

	r.POST("/comment/:id/unlike", func(c *gin.Context) {
		handlers.HandleUnlike(c, true, session, redisClient)
	})

	r.GET("/posts/:id", func(c *gin.Context) {
		handlers.HandlePostGet(c, false, session, redisClient)
	})

	r.GET("/comments/:id", func(c *gin.Context) {
		handlers.HandlePostGet(c, true, session, redisClient)
	})

	r.GET("/user/:id/posts/", func(c *gin.Context) {
		handlers.GetUserPosts(c, session)
	})
	// this doesnt delete from redis right now
	r.DELETE("/posts/:id", func(c *gin.Context) {
		handlers.HandlePostDelete(c, session)
	})

	// Run the server
	r.Run()
}
