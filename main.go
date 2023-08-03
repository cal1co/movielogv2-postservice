package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	handlers "github.com/cal1co/movielogv2-postservice/handlers"
	middleware "github.com/cal1co/movielogv2-postservice/middleware"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

var session *gocql.Session
var redisClient *redis.Client

type PostHandler struct {
	Session *gocql.Session
}

func init() {
	cluster := gocql.NewCluster("cassandra")
	cluster.Keyspace = "user_posts"
	var err error
	session, err = cluster.CreateSession()
	if err != nil {
		panic(err)
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     "yuzu-post-interactions:6379",
		Password: "",
		DB:       0,
	})
}

func MigrateLikesToDB() {
	handleMigration("post:*:likes", ":likes", "UPDATE post_interactions SET likes = ? WHERE post_id = ?")
	handleMigration("post:*:commentcount", ":commentcount", "UPDATE post_interactions SET comments = ? WHERE post_id = ?")
}

func handleMigration(key string, suffix string, query string) {
	ctx := context.Background()
	cursor := uint64(0)
	keys := []string{}
	for {
		var err error
		likesCache, cursor, err := redisClient.Scan(ctx, cursor, key, 100).Result()
		if err != nil {
			log.Printf("Error scanning Redis keys: %v", err)
			return
		}
		log.Printf("%s", likesCache)
		keys = append(keys, likesCache...)
		if cursor == 0 {
			break
		}
	}
	for _, key := range keys {
		postId := strings.TrimPrefix(strings.TrimSuffix(key, suffix), "post:")
		count, err := redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Error getting count for post %s: %v", postId, err)
			continue
		}
		err = session.Query(query, count, postId).Exec()
		if err != nil {
			log.Printf("Error updating count for post %s: %v", postId, err)
			continue
		}
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
			time.Sleep(time.Minute * 5)
		}
	}()

	loadEnv()

	r := gin.Default()

	config := cors.DefaultConfig()
	config.AllowMethods = []string{"GET", "POST", "DELETE", "OPTIONS"}
	config.AddAllowHeaders("Authorization")
	config.AllowOrigins = []string{"http://localhost:5173", "http://localhost:3000"}

	r.Use(cors.New(config))

	cert, _ := ioutil.ReadFile(os.Getenv("ELASTIC_CERT_PATH"))
	cfg := elasticsearch.Config{
		Addresses: []string{"ELASTIC_ADDRESS"},
		Username:  os.Getenv("ELASTIC_USERNAME"),
		Password:  os.Getenv("ELASTIC_PASSWORD"),
		CACert:    cert,
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		fmt.Printf("Error creating the client: %s\n", err)
		return
	}

	r.Use(middleware.RateLimiterMiddleware())

	handler := &handlers.Handler{
		Session: session,
	}

	authRoutes := r.Group("/")
	authRoutes.Use(middleware.AuthMiddleware())
	authRoutes.Use(middleware.ActivityTrackerMiddleware(redisClient))
	authRoutes.GET("/posts/user/:id", func(c *gin.Context) {
		handlers.HandleGetUserPosts(c, handler, redisClient)
	})

	authRoutes.POST("/post", func(c *gin.Context) {
		handlers.HandlePost(c, handler)
	})

	authRoutes.POST("/post/:id/comment", func(c *gin.Context) {
		handlers.HandleComment(c, handler, redisClient, false)
	})

	authRoutes.GET("/post/:id/comments", func(c *gin.Context) {
		handlers.GetPostComments(c, handler, redisClient)
	})

	authRoutes.POST("/comment/:id/comment", func(c *gin.Context) {
		handlers.HandleComment(c, handler, redisClient, true)
	})

	authRoutes.POST("/post/like/:id", func(c *gin.Context) {
		handlers.HandleLike(c, false, handler, redisClient)
	})

	authRoutes.POST("/post/unlike/:id", func(c *gin.Context) {
		handlers.HandleUnlike(c, false, handler, redisClient)
	})

	authRoutes.POST("/comment/:id/like", func(c *gin.Context) {
		handlers.HandleLike(c, true, handler, redisClient)
	})

	authRoutes.POST("/comment/:id/unlike", func(c *gin.Context) {
		handlers.HandleUnlike(c, true, handler, redisClient)
	})

	authRoutes.GET("/feed/user/:id", func(c *gin.Context) {
		handlers.GetUserPosts(c, handler, redisClient)
	})

	authRoutes.GET("/posts/:id", func(c *gin.Context) {
		handlers.HandlePostGet(c, false, handler, redisClient)
	})

	authRoutes.GET("/comments/:id", func(c *gin.Context) {
		handlers.HandlePostGet(c, true, handler, redisClient)
	})

	authRoutes.POST("/posts/search", func(c *gin.Context) {
		handlers.HandleSearch(c, es)
	})

	authRoutes.DELETE("/posts/:id", func(c *gin.Context) {
		handlers.HandlePostDelete(c, handler, redisClient, es)
	})

	authRoutes.POST("/post/media", func(c *gin.Context) {
		handlers.HandleAddMediaToPost(c, handler)
	})

	r.POST("/posts/feed/:id", func(c *gin.Context) {
		handlers.HandleFeedPosts(c, handler, redisClient)
	})

	go func() {
		if err := r.Run(":8080"); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down server...")

	log.Println("Server shutdown complete")
}
