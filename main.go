package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	handlers "github.com/cal1co/movielogv2-postservice/handlers"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
	"github.com/golang-jwt/jwt"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

var session *gocql.Session
var redisClient *redis.Client

func init() {
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

		scanResult, cursor, err = redisClient.Scan(ctx, cursor, "post:*:comments", 100).Result()
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
		// err = session.Query("UPDATE post_interactions SET comments = ? WHERE post_id = ?", commentCount, postID).Exec()
		// if err != nil {
		// 	log.Printf("Error updating likes for post %s: %v", postID, err)
		// 	continue
		// }
	}
}

func rateLimiterMiddleware() gin.HandlerFunc {
	limiter := rate.NewLimiter(1, 5)

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
func authMiddleware() gin.HandlerFunc {
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
func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func main() {
	defer session.Close()

	go func() {
		for {
			MigrateLikesToDB()
			time.Sleep(time.Hour)
		}
	}()

	loadEnv()
	r := gin.Default()

	config := cors.DefaultConfig()
	config.AllowMethods = []string{"GET", "POST", "DELETE", "OPTIONS"}
	config.AddAllowHeaders("Authorization")
	config.AllowOrigins = []string{"http://localhost:5173"}
	r.Use(cors.New(config))

	cert, _ := ioutil.ReadFile(os.Getenv("ELASTIC_CERT_PATH"))
	cfg := elasticsearch.Config{
		Addresses: []string{"https://localhost:9200"},
		Username:  os.Getenv("ELASTIC_USERNAME"),
		Password:  os.Getenv("ELASTIC_PASSWORD"),
		CACert:    cert,
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		fmt.Printf("Error creating the client: %s\n", err)
		return
	}

	r.Use(rateLimiterMiddleware())
	r.Use(authMiddleware())

	r.POST("/", func(c *gin.Context) {
		fmt.Println("post to /")
	})

	r.POST("/post", func(c *gin.Context) {
		handlers.HandlePost(c, session)
	})

	r.POST("/post/:id/comment", func(c *gin.Context) {
		handlers.HandleComment(c, session, redisClient, false)
	})
	r.GET("/post/:id/comments", func(c *gin.Context) {
		handlers.GetPostComments(c, session)
	})
	r.POST("/comment/:id/comment", func(c *gin.Context) {
		handlers.HandleComment(c, session, redisClient, true)
	})

	r.POST("/post/like/:id", func(c *gin.Context) {
		handlers.HandleLike(c, false, session, redisClient)
	})

	r.POST("/post/unlike/:id", func(c *gin.Context) {
		handlers.HandleUnlike(c, false, session, redisClient)
	})

	r.POST("/comment/:id/like", func(c *gin.Context) {
		handlers.HandleLike(c, true, session, redisClient)
	})

	r.POST("/comment/:id/unlike", func(c *gin.Context) {
		handlers.HandleUnlike(c, true, session, redisClient)
	})

	r.GET("/feed/user/:id", func(c *gin.Context) {
		handlers.GetUserPosts(c, session, redisClient)
	})

	r.GET("/posts/:id", func(c *gin.Context) {
		handlers.HandlePostGet(c, false, session, redisClient)
	})

	r.GET("/comments/:id", func(c *gin.Context) {
		handlers.HandlePostGet(c, true, session, redisClient)
	})

	r.DELETE("/posts/:id", func(c *gin.Context) {
		handlers.HandlePostDelete(c, session, redisClient, es)
	})

	r.Run()
}
