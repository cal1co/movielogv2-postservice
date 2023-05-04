package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	cacheoperations "github.com/cal1co/movielogv2-postservice/rediscache"
	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
	"github.com/redis/go-redis/v9"
)

type Post struct {
	ID          gocql.UUID `json:"post_id"`
	UserID      int        `json:"user_id"`
	PostContent string     `json:"post_content"`
	CreatedAt   time.Time  `json:"created_at"`
	Likes       int        `json:"like_count"`
	Comments    int        `json:"comments_count"`
}

type Comment struct {
	ID          gocql.UUID `json:"comment_id"`
	UserID      int        `json:"user_id"`
	ParentID    gocql.UUID `json:"parent_id"`
	PostContent string     `json:"comment_content"`
	CreatedAt   time.Time  `json:"created_at"`
	Likes       int        `json:"like_count"`
	Comments    int        `json:"comments_count"`
}
type PostInteraction struct {
	PostId   gocql.UUID
	Likes    int
	Comments int
}
type ReqUser struct {
	UserID int `json:"user_id"`
}

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

	r.POST("/post", func(c *gin.Context) {
		var post Post
		if err := c.BindJSON(&post); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		post.ID = gocql.TimeUUID()

		if err := session.Query(`INSERT INTO posts (post_id, user_id, post_content, created_at) VALUES (?, ?, ?, ?)`, post.ID, post.UserID, post.PostContent, time.Now()).Exec(); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, count not post with details %v, %d, %s", post.ID, post.UserID, post.PostContent))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		post.Likes = 0
		post.Comments = 0
		c.JSON(http.StatusCreated, post)
	})

	r.POST("/post/:id/comment", func(c *gin.Context) {
		var comment Comment
		if err := c.BindJSON(&comment); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, "ERROR POSTING")
			return
		}
		comment.ID = gocql.TimeUUID()
		parentId, err := gocql.ParseUUID(c.Param("id"))
		if err != nil {
			fmt.Println(err)
		}
		comment.ParentID = parentId

		if err := session.Query(`INSERT INTO post_comments (comment_id, user_id, parent_post_id, comment_content, created_at) VALUES (?, ?, ?, ?, ?)`, comment.ID, comment.UserID, comment.ParentID, comment.PostContent, time.Now()).Exec(); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, "Error commenting")
			return
		}
		comment.Likes = 0
		comment.Comments = 0

		c.JSON(http.StatusCreated, comment)

	})

	r.POST("/post/:id/like", func(c *gin.Context) {
		handleLike(c, false)
	})
	r.POST("/comment/:id/like", func(c *gin.Context) {
		handleLike(c, true)
	})

	r.POST("/post/:id/unlike", func(c *gin.Context) {
		handleUnlike(c, false)
	})
	r.POST("/comment/:id/unlike", func(c *gin.Context) {
		handleUnlike(c, true)
	})

	r.GET("/posts/:id", func(c *gin.Context) {
		handlePostGet(c, false)
	})

	r.GET("/comments/:id", func(c *gin.Context) {
		handlePostGet(c, true)
	})

	r.GET("/user/:id/posts/", func(c *gin.Context) {
		uid, err := strconv.Atoi(c.Param("id"))
		// page := c.Param("page")
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		// offset := (pageNum - 1) * 15
		var posts []Post
		iter := session.Query(`SELECT post_id, post_content, created_at, user_id FROM posts WHERE user_id = ? LIMIT 15;`, uid).Iter()
		var post Post
		for iter.Scan(&post.ID, &post.PostContent, &post.CreatedAt, &post.UserID) {
			posts = append(posts, post)
		}
		if err := iter.Close(); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not fetch post results for user with id %v", uid))
			c.AbortWithStatus(http.StatusNotFound)
		}
		c.JSON(http.StatusOK, posts)
	})

	r.DELETE("/posts/:id", func(c *gin.Context) {
		post_id, err := gocql.ParseUUID(c.Param("id"))
		if err != nil {
			fmt.Println(err)
		}
		var reqUser ReqUser
		if err := c.BindJSON(&reqUser); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		iter := session.Query(`SELECT created_at FROM posts WHERE user_id = ? and post_id=?`, reqUser.UserID, post_id).Iter()
		var timestamp time.Time
		for iter.Scan(&timestamp) {
			fmt.Println(timestamp)
		}
		if err := iter.Close(); err != nil {
			fmt.Println(err)
		}
		batch := gocql.NewBatch(gocql.LoggedBatch)
		batch.Query(`DELETE FROM posts WHERE post_id = ? AND user_id=? AND created_at=?`, post_id, reqUser.UserID, timestamp)
		batch.Query(`DELETE FROM user_likes WHERE post_id = ?`, post_id)
		batch.Query(`DELETE FROM post_interactions WHERE post_id = ?`, post_id)
		if err := session.ExecuteBatch(batch); err != nil {
			fmt.Println("Error with batch:", err)
			return
		}

		c.JSON(http.StatusOK, fmt.Sprintf("Deleted post with id %s", post_id))
	})

	// Run the server
	r.Run()
}

func handlePost() {

}
func handleUnlike(c *gin.Context, comment bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post_id := c.Param("id")
	var reqUser ReqUser
	if err := c.BindJSON(&reqUser); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	var parent string
	if comment {
		if err := session.Query(`select parent_post_id from post_comments where comment_id=?`, post_id).Scan(&parent); err != nil {
			fmt.Println("error checking likes", err)
			c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
			return
		}
	} else {
		parent = ""
	}

	var likeCount int
	if err := session.Query(`SELECT COUNT(*) FROM user_likes WHERE post_id=? AND user_id=?`, post_id, reqUser.UserID).Scan(&likeCount); err != nil {
		// Handle error
		fmt.Println("Error checking user likes:", err)
		c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
		return
	}
	if likeCount == 0 {
		// Post has already been liked by the user
		c.JSON(http.StatusBadRequest, "Sorry, you have not liked this post yet.")
		return
	}

	cacheoperations.Unlike(post_id, redisClient, ctx, c, session, comment, parent)

	if err := session.Query(`DELETE FROM user_likes WHERE user_id=? AND post_id=?`, reqUser.UserID, post_id).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, fmt.Sprintf("Post with id %s has been unliked", post_id))

}

func handleLike(c *gin.Context, comment bool) {
	// pass in a bool that checks if comment or not.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post_id := c.Param("id")
	var reqUser ReqUser
	if err := c.BindJSON(&reqUser); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	var parent string
	if comment {
		if err := session.Query(`select parent_post_id from post_comments where comment_id=?`, post_id).Scan(&parent); err != nil {
			fmt.Println("error checking likes", err)
			c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
			return
		}
	} else {
		parent = "null"
	}

	// check if already liked
	var likeCount int
	if err := session.Query(`SELECT COUNT(*) FROM user_likes WHERE post_id=? AND user_id=?`, post_id, reqUser.UserID).Scan(&likeCount); err != nil {
		// Handle error
		fmt.Println("Error checking user likes:", err)
		c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
		return
	}
	if likeCount > 0 {
		// Post has already been liked by the user
		c.JSON(http.StatusBadRequest, "Sorry, you have already liked this post.")
		return
	}

	cacheoperations.Like(post_id, redisClient, ctx, c, session, comment, parent)

	if err := session.Query(`INSERT INTO user_likes (user_id, post_id, created_at) VALUES (?, ?, ?)`, reqUser.UserID, post_id, time.Now()).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, fmt.Sprintf("Post with id %s has been liked", post_id))
}

func handlePostGet(c *gin.Context, comment bool) {
	post_id := c.Param("id")
	var query string
	if comment {
		query = `SELECT comment_id, user_id, comment_content, created_at FROM post_comments WHERE comment_id = ? LIMIT 1`
	} else {
		query = `SELECT post_id, user_id, post_content, created_at FROM posts WHERE post_id = ? LIMIT 1`
	}
	var post Post
	if err := session.Query(query, post_id).Consistency(gocql.One).Scan(&post.ID, &post.UserID, &post.PostContent, &post.CreatedAt); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, post with id '%s' could not be found", post_id))
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	// need to populate likes. if in cache, get. if not, get from cassandra.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	like_count := cacheoperations.GetPostLikes(post_id, redisClient, ctx, session)
	post.Likes = like_count

	fmt.Println("POST: ", post)
	c.JSON(http.StatusOK, post)
}

func handlePostDelete() {

}
