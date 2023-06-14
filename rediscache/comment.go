package cacheoperations

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gocql/gocql"
	"github.com/redis/go-redis/v9"
)

func ThrowCommentError(c *gin.Context, err error) {
	fmt.Println(err)
	c.JSON(http.StatusNotFound, "Error commenting on post")
	c.AbortWithStatus(http.StatusBadRequest)
}
func ThrowDeleteCommentError(c *gin.Context, err error) {
	fmt.Println(err)
	c.JSON(http.StatusNotFound, "Error deleting comment post")
	c.AbortWithStatus(http.StatusBadRequest)
}
func GetPostComments(postID string, redisClient *redis.Client, ctx context.Context, session *gocql.Session) int {
	commentCountKey := fmt.Sprintf("post:%s:commentcount", postID)
	isCached, err := redisClient.Exists(ctx, commentCountKey).Result()
	if err != nil {
		fmt.Println("cache err:", err)
	}
	if isCached == 0 {
		var commentCount int
		if err := session.Query(`SELECT comments from post_interactions WHERE post_id=?`, postID).Scan(&commentCount); err != nil {
			fmt.Println(err)
		}
		redisClient.Set(ctx, commentCountKey, commentCount, time.Hour).Err()
		return commentCount
	} else {
		commentCount, err := redisClient.Get(ctx, commentCountKey).Int()
		if err != nil {
			fmt.Println(err)
		}
		redisClient.Expire(ctx, commentCountKey, time.Hour).Result()
		return commentCount
	}
}
func Comment(postID string, redisClient *redis.Client, ctx context.Context, c *gin.Context, session *gocql.Session, parentID string) int {
	commentCountKey := fmt.Sprintf("post:%s:commentcount", postID)
	GetPostComments(postID, redisClient, ctx, session)
	err := redisClient.Incr(ctx, commentCountKey).Err()
	if err != nil {
		ThrowCommentError(c, err)
	}
	commentCount, err := redisClient.Get(ctx, commentCountKey).Int()
	if err != nil {
		ThrowCommentError(c, err)
	}
	UpdateCommentRanking(redisClient, ctx, commentCount, postID, parentID, float64(1))
	// fmt.Println("COMMENT COUNT", commentCount)
	return commentCount
}
func UpdateCommentRanking(redisClient *redis.Client, ctx context.Context, count int, commentID string, postID string, incrAmt float64) {
	parentPostId := fmt.Sprintf("post:%s:comments", postID)
	exists, err := redisClient.ZScore(ctx, parentPostId, commentID).Result()
	if err != nil {
		fmt.Println(err)
	}
	if exists == 0 {
		redisClient.ZAdd(ctx, parentPostId, redis.Z{
			Score:  float64(count),
			Member: commentID,
		})
	} else {
		_, err := redisClient.ZIncrBy(ctx, parentPostId, incrAmt, commentID).Result()
		if err != nil {
			fmt.Println(err)
		}
	}
	comments, err := redisClient.ZRevRangeWithScores(ctx, parentPostId, 0, -1).Result()
	if err != nil {
		fmt.Println(err)
	}
	for i, comment := range comments {
		fmt.Printf("Comment %d: %s - %f comments\n", i+1, comment.Member.(string), comment.Score)
	}
}
func DeleteComment(postID string, redisClient *redis.Client, ctx context.Context, c *gin.Context, session *gocql.Session, comment bool, parentID string) int {
	commentCountKey := fmt.Sprintf("post:%s:commentcount", postID)
	GetPostComments(postID, redisClient, ctx, session)
	err := redisClient.Decr(ctx, commentCountKey).Err()
	if err != nil {
		ThrowDeleteCommentError(c, err)
	}
	commentCount, err := redisClient.Get(ctx, commentCountKey).Int()
	if err != nil {
		ThrowDeleteCommentError(c, err)
	}
	if comment {
		UpdateCommentRanking(redisClient, ctx, commentCount, postID, parentID, float64(-1))
	}
	return commentCount
}

func GetRankingByComments(redisClient *redis.Client, ctx context.Context, page int) {

}
func GetCommentRankingByDateLatest(redisClient *redis.Client, ctx context.Context, page int) {

}
func GetCommentRankingByDateEarliest(redisClient *redis.Client, ctx context.Context, page int) {

}
