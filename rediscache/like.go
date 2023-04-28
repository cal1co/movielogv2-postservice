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

// // Increment the post's like counter in Cassandra
// // needs to batch instead
// if err := session.Query(`UPDATE post_interactions SET likes = likes + 1 WHERE post_id = ?`, post_id).Exec(); err != nil {
// 	fmt.Println(err)
// 	c.JSON(http.StatusNotFound, fmt.Sprintf("Sorry, could not like post with id %s", post_id))
// 	c.AbortWithStatus(http.StatusInternalServerError)
// 	return
// }

func ThrowLikeError(c *gin.Context, err error) {
	fmt.Println(err)
	c.JSON(http.StatusNotFound, "Error Liking post")
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
	fmt.Println(likeCountKey, likeCount)
	if comment {
		fmt.Println("UPDATING RANKING")
		UpdateRanking(redisClient, ctx, likeCountKey, likeCount, postID, parentID)
	}
	return likeCount
}

func UpdateRanking(redisClient *redis.Client, ctx context.Context, likeCountKey string, count int, commentID string, postID string) {
	// need to get postid
	parentPostId := fmt.Sprintf("post:%s:comments", postID)
	exists, err := redisClient.ZScore(ctx, parentPostId, commentID).Result()
	if exists == 0 {
		redisClient.ZAdd(ctx, parentPostId, redis.Z{
			Score:  float64(count),
			Member: commentID,
		})
	} else if err != nil {
		fmt.Println(err)
	} else {
		_, err := redisClient.ZIncrBy(ctx, parentPostId, float64(1), commentID).Result()
		if err != nil {
			fmt.Println(err)
		}
	}
	comments, err := redisClient.ZRevRangeWithScores(ctx, parentPostId, 0, -1).Result()
	fmt.Println("COMMENTS:", comments)
	if err != nil {
		fmt.Println(err)
	}
	for i, comment := range comments {
		fmt.Printf("Comment %d: %s - %f likes\n", i+1, comment.Member.(string), comment.Score)
	}
}

// func LikePost(ctx context.Context, redisClient *redis.Client, postID string, c *gin.Context) {
// 	likeCountKey := fmt.Sprintf("post:%s:likes", postID)
// 	Like(likeCountKey, redisClient, ctx, c)
// }

// func LikeComment(ctx context.Context, redisClient *redis.Client, commentID string, c *gin.Context, postID string) {
// 	likeCountKey := fmt.Sprintf("comment:%s:likes", commentID)
// 	count := Like(likeCountKey, redisClient, ctx, c)
// 	UpdateRanking(redisClient, ctx, likeCountKey, count, commentID, postID)
// }
