package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var db *gorm.DB

func main() {
	var err error
	db, err = gorm.Open(sqlite.Open("countertop.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("数据库打开失败:", err)
	}
	if err := db.AutoMigrate(
		&User{}, &Order{}, &Drawing{}, &Cutout{}, &MeasurementImage{},
		&Remeasure{}, &Confirmation{}, &Fabrication{}, &ActualCutout{},
		&FieldMeasurement{}, &ReworkPlan{}, &AuditLog{},
	); err != nil {
		log.Fatal("数据库迁移失败:", err)
	}
	if err := os.MkdirAll("uploads", 0o755); err != nil {
		log.Fatal("创建上传目录失败:", err)
	}
	seed()

	r := gin.Default()

	api := r.Group("/api")
	api.POST("/login", loginHandler)
	api.GET("/users", listUsers) // 演示账号列表（不含密码）

	authed := api.Group("", authRequired())
	authed.GET("/me", meHandler)
	authed.POST("/logout", logoutHandler)
	authed.GET("/orders", listOrders)
	authed.GET("/orders/:id", getOrder)

	// 测量员
	authed.POST("/orders", requireRole(RoleSurveyor), createOrder)
	authed.POST("/orders/:id/drawings", requireRole(RoleSurveyor), createDrawing)
	authed.POST("/drawings/:id/cutouts", requireRole(RoleSurveyor), addCutout)
	authed.PUT("/cutouts/:id", requireRole(RoleSurveyor), updateCutout)
	authed.DELETE("/cutouts/:id", requireRole(RoleSurveyor), deleteCutout)
	authed.POST("/cutouts/:id/images", requireRole(RoleSurveyor), uploadImage)
	authed.POST("/drawings/:id/submit", requireRole(RoleSurveyor), submitDrawing)

	// 客户
	authed.POST("/drawings/:id/confirm", requireRole(RoleCustomer), confirmDrawing)
	authed.POST("/rework-plans/:id/decide", requireRole(RoleCustomer), decidePlan)

	// 工厂
	authed.POST("/orders/:id/fabricate", requireRole(RoleFactory), fabricateOrder)

	// 安装员
	authed.POST("/orders/:id/field-measurements", requireRole(RoleInstaller), addFieldMeasurement)
	authed.POST("/orders/:id/rework-plans", requireRole(RoleInstaller), proposePlan)
	authed.POST("/rework-plans/:id/complete", requireRole(RoleInstaller), completePlan)
	authed.POST("/orders/:id/complete", requireRole(RoleInstaller), completeOrder)

	r.Static("/uploads", "uploads")
	r.Static("/static", "static")
	r.GET("/", func(c *gin.Context) { c.File("static/index.html") })

	log.Println("厨房台面开孔复核系统已启动: http://localhost:8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
