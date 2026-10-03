package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	db      *gorm.DB
	tokens  = map[string]uint{} // token -> user id
	tokenMu sync.RWMutex
)

func initDB(path string) {
	var err error
	db, err = gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(
		&User{}, &Order{}, &DrawingVersion{}, &Opening{}, &MeasureRecord{},
		&Confirmation{}, &ProductionRecord{}, &Deviation{}, &ReworkEvent{},
		&CostRecord{}, &InstallationRecord{}, &AcceptanceRecord{},
	); err != nil {
		panic(err)
	}
}

func hashPassword(p string) string {
	h, _ := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(h)
}

func checkPassword(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type authUser struct {
	ID   uint
	Name string
	Role string
}

func issueToken(uid uint) string {
	t := newToken()
	tokenMu.Lock()
	tokens[t] = uid
	tokenMu.Unlock()
	return t
}

func currentUser(c *gin.Context) (*authUser, bool) {
	t := c.GetHeader("Authorization")
	if len(t) > 7 && t[:7] == "Bearer " {
		t = t[7:]
	}
	if t == "" {
		t = c.Query("token")
	}
	tokenMu.RLock()
	uid, ok := tokens[t]
	tokenMu.RUnlock()
	if !ok {
		return nil, false
	}
	var u User
	if err := db.First(&u, uid).Error; err != nil {
		return nil, false
	}
	return &authUser{ID: u.ID, Name: u.Name, Role: u.Role}, true
}

// authRequired 解析登录态
func authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := currentUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或登录已失效"})
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

// rolesRequired 限制岗位
func rolesRequired(roles ...string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		u, _ := c.Get("user")
		au := u.(*authUser)
		if !allowed[au.Role] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "无权操作：当前岗位(" + roleName(au.Role) + ")只能执行被授权的操作",
			})
			return
		}
		c.Next()
	}
}

func roleName(r string) string {
	switch r {
	case RoleSurveyor:
		return "测量员"
	case RoleFactory:
		return "工厂"
	case RoleInstaller:
		return "安装人员"
	case RoleCustomer:
		return "客户"
	}
	return r
}

func nowPtr(t time.Time) *time.Time { return &t }
