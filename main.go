package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:web
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	dbPath := flag.String("db", "countertop.db", "SQLite 数据库文件")
	seed := flag.Bool("seed", false, "写入演示数据（超差/未确认/加工后换版/返工 四个例子）")
	reset := flag.Bool("reset", false, "启动前删除旧数据库与图片目录")
	flag.Parse()

	if *reset {
		_ = os.Remove(*dbPath)
		_ = os.RemoveAll(UploadDir)
	}
	if err := os.MkdirAll(UploadDir, 0o755); err != nil {
		log.Fatal(err)
	}

	gin.SetMode(gin.ReleaseMode)
	initDB(*dbPath)
	seedUsers()
	if *seed {
		if err := seedData(); err != nil {
			log.Fatalf("种子数据失败: %v", err)
		}
		log.Println("演示数据已写入")
	}

	r := gin.Default()
	registerRoutes(r)

	// 前端静态文件（go:embed，单文件部署）
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/uploads/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.FileFromFS(path, http.FS(sub))
	})

	log.Printf("厨房台面开孔复核与返工系统已启动: http://localhost%s", *addr)
	if err := r.Run(*addr); err != nil {
		log.Fatal(err)
	}
}
