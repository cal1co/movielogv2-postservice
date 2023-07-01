package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	cacheoperations "github.com/cal1co/movielogv2-postservice/rediscache"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
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
type PostRes struct {
	Post
	Liked bool `json:"liked"`
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

func throwError(message string, c *gin.Context) {
	c.JSON(http.StatusNotFound, message)
	c.AbortWithStatus(http.StatusBadRequest)
}
func ThrowUserIDExtractError(c *gin.Context) {
	c.JSON(http.StatusNotFound, "Couldn't extract uid")
	c.AbortWithStatus(http.StatusBadRequest)
}
func CheckLikedByUser(uid string, postId string, session *gocql.Session) bool {

	var likeCount int
	if err := session.Query(`SELECT COUNT(*) FROM user_likes WHERE post_id=? AND user_id=?`, postId, uid).Scan(&likeCount); err != nil {
		fmt.Println("Error checking user likes:", err)
		return false
	}
	if likeCount > 0 {
		return true
	} else {
		return false
	}
}
func HandlePost(c *gin.Context, session *gocql.Session) {
	userID, exists := c.Get("user_id")
	if !exists {
		ThrowUserIDExtractError(c)
		return
	}
	uid := int(userID.(float64))

	var post Post
	if err := c.BindJSON(&post); err != nil {
		fmt.Println(err)
		throwError("error unmarshling payload", c)
		return
	}

	post.UserID = uid
	post.ID = gocql.TimeUUID()
	post.Likes = 0
	post.Comments = 0

	if err := session.Query(`INSERT INTO posts (post_id, user_id, post_content, created_at) VALUES (?, ?, ?, ?)`, post.ID, post.UserID, post.PostContent, time.Now()).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, count not post with details %v, %d, %s", post.ID, post.UserID, post.PostContent))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	err := fanoutPost(post)
	if err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, count not post with details %v, %d, %s", post.ID, post.UserID, post.PostContent))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusCreated, post)
}

func fanoutPost(post Post) error {
	endpoint := "http://localhost:8081/post"
	payload, err := json.Marshal(post)
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("error adding post to user feeds - json error")
	}
	res, err := http.Post(endpoint, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("error adding post to user feeds - post error")
	}
	defer res.Body.Close()

	fmt.Println("Response status code:", res.StatusCode)
	return nil
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
		ThrowUserIDExtractError(c)
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
	comment_count := cacheoperations.GetPostComments(comment.ParentID.String(), redisClient, ctx, session)

	c.JSON(http.StatusCreated, comment_count)
}
func HandleUnlike(c *gin.Context, comment bool, session *gocql.Session, redisClient *redis.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post_id := c.Param("id")
	userID, exists := c.Get("user_id")
	if !exists {
		ThrowUserIDExtractError(c)
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

	likes := cacheoperations.Unlike(post_id, redisClient, ctx, c, session, comment, parent)

	if err := session.Query(`DELETE FROM user_likes WHERE user_id=? AND post_id=?`, uid, post_id).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not unlike post with id %s", post_id))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, likes)
}
func HandleLike(c *gin.Context, comment bool, session *gocql.Session, redisClient *redis.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post_id := c.Param("id")
	userID, exists := c.Get("user_id")
	if !exists {
		ThrowUserIDExtractError(c)
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

	likes := cacheoperations.Like(post_id, redisClient, ctx, c, session, comment, parent)

	if err := session.Query(`INSERT INTO user_likes (user_id, post_id, created_at) VALUES (?, ?, ?)`, uid, post_id, time.Now()).Exec(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, likes)
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
func GetUserPosts(c *gin.Context, session *gocql.Session, redisClient *redis.Client) {
	uid := c.Param("id")
	var posts []PostRes
	iter := session.Query(`SELECT post_id, post_content, created_at, user_id FROM posts WHERE user_id = ? AND created_at < ? LIMIT 12;`, uid, time.Now()).Iter()
	var post PostRes
	for iter.Scan(&post.ID, &post.PostContent, &post.CreatedAt, &post.UserID) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		like_count := cacheoperations.GetPostLikes(post.ID.String(), redisClient, ctx, session)
		comment_count := cacheoperations.GetPostComments(post.ID.String(), redisClient, ctx, session)
		post.Likes = like_count
		post.Comments = comment_count

		post.Liked = CheckLikedByUser(uid, post.ID.String(), session)

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
func GetPostComments(c *gin.Context, session *gocql.Session) {
	post_id := c.Param("id")
	var comments []Comment
	iter := session.Query(`SELECT comment_id, comment_content, created_at, user_id FROM post_comments WHERE parent_post_id = ? LIMIT 10;`, post_id).Iter()
	var comment Comment
	uuid, err := gocql.ParseUUID(post_id)
	if err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not fetch comments results for post with id %v", post_id))
		c.AbortWithStatus(http.StatusNotFound)
	}
	for iter.Scan(&comment.ID, &comment.PostContent, &comment.CreatedAt, &comment.UserID) {
		comment.ParentID = uuid
		comments = append(comments, comment)
	}
	if err := iter.Close(); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not fetch comments results for post with id %v", post_id))
		c.AbortWithStatus(http.StatusNotFound)
	}

	c.JSON(http.StatusOK, comments)
	return
}

type CommentBatchDelete struct {
	comment_id string
	user_id    string
	created_at time.Time
	parent     string
}

func HandlePostDelete(c *gin.Context, session *gocql.Session, redisClient *redis.Client, es *elasticsearch.Client) {
	userID, exists := c.Get("user_id")
	if !exists {
		ThrowUserIDExtractError(c)
		return
	}
	uid := int(userID.(float64))
	postId := c.Param("id")

	var parentCreateTime time.Time
	var commentList []CommentBatchDelete

	iter := session.Query(`SELECT created_at FROM posts WHERE user_id=? and post_id=?`, uid, postId).Iter()
	for iter.Scan(&parentCreateTime) {
		fmt.Println("post with id:", parentCreateTime)
		commentList = getAllCommentDependents(postId, session)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	b.Entries = append(b.Entries, gocql.BatchEntry{
		Stmt:       "DELETE FROM posts WHERE post_id=? AND user_id=? and created_at=?;",
		Args:       []interface{}{postId, uid, parentCreateTime},
		Idempotent: true,
	})
	b.Entries = append(b.Entries, gocql.BatchEntry{
		Stmt:       "DELETE FROM post_interactions WHERE post_id=?;",
		Args:       []interface{}{postId},
		Idempotent: true,
	})
	for i := 0; i < len(commentList); i++ {
		b.Entries = append(b.Entries, gocql.BatchEntry{
			Stmt:       "DELETE FROM post_comments WHERE comment_id=? AND user_id=? and parent_post_id=?;",
			Args:       []interface{}{commentList[i].comment_id, commentList[i].user_id, commentList[i].parent},
			Idempotent: true,
		})
		b.Entries = append(b.Entries, gocql.BatchEntry{
			Stmt:       "DELETE FROM post_interactions WHERE post_id=?;",
			Args:       []interface{}{commentList[i].comment_id},
			Idempotent: true,
		})
	}
	err := session.ExecuteBatch(b)
	if err != nil {
		fmt.Println(err)
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	req := esapi.DeleteRequest{
		Index:      "posts",
		DocumentID: postId,
	}

	res, err := req.Do(context.Background(), es)
	if err != nil {
		fmt.Printf("Error deleting document: %s\n", err)
		return
	}

	if res.IsError() {
		fmt.Printf("Error deleting document: %s\n", res.Status())
		return
	}

	c.JSON(http.StatusOK, fmt.Sprintf("Deleted post with id %s", postId))
}
func getAllCommentDependents(post_id string, session *gocql.Session) []CommentBatchDelete {
	var comments []CommentBatchDelete
	var comment CommentBatchDelete

	iter := session.Query(`select comment_id, created_at, user_id, parent_post_id from post_comments where parent_post_id=?`, post_id).Iter()
	for iter.Scan(&comment.comment_id, &comment.created_at, &comment.user_id, &comment.parent) {
		comments = append(comments, comment)
		comments = append(comments, getAllCommentDependents(comment.comment_id, session)...)
	}
	if err := iter.Close(); err != nil {
		fmt.Println(err)
	}
	return comments
}

type Search struct {
	Query string
}

func HandleSearch(c *gin.Context, es *elasticsearch.Client) {

	var search Search
	if err := c.BindJSON(&search); err != nil {
		fmt.Println(err)
		throwError("ERROR WITH JSON UNMARSHAL", c)
		return
	}

	fmt.Printf("%s\n", search)
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"match": map[string]interface{}{
				"post_content": map[string]interface{}{
					"query":     search.Query,
					"fuzziness": "AUTO",
				},
			},
		},
		"highlight": map[string]interface{}{
			"fields": map[string]interface{}{
				"post_content": struct{}{},
			},
		},
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		fmt.Printf("Error marshaling query: %s\n", err)
		return
	}
	fmt.Println(queryJSON)

	req := esapi.SearchRequest{
		Index: []string{"posts"},
		Body:  bytes.NewReader(queryJSON),
	}

	res, err := req.Do(context.Background(), es)
	if err != nil {
		fmt.Printf("Error performing search request: %s\n", err)
		return
	}

	if res.IsError() {
		fmt.Printf("Error executing search: %s\n", res.Status())
		return
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		fmt.Printf("Error parsing search response: %s\n", err)
		return
	}

	hits, found := result["hits"].(map[string]interface{})["hits"].([]interface{})
	if !found || len(hits) == 0 {
		fmt.Println("No results found.")
		return
	}
	// var output []string
	for _, hit := range hits {
		source := hit.(map[string]interface{})["highlight"]
		// source := hit.(map[string]interface{})["_source"]
		fmt.Println(source)
		// output = append(output, source)
	}
	c.JSON(http.StatusCreated, hits)
	res.Body.Close()
}
