package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
)

type Post struct {
	ID          gocql.UUID `json:"post_id"`
	UserID      int        `json:"user_id"`
	PostContent string     `json:"post_content"`
	CreatedAt   time.Time  `json:"created_at"`
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

func init() {
	// Connect to Cassandra
	cluster := gocql.NewCluster("127.0.0.1")
	cluster.Keyspace = "user_posts"
	var err error
	session, err = cluster.CreateSession()
	if err != nil {
		panic(err)
	}
}

const pageSize int = 15

func main() {
	defer session.Close()

	// Initialize Gin
	r := gin.Default()

	// POST post
	r.POST("/posts", func(c *gin.Context) {
		var post Post
		if err := c.BindJSON(&post); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		// Generate a UUID for the post
		post.ID = gocql.TimeUUID()

		// Insert the post into Cassandra
		if err := session.Query(`INSERT INTO posts (post_id, user_id, post_content, created_at) VALUES (?, ?, ?, ?)`, post.ID, post.UserID, post.PostContent, time.Now()).Exec(); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, count not post with details %v, %d, %s", post.ID, post.UserID, post.PostContent))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		c.JSON(http.StatusCreated, post)
	})

	r.POST("/posts/:id/like", func(c *gin.Context) {
		post_id := c.Param("id")

		var reqUser ReqUser
		if err := c.BindJSON(&reqUser); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		// check if already liked
		var likeCount int
		if err := session.Query(`SELECT count(*) FROM user_likes WHERE post_id=? AND user_id=?`, post_id, reqUser.UserID).Scan(&likeCount); err != nil {
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
		// Increment the post's like counter in Cassandra
		// needs to batch instead
		if err := session.Query(`UPDATE post_interactions SET likes = likes + 1 WHERE post_id = ?`, post_id).Exec(); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		if err := session.Query(`INSERT INTO user_likes (user_id, post_id, created_at) VALUES (?, ?, ?)`, reqUser.UserID, post_id, time.Now()).Exec(); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		c.JSON(http.StatusOK, fmt.Sprintf("Post with id %s has been liked", post_id))
	})

	// GET specific post
	r.GET("/posts/:id", func(c *gin.Context) {
		post_id := c.Param("id")

		// Retrieve the post from Cassandra
		var post Post
		if err := session.Query(`SELECT post_id, user_id, post_content, created_at FROM posts WHERE post_id = ? LIMIT 1`, post_id).Consistency(gocql.One).Scan(&post.ID, &post.UserID, &post.PostContent, &post.CreatedAt); err != nil {
			fmt.Println(err)
			c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, post with id '%s' could not be found", post_id))
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		fmt.Println("POST: ", post)
		c.JSON(http.StatusOK, post)
	})

	// GET user posts - returns first 15
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

	// GET user posts - past latest 15
	// r.GET("/user/:id/posts/")

	// DELETE /posts/:id
	// need to delete likes also!
	r.DELETE("/posts/:id", func(c *gin.Context) {
		post_id, err := gocql.ParseUUID(c.Param("id"))
		if err != nil {
			// handle err
			fmt.Println(err)
		}

		// // Delete the post from Cassandra
		batch := gocql.NewBatch(gocql.LoggedBatch)
		batch.Query(`DELETE FROM posts WHERE post_id = ?`, post_id)
		// batch.Query(`DELETE FROM user_likes WHERE post_id = ?`, post_id)
		// batch.Query(`DELETE FROM post_interactions WHERE post_id = ?`, post_id)
		if err := session.ExecuteBatch(batch); err != nil {
			fmt.Println(err)
			return
		}

		c.JSON(http.StatusOK, fmt.Sprintf("Deleted post with id %s", post_id))
	})

	// Run the server
	r.Run()
}
