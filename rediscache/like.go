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

func ThrowLikeError(c *gin.Context, err error) {
	fmt.Println(err)
	c.JSON(http.StatusNotFound, "Error Liking post")
	c.AbortWithStatus(http.StatusBadRequest)
}
func ThrowUnlikeError(c *gin.Context, err error) {
	fmt.Println(err)
	c.JSON(http.StatusNotFound, "Error Unliking post")
	c.AbortWithStatus(http.StatusBadRequest)
}

func GetPostLikes(postID string, redisClient *redis.Client, ctx context.Context, session *gocql.Session) int {
	likeCountKey := fmt.Sprintf("post:%s:likes", postID)
	isCached, err := redisClient.Exists(ctx, likeCountKey).Result()
	if err != nil {
		fmt.Println(err)
	}
	if isCached == 0 {
		var likeCount int
		if err := session.Query(`SELECT likes from post_interactions WHERE post_id=?`, postID).Scan(&likeCount); err != nil {
			fmt.Println(err)
		}
		redisClient.Set(ctx, likeCountKey, likeCount, time.Hour).Err()
		return likeCount
	} else {
		likeCount, err := redisClient.Get(ctx, likeCountKey).Int()
		if err != nil {
			fmt.Println(err)
		}
		redisClient.Expire(ctx, likeCountKey, time.Hour).Result()
		return likeCount
	}
}
func Like(postID string, redisClient *redis.Client, ctx context.Context, c *gin.Context, session *gocql.Session, comment bool, parentID string) int {
	likeCountKey := fmt.Sprintf("post:%s:likes", postID)
	GetPostLikes(postID, redisClient, ctx, session)
	err := redisClient.Incr(ctx, likeCountKey).Err()
	if err != nil {
		ThrowLikeError(c, err)
	}
	likeCount, err := redisClient.Get(ctx, likeCountKey).Int()
	if err != nil {
		ThrowLikeError(c, err)
	}
	if comment {
		UpdateLikeRanking(redisClient, ctx, likeCount, postID, parentID, float64(1))
	}
	return likeCount
}
func UpdateLikeRanking(redisClient *redis.Client, ctx context.Context, count int, commentID string, postID string, incrAmt float64) {
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
		fmt.Printf("Comment %d: %s - %f likes\n", i+1, comment.Member.(string), comment.Score)
	}
}
func Unlike(postID string, redisClient *redis.Client, ctx context.Context, c *gin.Context, session *gocql.Session, comment bool, parentID string) int {
	likeCountKey := fmt.Sprintf("post:%s:likes", postID)
	GetPostLikes(postID, redisClient, ctx, session)
	err := redisClient.Decr(ctx, likeCountKey).Err()
	if err != nil {
		ThrowUnlikeError(c, err)
	}
	likeCount, err := redisClient.Get(ctx, likeCountKey).Int()
	if err != nil {
		ThrowUnlikeError(c, err)
	}
	if comment {
		UpdateLikeRanking(redisClient, ctx, likeCount, postID, parentID, float64(-1))
	}
	return likeCount
}

func GetRankingByLikes(redisClient *redis.Client, ctx context.Context, page int) {

}
func GetRankingByDateLatest(redisClient *redis.Client, ctx context.Context, page int) {

}
func GetRankingByDateEarliest(redisClient *redis.Client, ctx context.Context, page int) {

}
