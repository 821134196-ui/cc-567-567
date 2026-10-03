package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var (
	sessionsMu sync.Mutex
	sessions   = map[string]uint{} // token -> userID
)

func loginHandler(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var user User
	if err := db.Where("username = ?", req.Username).First(&user).Error; err != nil || user.Password != req.Password {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	sessionsMu.Lock()
	sessions[token] = user.ID
	sessionsMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

func logoutHandler(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func currentUser(c *gin.Context) *User {
	u, ok := c.Get("user")
	if !ok {
		return nil
	}
	user := u.(User)
	return &user
}

func authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		sessionsMu.Lock()
		uid, ok := sessions[token]
		sessionsMu.Unlock()
		if token == "" || !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或登录已过期"})
			return
		}
		var user User
		if err := db.First(&user, uid).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
			return
		}
		c.Set("user", user)
		c.Next()
	}
}

// requireRole 限制接口只能由指定岗位调用
func requireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		if u == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		for _, r := range roles {
			if u.Role == r {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "当前岗位无权执行此操作"})
	}
}
