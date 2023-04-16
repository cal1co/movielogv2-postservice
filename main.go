package main

import (
	"fmt"
	"net/http"
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

func main() {
	defer session.Close()

	// Initialize Gin
	r := gin.Default()

	// POST /posts
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

	// GET /posts/:id
	r.GET("/posts/:id", func(c *gin.Context) {
		post_id := c.Param("id")
		id, err := gocql.ParseUUID(post_id)
		if err != nil {
			// handle err
		}
		fmt.Println(id)
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

	// DELETE /posts/:id
	r.DELETE("/posts/:id", func(c *gin.Context) {
		// id := gocql.ParseUUID(c.Param("id"))

		// // Delete the post from Cassandra
		// if err := session.Query(`DELETE FROM posts WHERE id = ?`, id).Exec(); err != nil {
		// 	c.AbortWithStatus(http.StatusInternalServerError)
		// 	return
		// }

		// c.Status(http.StatusNoContent)
	})

	// Run the server
	r.Run()
}
