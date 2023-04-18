package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
)

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// Verify the token's signature using a secret key
			return []byte("my-secret-key"), nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			return
		}

		userID, ok := claims["user_id"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user ID in token"})
			return
		}

		// Check if the user exists in your database and has the necessary permissions
		// to perform the requested operation.
		// ...

		// Set the user ID in the context for further processing in the request handler.
		c.Set("user_id", userID)

		// Call the next middleware or the request handler.
		c.Next()
	}
}

// r.GET("/posts", authMiddleware(), func(c *gin.Context) {
//     userID := c.GetString("user_id")
//     // Fetch all posts for the user with the given ID and return the response.
//     // ...
// })
