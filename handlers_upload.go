package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const UploadDir = "uploads"

// uploadImage 测量图片保存到本地 uploads 目录
func uploadImage(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请通过 image 字段上传图片"})
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 jpg/png/gif/webp 图片"})
		return
	}
	u := c.MustGet("user").(*authUser)
	name := fmt.Sprintf("u%d_%d%s", u.ID, time.Now().UnixNano(), ext)
	full := filepath.Join(UploadDir, name)
	if err := c.SaveUploadedFile(file, full); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "图片保存失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"url": "/uploads/" + name, "path": full})
}
