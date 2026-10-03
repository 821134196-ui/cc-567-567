package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// submitInitialMeasurements 测量员提交初测数据（型号/开孔位置/尺寸/测量图片）
func submitInitialMeasurements(c *gin.Context) {
	o, ok := getOrderByID(c)
	if !ok {
		return
	}
	u := c.MustGet("user").(*authUser)
	var req struct {
		Items []struct {
			Slot   string   `json:"slot"`
			Width  float64  `json:"width"`
			Depth  float64  `json:"depth"`
			PosX   float64  `json:"pos_x"`
			PosY   float64  `json:"pos_y"`
			Images []string `json:"images"`
			Note   string   `json:"note"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写初测数据"})
		return
	}
	created := []MeasureRecord{}
	for _, it := range req.Items {
		rec := MeasureRecord{
			OrderID: o.ID, VersionID: o.CurrentVersionID, Slot: it.Slot, Stage: MeasureInitial,
			Width: it.Width, Depth: it.Depth, PosX: it.PosX, PosY: it.PosY,
			Images: encodeImages(it.Images), Note: it.Note,
			MeasuredByID: u.ID, MeasuredByName: u.Name,
		}
		if err := db.Create(&rec).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		created = append(created, rec)
	}
	if o.Status == StatusMeasuring {
		db.Model(&Order{}).Where("id = ?", o.ID).Update("status", StatusMeasuring)
	}
	c.JSON(http.StatusCreated, created)
}
