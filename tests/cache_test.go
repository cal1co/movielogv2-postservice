package cacheopeartions

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient interface {
	Exists(ctx context.Context, key string) *redis.IntCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

// func TestGetPostLikes(t *testing.T) {
// 	mockRedis := &MockRedisClient{}
// 	mockSession := &MockSession{}

// 	postID := "123"
// 	ctx := context.Background()

// 	likeCount := GetPostLikes(postID, mockRedis, ctx, mockSession)

// }
