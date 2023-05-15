package handlers

import (
	"context"
	"fmt"
	"net/http"
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

func HandlePost(c *gin.Context, session *gocql.Session) {
	var post Post
	if err := c.BindJSON(&post); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, "ERROR WITH JSON UNMARSHAL")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		fmt.Println(userID, exists)
		c.JSON(http.StatusNotFound, "Couldn't extract uid")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	uid := int(userID.(float64))
	post.UserID = uid
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
}
func HandleComment(c *gin.Context, session *gocql.Session, redisClient *redis.Client, isComment bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var comment Comment
	if err := c.BindJSON(&comment); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, "ERROR POSTING")
		return
	}
	comment.ID = gocql.TimeUUID()
	userID, exists := c.Get("user_id")
	if !exists {
		fmt.Println(userID, exists)
		c.JSON(http.StatusNotFound, "Couldn't extract uid")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	uid := int(userID.(float64))
	comment.UserID = uid

	parentId, err := gocql.ParseUUID(c.Param("id"))
	if err != nil {
		fmt.Println(err)
	}
	comment.ParentID = parentId

	var parent string
	if isComment {
		if err := session.Query(`select parent_post_id from post_comments where comment_id=?`, parentId).Scan(&parent); err != nil {
			fmt.Println("error checking likes", err)
			c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
			return
		}
	} else {
		parent = "null"
	}

	if err := session.Query(`INSERT INTO post_comments (comment_id, user_id, parent_post_id, comment_content, created_at) VALUES (?, ?, ?, ?, ?)`, comment.ID, comment.UserID, comment.ParentID, comment.PostContent, time.Now()).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, "Error commenting")
		return
	}
	comment.Likes = 0
	comment.Comments = 0

	cacheoperations.Comment(comment.ParentID.String(), redisClient, ctx, c, session, parent)

	c.JSON(http.StatusCreated, comment)
}
func HandleUnlike(c *gin.Context, comment bool, session *gocql.Session, redisClient *redis.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post_id := c.Param("id")
	userID, exists := c.Get("user_id")
	if !exists {
		fmt.Println(userID, exists)
		c.JSON(http.StatusNotFound, "Couldn't extract uid")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	uid := int(userID.(float64))
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
	if err := session.Query(`SELECT COUNT(*) FROM user_likes WHERE post_id=? AND user_id=?`, post_id, uid).Scan(&likeCount); err != nil {
		fmt.Println("Error checking user likes:", err)
		c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
		return
	}
	if likeCount == 0 {
		c.JSON(http.StatusBadRequest, "Sorry, you have not liked this post yet.")
		return
	}

	cacheoperations.Unlike(post_id, redisClient, ctx, c, session, comment, parent)

	if err := session.Query(`DELETE FROM user_likes WHERE user_id=? AND post_id=?`, uid, post_id).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, fmt.Sprintf("Post with id %s has been unliked", post_id))
}
func HandleLike(c *gin.Context, comment bool, session *gocql.Session, redisClient *redis.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post_id := c.Param("id")
	userID, exists := c.Get("user_id")
	if !exists {
		fmt.Println(userID, exists)
		c.JSON(http.StatusNotFound, "Couldn't extract uid")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	uid := int(userID.(float64))
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

	var likeCount int
	if err := session.Query(`SELECT COUNT(*) FROM user_likes WHERE post_id=? AND user_id=?`, post_id, uid).Scan(&likeCount); err != nil {
		fmt.Println("Error checking user likes:", err)
		c.JSON(http.StatusInternalServerError, "Sorry, could not check if user has liked post.")
		return
	}
	if likeCount > 0 {
		c.JSON(http.StatusBadRequest, "Sorry, you have already liked this post.")
		return
	}

	cacheoperations.Like(post_id, redisClient, ctx, c, session, comment, parent)

	if err := session.Query(`INSERT INTO user_likes (user_id, post_id, created_at) VALUES (?, ?, ?)`, uid, post_id, time.Now()).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, fmt.Sprintf("Post with id %s has been liked", post_id))
}
func HandlePostGet(c *gin.Context, comment bool, session *gocql.Session, redisClient *redis.Client) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	like_count := cacheoperations.GetPostLikes(post_id, redisClient, ctx, session)
	post.Likes = like_count
	comment_count := cacheoperations.GetPostComments(post_id, redisClient, ctx, session)
	post.Comments = comment_count

	fmt.Println("POST: ", post)
	c.JSON(http.StatusOK, post)
}
func GetUserPosts(c *gin.Context, session *gocql.Session) {
	fmt.Println("GETTING POSTS!")
	// userID, exists := c.Get("user_id")
	// if !exists {
	// 	fmt.Println(userID, exists)
	// 	c.JSON(http.StatusNotFound, "Couldn't extract uid")
	// 	c.AbortWithStatus(http.StatusBadRequest)
	// return
	// }
	uid := c.Param("id")
	// uid := int(userID.(float64))
	var posts []Post
	iter := session.Query(`SELECT post_id, post_content, created_at, user_id FROM posts WHERE user_id = ? AND created_at < ? LIMIT 12;`, uid, time.Now()).Iter()
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
	return
}
func HandlePostDelete(c *gin.Context, session *gocql.Session) {
	post_id, err := gocql.ParseUUID(c.Param("id"))
	if err != nil {
		fmt.Println(err)
	}
	userID, exists := c.Get("user_id")
	if !exists {
		fmt.Println(userID, exists)
		c.JSON(http.StatusNotFound, "Couldn't extract uid")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	uid := int(userID.(float64))

	iter := session.Query(`SELECT created_at FROM posts WHERE user_id = ? and post_id=?`, uid, post_id).Iter()
	var timestamp time.Time
	for iter.Scan(&timestamp) {
		fmt.Println(timestamp)
	}
	if err := iter.Close(); err != nil {
		fmt.Println(err)
	}
	batch := gocql.NewBatch(gocql.LoggedBatch)
	batch.Query(`DELETE FROM posts WHERE post_id = ? AND user_id=? AND created_at=?`, post_id, uid, timestamp)
	batch.Query(`DELETE FROM user_likes WHERE post_id = ?`, post_id)
	batch.Query(`DELETE FROM post_interactions WHERE post_id = ?`, post_id)
	if err := session.ExecuteBatch(batch); err != nil {
		fmt.Println("Error with batch:", err)
		return
	}

	c.JSON(http.StatusOK, fmt.Sprintf("Deleted post with id %s", post_id))
}
